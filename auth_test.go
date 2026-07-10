package reddit

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// oauthTestClient returns a Client whose token endpoint and API base both
// point at a single httptest server, so the full authenticated round-trip
// (token fetch + Bearer request) can be exercised offline.
func oauthTestClient(t *testing.T, tokenBody string, tokenStatus int, api http.HandlerFunc) (*Client, *int) {
	t.Helper()
	var tokenCalls int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/access_token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if u, p, ok := r.BasicAuth(); !ok || u != "id" || p != "secret" {
			t.Errorf("basic auth = %q,%q,%v", u, p, ok)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		if tokenStatus != 0 {
			w.WriteHeader(tokenStatus)
		}
		w.Write([]byte(tokenBody))
	})
	mux.HandleFunc("/", api)
	srv := newRawServer(t, mux)
	c := NewClient(WithOAuth("id", "secret"), WithUserAgent("t/1"), WithBaseURL(srv))
	c.auth.tokenURL = srv + "/api/v1/access_token"
	return c, &tokenCalls
}

func TestOAuthClientCredentialsFlow(t *testing.T) {
	c, calls := oauthTestClient(t, `{"access_token":"TOK","token_type":"bearer","expires_in":3600}`, 0,
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer TOK" {
				t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
			}
			w.Write([]byte(sampleListing))
		})
	page, err := c.Subreddit(context.Background(), "golang", SortHot, ListingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 1 {
		t.Fatalf("got %d posts", len(page.Posts))
	}
	// Second call reuses the cached token (no second token fetch).
	if _, err := c.Frontpage(context.Background(), SortHot, ListingOptions{}); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Errorf("token fetched %d times, want 1 (cached)", *calls)
	}
}

func TestOAuthScriptGrantSendsPassword(t *testing.T) {
	var gotGrant string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/access_token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		gotGrant = r.Form.Get("grant_type")
		if r.Form.Get("username") != "bob" || r.Form.Get("password") != "pw" {
			t.Errorf("form = %v", r.Form)
		}
		w.Write([]byte(`{"access_token":"TOK","expires_in":3600}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sampleListing)) })
	srv := newRawServer(t, mux)
	c := NewClient(WithOAuthScript("id", "secret", "bob", "pw"), WithBaseURL(srv))
	c.auth.tokenURL = srv + "/api/v1/access_token"
	if _, err := c.Subreddit(context.Background(), "golang", SortHot, ListingOptions{}); err != nil {
		t.Fatal(err)
	}
	if gotGrant != "password" {
		t.Errorf("grant_type = %q, want password", gotGrant)
	}
}

func TestOAuthOptionsIgnoreEmptyCreds(t *testing.T) {
	c := NewClient(WithOAuth("", "secret"), WithOAuthScript("id", "secret", "", "pw"))
	if c.auth != nil {
		t.Error("empty credentials should leave client anonymous")
	}
}

func TestOAuthTokenError(t *testing.T) {
	c, _ := oauthTestClient(t, `{"error":"invalid_grant"}`, http.StatusUnauthorized, func(w http.ResponseWriter, r *http.Request) {})
	_, err := c.Subreddit(context.Background(), "golang", SortHot, ListingOptions{})
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("want token APIError, got %v", err)
	}
}

func TestOAuthTokenDecodeError(t *testing.T) {
	c, _ := oauthTestClient(t, `{bad json`, 0, func(w http.ResponseWriter, r *http.Request) {})
	_, err := c.Subreddit(context.Background(), "golang", SortHot, ListingOptions{})
	if err == nil || !strings.Contains(err.Error(), "decode token") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestOAuthTokenTransportError(t *testing.T) {
	c := NewClient(WithOAuth("id", "secret"))
	c.auth.tokenURL = "http://127.0.0.1:0/token" // unroutable
	_, err := c.Subreddit(context.Background(), "golang", SortHot, ListingOptions{})
	if err == nil || !strings.Contains(err.Error(), "token request") {
		t.Fatalf("want token transport error, got %v", err)
	}
}

func TestEnsureTokenBuildRequestError(t *testing.T) {
	c := NewClient(WithOAuth("id", "secret"))
	c.auth.tokenURL = "http://example.com/\x7f\n"
	_, err := c.auth.ensureToken(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "build token request") {
		t.Fatalf("want build error, got %v", err)
	}
}

func TestEnsureTokenCacheExpiry(t *testing.T) {
	a := &oauthConfig{token: "cached", tokenExp: time.Now().Add(2 * time.Hour)}
	tok, err := a.ensureToken(context.Background(), NewClient())
	if err != nil || tok != "cached" {
		t.Fatalf("fresh cached token should be reused: tok=%q err=%v", tok, err)
	}
	// Concurrent callers must not race the cache.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.ensureToken(context.Background(), NewClient()) }()
	}
	wg.Wait()
}
