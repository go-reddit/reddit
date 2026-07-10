package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// oauthBaseURL is the host that serves authenticated API traffic. Reddit
// increasingly blocks the anonymous www ".json" endpoints (403), so OAuth is
// the reliable path for server-side and datacenter callers.
const oauthBaseURL = "https://oauth.reddit.com"

// tokenURL is Reddit's OAuth2 token endpoint (always on www, even for
// app-only grants).
const tokenURL = "https://www.reddit.com/api/v1/access_token"

// oauthConfig holds the credentials and cached bearer token for an
// authenticated Client. A nil *oauthConfig on the Client means anonymous
// access (plain ".json" endpoints).
type oauthConfig struct {
	clientID     string
	clientSecret string
	username     string // empty => app-only (client_credentials) grant
	password     string
	tokenURL     string // overridable for tests

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

// WithOAuth configures application-only ("client_credentials") OAuth using a
// registered script/web app's client id and secret. This grants read access
// to public listings without a user account and works from IPs where the
// anonymous ".json" endpoints are blocked. Empty credentials are ignored
// (the Client stays anonymous).
func WithOAuth(clientID, clientSecret string) Option {
	return func(c *Client) {
		if clientID == "" || clientSecret == "" {
			return
		}
		c.auth = &oauthConfig{clientID: clientID, clientSecret: clientSecret, tokenURL: tokenURL}
		c.baseURL = oauthBaseURL
	}
}

// WithOAuthScript configures the resource-owner-password ("script app") grant,
// authenticating as a specific Reddit user. Use it when you need per-user
// context; otherwise prefer [WithOAuth]. Empty credentials are ignored.
func WithOAuthScript(clientID, clientSecret, username, password string) Option {
	return func(c *Client) {
		if clientID == "" || clientSecret == "" || username == "" || password == "" {
			return
		}
		c.auth = &oauthConfig{
			clientID: clientID, clientSecret: clientSecret,
			username: username, password: password, tokenURL: tokenURL,
		}
		c.baseURL = oauthBaseURL
	}
}

// tokenResponse is Reddit's /access_token JSON reply.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Error       string `json:"error"`
}

// ensureToken returns a valid bearer token, fetching a new one if the cache
// is empty or within 60s of expiry. Safe for concurrent use.
func (a *oauthConfig) ensureToken(ctx context.Context, c *Client) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && time.Until(a.tokenExp) > 60*time.Second {
		return a.token, nil
	}

	form := url.Values{}
	if a.username != "" {
		form.Set("grant_type", "password")
		form.Set("username", a.username)
		form.Set("password", a.password)
	} else {
		form.Set("grant_type", "client_credentials")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("reddit: build token request: %w", err)
	}
	req.SetBasicAuth(a.clientID, a.clientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("reddit: token request: %w", err)
	}
	defer resp.Body.Close()
	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("reddit: decode token: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || tr.Error != "" || tr.AccessToken == "" {
		return "", &APIError{StatusCode: resp.StatusCode, Status: resp.Status, Body: tr.Error}
	}
	a.token = tr.AccessToken
	a.tokenExp = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	return a.token, nil
}
