// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reddit answers bursts of requests — exactly what aggregating many
// subscriptions produces — with HTTP 429 "Too Many Requests". The client paces
// its own traffic through a [limiter] so it stays under Reddit's budget, adapts
// to the X-Ratelimit-* headers Reddit returns on every response, and, when a 429
// slips through anyway, honours the Retry-After header and retries. All of this
// lives below get(), so every endpoint benefits and concurrent callers (the
// per-subscription goroutines) serialise through the one shared limiter.

// DefaultRequestsPerMinute is the self-imposed request rate a fresh client uses
// until Reddit's own X-Ratelimit headers refine it. 60/min (one request per
// second) sits at Reddit's documented OAuth budget and well under what the
// anonymous JSON endpoints tolerate, so a first burst does not trip a 429.
const DefaultRequestsPerMinute = 60

// maxRetries bounds how many times get() re-sends a request that came back 429
// (or 503) before giving up and returning the error, so a persistently
// rate-limited or wedged endpoint cannot loop forever.
const maxRetries = 4

// rateLimitFloor is the smallest spacing the adaptive pacer will impose from the
// X-Ratelimit headers, so a momentarily tiny "remaining" cannot stall the client
// for minutes; the explicit Retry-After path still honours longer server waits.
const rateLimitFloor = 250 * time.Millisecond

// limiter paces outbound requests to at most one per minInterval and lets a
// response push the next-allowed instant further out (a 429's Retry-After, or an
// exhausted X-Ratelimit window). It is safe for concurrent use: the
// per-subscription fetch goroutines all wait on the same limiter, so they leave
// the client one at a time rather than stampeding Reddit at once.
type limiter struct {
	mu          sync.Mutex
	minInterval time.Duration // baseline spacing between requests
	next        time.Time     // earliest instant the next request may go out

	// now/sleep are seams so tests drive timing deterministically. sleep must
	// return early with ctx.Err() when ctx is cancelled.
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
}

// timeNow and timeSleep are the wall-clock sources every limiter is built with.
// They are package vars only so the test suite can install instant timing once
// (in TestMain) instead of sleeping through the real pacing on every request.
var (
	timeNow   = time.Now
	timeSleep = sleepCtx
)

// newLimiter builds a limiter pacing to perMinute requests (falling back to
// [DefaultRequestsPerMinute] when perMinute <= 0), with wall-clock timing.
func newLimiter(perMinute int) *limiter {
	if perMinute <= 0 {
		perMinute = DefaultRequestsPerMinute
	}
	return &limiter{
		minInterval: time.Minute / time.Duration(perMinute),
		now:         timeNow,
		sleep:       timeSleep,
	}
}

// sleepCtx sleeps for d unless ctx is cancelled first, in which case it returns
// ctx.Err() immediately. A non-positive d returns nil without blocking.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// wait blocks until this request's turn, then reserves the following slot. It
// returns ctx.Err() if the context is cancelled while waiting.
func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := l.now()
	start := l.next
	if start.Before(now) {
		start = now
	}
	// Reserve the slot after this one before unlocking, so concurrent callers
	// each get a distinct, spaced turn instead of all reading the same "next".
	l.next = start.Add(l.minInterval)
	l.mu.Unlock()
	return l.sleep(ctx, start.Sub(now))
}

// pauseFor pushes the next-allowed instant out to at least now+d (never pulling
// it in), so a 429 Retry-After or an exhausted rate window delays every queued
// request, not just the one that saw the header.
func (l *limiter) pauseFor(d time.Duration) {
	if d <= 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	until := l.now().Add(d)
	if until.After(l.next) {
		l.next = until
	}
}

// observe adapts the pacing from a response's X-Ratelimit headers: when the
// window is exhausted it pauses until the reset, and otherwise spreads the
// remaining allowance across the remaining window (bounded by the floor) so the
// client glides down to the limit instead of slamming into a 429.
func (l *limiter) observe(h http.Header) {
	remaining, okR := parseRateFloat(h.Get("X-Ratelimit-Remaining"))
	reset, okT := parseRateFloat(h.Get("X-Ratelimit-Reset"))
	if !okR || !okT {
		return
	}
	resetD := time.Duration(reset * float64(time.Second))
	if remaining <= 0 {
		l.pauseFor(resetD)
		return
	}
	spacing := time.Duration(float64(resetD) / remaining)
	if spacing < rateLimitFloor {
		return // plenty of headroom; keep the baseline pace
	}
	l.pauseFor(spacing)
}

// parseRateFloat parses a Reddit rate-limit header (Reddit sends these as
// floats, e.g. "59.0"); ok is false for an absent or malformed value.
func parseRateFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// retryAfter returns the wait a 429/503 response asks for via its Retry-After
// header — either delta-seconds ("5") or an HTTP-date — or ok=false when the
// header is absent or unparseable (the caller then falls back to backoff).
func (l *limiter) retryAfter(h http.Header) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			secs = 0
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(l.now()); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}
