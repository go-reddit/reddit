package reddit

import (
	"context"
	"net/http"
	"testing"
)

func TestNormalizeCookie(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"bare value wrapped", "abc123", "reddit_session=abc123"},
		{"bare value trimmed then wrapped", "  abc123  ", "reddit_session=abc123"},
		{"full cookie kept", "reddit_session=abc123; token_v2=xyz", "reddit_session=abc123; token_v2=xyz"},
		{"full cookie trimmed", "  reddit_session=abc123  ", "reddit_session=abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeCookie(tc.in); got != tc.want {
				t.Errorf("normalizeCookie(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestWithSessionCookieStoresValueAndKeepsWWWBase(t *testing.T) {
	c := NewClient(WithSessionCookie("abc123"))
	if c.sessionCookie != "reddit_session=abc123" {
		t.Errorf("sessionCookie = %q, want normalized value", c.sessionCookie)
	}
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want default www base %q", c.baseURL, DefaultBaseURL)
	}
	if c.auth != nil {
		t.Error("WithSessionCookie must not configure OAuth")
	}
}

func TestWithSessionCookieEmptyIgnored(t *testing.T) {
	c := NewClient(WithSessionCookie("   "))
	if c.sessionCookie != "" {
		t.Errorf("empty cookie should be ignored, got %q", c.sessionCookie)
	}
}

func TestGetSendsSessionCookieNoBearer(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Cookie"); got != "reddit_session=sess" {
			t.Errorf("Cookie = %q, want reddit_session=sess", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want none for cookie auth", got)
		}
		w.Write([]byte(`{}`))
	})
	// newTestClient builds an anonymous client; add the session cookie on top.
	WithSessionCookie("sess")(c)
	var v map[string]any
	if err := c.get(context.Background(), "/x.json", &v); err != nil {
		t.Fatal(err)
	}
}

func TestGetSessionCookieBeatsOAuth(t *testing.T) {
	// When both a session cookie and OAuth are configured, the cookie wins:
	// the request must carry the Cookie header and no Bearer, and the OAuth
	// token endpoint must never be hit.
	base := newRawServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/access_token" {
			t.Fatalf("OAuth token endpoint must not be called when a session cookie is set")
		}
		if got := r.Header.Get("Cookie"); got != "reddit_session=sess" {
			t.Errorf("Cookie = %q, want reddit_session=sess", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want none (cookie precedence)", got)
		}
		w.Write([]byte(`{}`))
	}))
	// WithOAuth points baseURL at oauth.reddit.com; a trailing WithBaseURL
	// redirects everything (including the token endpoint) at the test server.
	c := NewClient(
		WithUserAgent("test/1.0"),
		WithOAuth("id", "secret"),
		WithSessionCookie("sess"),
		WithBaseURL(base),
	)
	c.auth.tokenURL = base + "/api/v1/access_token"
	var v map[string]any
	if err := c.get(context.Background(), "/x.json", &v); err != nil {
		t.Fatal(err)
	}
}
