// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestFriends(t *testing.T) {
	// A UserList envelope with two followed accounts; the blank-name child is
	// skipped so a malformed entry never yields a u/ subscription with no name.
	body := `{"kind":"UserList","data":{"children":[
	  {"name":"spez","id":"t2_1w72","date":1607980800.0,"rel_id":"r9_abc"},
	  {"name":"","id":"t2_zzz","date":0,"rel_id":"r9_zzz"},
	  {"name":"kn0thing","id":"t2_2v4","date":1600000000.0,"rel_id":"r9_def"}
	]}}`
	srv := newRawServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/me/friends" {
			t.Errorf("path = %q, want /api/v1/me/friends", r.URL.Path)
		}
		if r.Header.Get("Cookie") != "reddit_session=sess" {
			t.Errorf("authenticated request must carry the session Cookie, got %q", r.Header.Get("Cookie"))
		}
		_, _ = w.Write([]byte(body))
	}))
	c := NewClient(WithBaseURL(srv), WithUserAgent("test/1.0"), WithSessionCookie("sess"))

	friends, err := c.Friends(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(friends) != 2 {
		t.Fatalf("friends = %d, want 2 (blank-name skipped)", len(friends))
	}
	if friends[0].Name != "spez" || friends[0].ID != "t2_1w72" ||
		friends[0].Date != 1607980800.0 || friends[0].RelID != "r9_abc" {
		t.Errorf("friend[0] = %+v", friends[0])
	}
	if friends[1].Name != "kn0thing" {
		t.Errorf("friend[1] = %+v", friends[1])
	}
}

func TestFriendsRequiresAuth(t *testing.T) {
	// An anonymous client is rejected before any request is made.
	_, err := NewClient().Friends(context.Background())
	if err == nil || !strings.Contains(err.Error(), "authentication required") {
		t.Fatalf("want authentication-required error, got %v", err)
	}
}

func TestFriendsPropagatesError(t *testing.T) {
	srv := newRawServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	c := NewClient(WithBaseURL(srv), WithUserAgent("test/1.0"), WithSessionCookie("sess"))
	if _, err := c.Friends(context.Background()); err == nil {
		t.Fatal("want error on 403")
	}
}
