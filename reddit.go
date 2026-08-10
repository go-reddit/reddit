// Package reddit is a dependency-free (stdlib-only, CGO=0) client for
// Reddit's public read-only JSON API.
//
// It targets the anonymous endpoints that back every subreddit and post
// page — appending ".json" to any Reddit URL returns the same listing the
// site renders. No OAuth, no API key: the only requirement Reddit enforces
// on anonymous traffic is a descriptive, non-generic User-Agent (a missing
// or browser-mimicking UA earns a 429). Set one with [WithUserAgent].
//
// The client is deliberately small and allocation-light so it can be reused
// unchanged from a native binary, a wasm proxy, or a Ruby binding
// (github.com/go-ruby-reddit). It builds for every Go target, cgo disabled.
package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is Reddit's canonical JSON host. The "www" host (rather
// than "old" or "api") returns the modern listing schema this package
// decodes.
const DefaultBaseURL = "https://www.reddit.com"

// DefaultUserAgent identifies this library. Reddit rate-limits anonymous
// requests that omit a UA or impersonate a browser, so callers are strongly
// encouraged to override this with [WithUserAgent] using their own contact
// string (Reddit's guidance: "<platform>:<app-id>:<version> (by /u/<name>)").
const DefaultUserAgent = "go-reddit/reddit (+https://github.com/go-reddit/reddit)"

// Client fetches listings and comments from Reddit's public JSON API. The
// zero value is not usable; construct one with [NewClient]. A Client is safe
// for concurrent use as long as the underlying http.Client is.
type Client struct {
	httpc     *http.Client
	baseURL   string
	userAgent string
	auth      *oauthConfig // nil => anonymous ".json" access
	// sessionCookie is a ready-to-send Cookie header value carrying the user's
	// logged-in reddit_session cookie. When non-empty it authenticates reads in
	// place of OAuth (see [WithSessionCookie]); it takes precedence over auth.
	sessionCookie string

	// limiter paces outbound requests so aggregating many subscriptions stays
	// under Reddit's budget instead of tripping 429s. Always non-nil (NewClient
	// installs a default); [WithRateLimit] tunes the rate.
	limiter *limiter
}

// Option customises a [Client] at construction time.
type Option func(*Client)

// WithHTTPClient supplies the http.Client used for every request. Use it to
// inject timeouts, proxies, or a test transport. A nil client is ignored.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpc = h
		}
	}
}

// WithUserAgent sets the User-Agent header. An empty string is ignored so the
// [DefaultUserAgent] always remains in effect. Reddit blocks generic agents,
// so production callers should pass a unique, descriptive value.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// WithBaseURL overrides the API host. Primarily a test seam (point it at an
// httptest server); an empty string is ignored. Any trailing slash is
// trimmed so path joining stays predictable.
func WithBaseURL(base string) Option {
	return func(c *Client) {
		if base != "" {
			c.baseURL = strings.TrimRight(base, "/")
		}
	}
}

// WithRateLimit paces the client to at most perMinute requests, so aggregating
// many subscriptions (or paging an infinite scroll) glides under Reddit's budget
// instead of tripping 429 "Too Many Requests". The client further adapts to the
// X-Ratelimit headers Reddit returns and honours a 429's Retry-After. A
// perMinute <= 0 restores [DefaultRequestsPerMinute].
func WithRateLimit(perMinute int) Option {
	return func(c *Client) {
		c.limiter = newLimiter(perMinute)
	}
}

// NewClient returns a Client with sensible defaults (a 30-second HTTP
// timeout, the canonical base URL, [DefaultUserAgent], and a
// [DefaultRequestsPerMinute] rate limiter), then applies each Option in order.
func NewClient(opts ...Option) *Client {
	c := &Client{
		httpc:     &http.Client{Timeout: 30 * time.Second},
		baseURL:   DefaultBaseURL,
		userAgent: DefaultUserAgent,
		limiter:   newLimiter(DefaultRequestsPerMinute),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// APIError is returned when Reddit responds with a non-2xx status. It carries
// the HTTP status code and a short snippet of the body for diagnosis (Reddit
// serves an HTML or JSON error page on 403/429/5xx).
type APIError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("reddit: %s", e.Status)
	}
	return fmt.Sprintf("reddit: %s: %s", e.Status, e.Body)
}

// get performs a GET against path (already including any query string),
// decodes the JSON body into v, and translates transport/status failures into
// typed errors. path must begin with "/".
func (c *Client) get(ctx context.Context, path string, v any) error {
	// Each attempt waits its turn on the shared limiter, then sends. A 429/503
	// (Reddit's rate-limit responses) is retried after the server's Retry-After
	// (or an exponential backoff); every other outcome returns immediately. The
	// limiter also adapts to the X-Ratelimit headers on each response.
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := c.limiter.wait(ctx); err != nil {
			return err
		}
		resp, err := c.doOnce(ctx, path)
		if err != nil {
			return err
		}
		c.limiter.observe(resp.header)
		if resp.status == http.StatusTooManyRequests || resp.status == http.StatusServiceUnavailable {
			lastErr = &APIError{StatusCode: resp.status, Status: resp.statusText, Body: snippet(resp.body)}
			if attempt == maxRetries {
				break
			}
			c.limiter.pauseFor(c.backoff(resp.header, attempt))
			continue
		}
		if resp.status < 200 || resp.status >= 300 {
			return &APIError{StatusCode: resp.status, Status: resp.statusText, Body: snippet(resp.body)}
		}
		body := resp.body
		if err := json.Unmarshal(body, v); err != nil {
			return fmt.Errorf("reddit: decode %s: %w", path, err)
		}
		return nil
	}
	return lastErr
}

// backoff picks how long to wait before retrying a rate-limited request: the
// server's Retry-After when present, otherwise an exponential 1s, 2s, 4s, …
// escalation keyed to the attempt number.
func (c *Client) backoff(hdr http.Header, attempt int) time.Duration {
	if d, ok := c.limiter.retryAfter(hdr); ok {
		return d
	}
	return time.Second << attempt
}

// rawResponse is one HTTP round-trip's outcome: the status code + line, the
// response headers (for rate-limit adaptation), and the length-capped body.
type rawResponse struct {
	status     int
	statusText string
	header     http.Header
	body       []byte
}

// doOnce sends a single GET (building the request fresh so a retry re-applies a
// possibly-refreshed auth token) and returns the round-trip. A transport error
// or a token-refresh failure is returned as err; an HTTP error status is not
// (the caller inspects the status).
func (c *Client) doOnce(ctx context.Context, path string) (*rawResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("reddit: build request: %w", err)
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	// A configured session cookie authenticates the request and takes
	// precedence over OAuth: send it as-is and add no Bearer. Otherwise fall
	// back to the OAuth bearer when credentials are configured.
	switch {
	case c.sessionCookie != "":
		req.Header.Set("Cookie", c.sessionCookie)
	case c.auth != nil:
		tok, err := c.auth.ensureToken(ctx, c)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reddit: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	// Cap the body read so a hostile or misbehaving endpoint can't exhaust
	// memory; 8 MiB comfortably covers the largest real listing pages.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("reddit: read body %s: %w", path, err)
	}
	return &rawResponse{status: resp.StatusCode, statusText: resp.Status, header: resp.Header, body: body}, nil
}

// snippet returns a single-line, length-bounded view of an error body so
// APIError.Error() stays readable in logs.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// listingQuery builds the "?limit=…&after=…&t=…" query common to every
// listing endpoint. Zero/empty fields are omitted so URLs stay canonical.
func listingQuery(opts ListingOptions) string {
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.After != "" {
		q.Set("after", opts.After)
	}
	if opts.Time != "" {
		q.Set("t", string(opts.Time))
	}
	if enc := q.Encode(); enc != "" {
		return "?" + enc
	}
	return ""
}
