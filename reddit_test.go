package reddit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestClient spins up an httptest server whose handler is h and returns a
// Client pointed at it.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(WithBaseURL(srv.URL), WithUserAgent("test/1.0"))
}

// newRawServer starts an httptest server with the given handler and returns
// its base URL, closing it at test end.
func newRawServer(t *testing.T, h http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient()
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c.userAgent != DefaultUserAgent {
		t.Errorf("userAgent = %q, want default", c.userAgent)
	}
	if c.httpc == nil || c.httpc.Timeout != 30*time.Second {
		t.Errorf("http client not defaulted: %+v", c.httpc)
	}
}

func TestOptionsIgnoreEmptyAndNil(t *testing.T) {
	c := NewClient(
		WithHTTPClient(nil),
		WithUserAgent(""),
		WithBaseURL(""),
	)
	if c.userAgent != DefaultUserAgent || c.baseURL != DefaultBaseURL || c.httpc == nil {
		t.Errorf("empty/nil options should be ignored, got %+v", c)
	}
	custom := &http.Client{Timeout: time.Second}
	c = NewClient(WithHTTPClient(custom), WithUserAgent("ua"), WithBaseURL("http://x/"))
	if c.httpc != custom {
		t.Error("WithHTTPClient not applied")
	}
	if c.userAgent != "ua" {
		t.Error("WithUserAgent not applied")
	}
	if c.baseURL != "http://x" { // trailing slash trimmed
		t.Errorf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
}

func TestGetSendsHeaders(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "test/1.0" {
			t.Errorf("UA = %q", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.Write([]byte(`{}`))
	})
	var v map[string]any
	if err := c.get(context.Background(), "/x.json", &v); err != nil {
		t.Fatal(err)
	}
}

func TestGetBuildRequestError(t *testing.T) {
	// A control character in the URL makes http.NewRequestWithContext fail.
	c := NewClient(WithBaseURL("http://example.com"))
	c.baseURL = "http://example.com/\x7f"
	err := c.get(context.Background(), "\n", nil)
	if err == nil || !strings.Contains(err.Error(), "build request") {
		t.Fatalf("want build-request error, got %v", err)
	}
}

func TestGetTransportError(t *testing.T) {
	// Point at a closed server so Do() fails.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	c := NewClient(WithBaseURL(url))
	err := c.get(context.Background(), "/x.json", nil)
	if err == nil || !strings.Contains(err.Error(), "request /x.json") {
		t.Fatalf("want transport error, got %v", err)
	}
}

// errBody is a ReadCloser whose Read always fails, to exercise the
// body-read error path in get().
type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("read boom") }
func (errBody) Close() error             { return nil }

type errBodyTransport struct{}

func (errBodyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       errBody{},
		Header:     make(http.Header),
	}, nil
}

func TestGetReadBodyError(t *testing.T) {
	c := NewClient(WithBaseURL("http://example.com"), WithHTTPClient(&http.Client{Transport: errBodyTransport{}}))
	err := c.get(context.Background(), "/x.json", nil)
	if err == nil || !strings.Contains(err.Error(), "read body") {
		t.Fatalf("want read-body error, got %v", err)
	}
}

func TestGetAPIError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("slow down\nplease"))
	})
	err := c.get(context.Background(), "/x.json", nil)
	var ae *APIError
	if !errors.As(err, &ae) {
		t.Fatalf("want *APIError, got %T %v", err, err)
	}
	if ae.StatusCode != http.StatusTooManyRequests {
		t.Errorf("StatusCode = %d", ae.StatusCode)
	}
	if !strings.Contains(ae.Error(), "slow down please") {
		t.Errorf("Error() = %q, want body snippet", ae.Error())
	}
}

func TestAPIErrorMessageWithoutBody(t *testing.T) {
	e := &APIError{Status: "429 Too Many Requests"}
	if got := e.Error(); got != "reddit: 429 Too Many Requests" {
		t.Errorf("Error() = %q", got)
	}
}

func TestGetDecodeError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{not json`))
	})
	var v map[string]any
	err := c.get(context.Background(), "/x.json", &v)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestSnippetTruncates(t *testing.T) {
	long := strings.Repeat("a", 250)
	got := snippet([]byte("  " + long + "  "))
	if !strings.HasSuffix(got, "…") || len(got) < 200 {
		t.Errorf("snippet not truncated: len=%d", len(got))
	}
	if got := snippet([]byte("line1\nline2")); got != "line1 line2" {
		t.Errorf("newline not folded: %q", got)
	}
}

func TestListingQuery(t *testing.T) {
	if q := listingQuery(ListingOptions{}); q != "" {
		t.Errorf("empty opts -> %q, want empty", q)
	}
	q := listingQuery(ListingOptions{Limit: 25, After: "t3_x", Time: TimeWeek})
	for _, want := range []string{"limit=25", "after=t3_x", "t=week"} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q missing %q", q, want)
		}
	}
	if !strings.HasPrefix(q, "?") {
		t.Errorf("query %q should start with ?", q)
	}
}
