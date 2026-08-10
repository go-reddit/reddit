// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a controllable clock + sleep for the limiter: sleeping advances
// the clock instantly (no real waiting) and records the requested durations.
type fakeClock struct {
	t      time.Time
	slept  []time.Duration
	cancel bool // when true, sleep reports a cancelled context
}

func (f *fakeClock) now() time.Time { return f.t }

func (f *fakeClock) sleep(ctx context.Context, d time.Duration) error {
	if f.cancel {
		return context.Canceled
	}
	if d > 0 {
		f.slept = append(f.slept, d)
		f.t = f.t.Add(d)
	}
	return ctx.Err()
}

func newFakeLimiter(perMinute int) (*limiter, *fakeClock) {
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := &limiter{
		minInterval: time.Minute / time.Duration(perMinute),
		now:         fc.now,
		sleep:       fc.sleep,
	}
	return l, fc
}

func TestLimiterPacesRequests(t *testing.T) {
	l, fc := newFakeLimiter(60) // 1s spacing
	ctx := context.Background()
	// First request goes out immediately (no prior reservation).
	if err := l.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fc.slept) != 0 {
		t.Fatalf("first request slept %v, want none", fc.slept)
	}
	// The next two each wait one interval.
	for i := 0; i < 2; i++ {
		if err := l.wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(fc.slept) != 2 || fc.slept[0] != time.Second || fc.slept[1] != time.Second {
		t.Fatalf("paced sleeps = %v, want [1s 1s]", fc.slept)
	}
}

func TestLimiterWaitRespectsCancellation(t *testing.T) {
	l, fc := newFakeLimiter(60)
	fc.cancel = true
	if err := l.wait(context.Background()); err != context.Canceled {
		t.Fatalf("wait err = %v, want context.Canceled", err)
	}
}

func TestLimiterPauseFor(t *testing.T) {
	l, fc := newFakeLimiter(600) // 100ms baseline
	ctx := context.Background()
	l.pauseFor(5 * time.Second)
	if err := l.wait(ctx); err != nil {
		t.Fatal(err)
	}
	if len(fc.slept) != 1 || fc.slept[0] != 5*time.Second {
		t.Fatalf("after pauseFor, slept = %v, want [5s]", fc.slept)
	}
	// A non-positive pause is a no-op (never pulls the schedule in).
	before := l.next
	l.pauseFor(0)
	l.pauseFor(-time.Second)
	if !l.next.Equal(before) {
		t.Fatalf("non-positive pauseFor moved next: %v -> %v", before, l.next)
	}
}

func TestLimiterObserveExhaustedWindow(t *testing.T) {
	l, fc := newFakeLimiter(600)
	h := http.Header{}
	h.Set("X-Ratelimit-Remaining", "0")
	h.Set("X-Ratelimit-Reset", "30")
	l.observe(h)
	if err := l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.slept) != 1 || fc.slept[0] != 30*time.Second {
		t.Fatalf("exhausted-window wait = %v, want [30s]", fc.slept)
	}
}

func TestLimiterObserveSpreadsRemaining(t *testing.T) {
	l, fc := newFakeLimiter(6000) // 10ms baseline — well below the spread
	h := http.Header{}
	// 10 requests left over 40s -> 4s spacing (> floor), so pacing widens.
	h.Set("X-Ratelimit-Remaining", "10")
	h.Set("X-Ratelimit-Reset", "40")
	l.observe(h)
	if err := l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.slept) != 1 || fc.slept[0] != 4*time.Second {
		t.Fatalf("spread wait = %v, want [4s]", fc.slept)
	}
}

func TestLimiterObserveHeadroomKeepsBaseline(t *testing.T) {
	l, fc := newFakeLimiter(600) // 100ms baseline
	h := http.Header{}
	// Lots of headroom: 1000 left over 60s -> 60ms spacing < floor -> ignored.
	h.Set("X-Ratelimit-Remaining", "1000")
	h.Set("X-Ratelimit-Reset", "60")
	l.observe(h)
	// No pause applied; the first wait still goes out immediately.
	if err := l.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fc.slept) != 0 {
		t.Fatalf("headroom should not pause, slept %v", fc.slept)
	}
}

func TestLimiterObserveIgnoresMissingHeaders(t *testing.T) {
	l, _ := newFakeLimiter(600)
	before := l.next
	l.observe(http.Header{})                                                             // absent
	l.observe(http.Header{"X-Ratelimit-Remaining": {"nope"}})                            // malformed remaining
	l.observe(http.Header{"X-Ratelimit-Remaining": {"5"}})                               // reset absent
	l.observe(http.Header{"X-Ratelimit-Remaining": {"5"}, "X-Ratelimit-Reset": {"bad"}}) // malformed reset
	if !l.next.Equal(before) {
		t.Fatal("observe with bad/missing headers must not change pacing")
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	l, _ := newFakeLimiter(600)
	h := http.Header{"Retry-After": {"7"}}
	if d, ok := l.retryAfter(h); !ok || d != 7*time.Second {
		t.Fatalf("retryAfter seconds = %v,%v, want 7s,true", d, ok)
	}
	// A negative delta clamps to zero.
	if d, ok := l.retryAfter(http.Header{"Retry-After": {"-3"}}); !ok || d != 0 {
		t.Fatalf("negative retryAfter = %v,%v, want 0,true", d, ok)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	l, fc := newFakeLimiter(600)
	future := fc.t.Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if d, ok := l.retryAfter(http.Header{"Retry-After": {future}}); !ok || d != 90*time.Second {
		t.Fatalf("retryAfter date = %v,%v, want 90s,true", d, ok)
	}
	// A past date yields ok with a zero wait.
	past := fc.t.Add(-time.Minute).UTC().Format(http.TimeFormat)
	if d, ok := l.retryAfter(http.Header{"Retry-After": {past}}); !ok || d != 0 {
		t.Fatalf("past retryAfter = %v,%v, want 0,true", d, ok)
	}
}

func TestRetryAfterAbsentOrJunk(t *testing.T) {
	l, _ := newFakeLimiter(600)
	if _, ok := l.retryAfter(http.Header{}); ok {
		t.Fatal("absent Retry-After should be ok=false")
	}
	if _, ok := l.retryAfter(http.Header{"Retry-After": {"soon"}}); ok {
		t.Fatal("junk Retry-After should be ok=false")
	}
}

// TestGetRetriesOn429 drives the full get() path: the server returns 429 twice
// (with a Retry-After) then 200, and the client must retry and succeed, honoring
// the Retry-After wait each time.
func TestGetRetriesOn429(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) <= 2 {
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(sampleListing))
	})
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	c.limiter.now, c.limiter.sleep = fc.now, fc.sleep

	page, err := c.Subreddit(context.Background(), "golang", SortHot, ListingOptions{})
	if err != nil {
		t.Fatalf("Subreddit after retries: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (two 429s then success)", calls)
	}
	if len(page.Posts) != 1 {
		t.Fatalf("page.Posts = %d, want 1", len(page.Posts))
	}
	// Each 429 honored the 3s Retry-After.
	if len(fc.slept) < 2 {
		t.Fatalf("expected >=2 Retry-After sleeps, got %v", fc.slept)
	}
	for _, d := range fc.slept {
		if d != 3*time.Second {
			t.Fatalf("Retry-After sleep = %v, want 3s (all)", d)
		}
	}
}

// TestGetGivesUpAfterMaxRetries: a server that always 429s eventually returns
// the error rather than looping forever.
func TestGetGivesUpAfterMaxRetries(t *testing.T) {
	var calls int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	})
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	c.limiter.now, c.limiter.sleep = fc.now, fc.sleep

	_, err := c.Subreddit(context.Background(), "golang", SortHot, ListingOptions{})
	ae := &APIError{}
	if !errors.As(err, &ae) || ae.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("err = %v, want 429 APIError", err)
	}
	if calls != maxRetries+1 {
		t.Fatalf("calls = %d, want %d (initial + maxRetries)", calls, maxRetries+1)
	}
}

// TestGetBackoffWithoutRetryAfter: a 429 without Retry-After falls back to the
// exponential 1s,2s,4s,… backoff.
func TestGetBackoffWithoutRetryAfter(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	c.limiter.now, c.limiter.sleep = fc.now, fc.sleep

	_, _ = c.Subreddit(context.Background(), "golang", SortHot, ListingOptions{})
	// The pauseFor-driven backoffs surface on the following wait()s; assert the
	// escalating 1s,2s,4s,8s among the recorded sleeps.
	want := map[time.Duration]bool{time.Second: false, 2 * time.Second: false, 4 * time.Second: false, 8 * time.Second: false}
	for _, d := range fc.slept {
		if _, ok := want[d]; ok {
			want[d] = true
		}
	}
	for d, seen := range want {
		if !seen {
			t.Fatalf("expected a %v backoff among sleeps %v", d, fc.slept)
		}
	}
}

func TestNewLimiterDefaultsOnNonPositive(t *testing.T) {
	l := newLimiter(0)
	if l.minInterval != time.Minute/time.Duration(DefaultRequestsPerMinute) {
		t.Fatalf("minInterval = %v, want default", l.minInterval)
	}
}

func TestWithRateLimit(t *testing.T) {
	c := NewClient(WithRateLimit(120))
	if c.limiter.minInterval != time.Minute/120 {
		t.Fatalf("minInterval = %v, want 500ms", c.limiter.minInterval)
	}
	c0 := NewClient(WithRateLimit(0))
	if c0.limiter.minInterval != time.Minute/time.Duration(DefaultRequestsPerMinute) {
		t.Fatalf("WithRateLimit(0) minInterval = %v, want default", c0.limiter.minInterval)
	}
}

func TestSleepCtx(t *testing.T) {
	// Non-positive duration returns immediately with the context's status.
	if err := sleepCtx(context.Background(), 0); err != nil {
		t.Fatalf("sleepCtx(0) = %v, want nil", err)
	}
	// A real short sleep fires the timer and returns nil.
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("sleepCtx(1ms) = %v, want nil", err)
	}
	// A cancelled context short-circuits both the d<=0 and the timer paths.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, 0); err != context.Canceled {
		t.Fatalf("sleepCtx(0, cancelled) = %v, want Canceled", err)
	}
	if err := sleepCtx(ctx, time.Hour); err != context.Canceled {
		t.Fatalf("sleepCtx(1h, cancelled) = %v, want Canceled", err)
	}
}

func TestGetAbortsWhenLimiterWaitFails(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleListing))
	})
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0), cancel: true}
	c.limiter.now, c.limiter.sleep = fc.now, fc.sleep
	if _, err := c.Subreddit(context.Background(), "golang", SortHot, ListingOptions{}); err != context.Canceled {
		t.Fatalf("Subreddit err = %v, want context.Canceled from limiter wait", err)
	}
}
