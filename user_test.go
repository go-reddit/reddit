// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestUserPosts(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/spez/submitted.json" {
			t.Errorf("path = %q, want /user/spez/submitted.json", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("sort") != "top" || q.Get("limit") != "5" || q.Get("t") != "week" || q.Get("after") != "t3_p" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(sampleListing))
	})
	// A "u/" prefix is tolerated.
	page, err := c.UserPosts(context.Background(), "u/spez", SortTop, ListingOptions{Limit: 5, After: "t3_p", Time: TimeWeek})
	if err != nil {
		t.Fatal(err)
	}
	if page.After != "t3_next" || len(page.Posts) != 1 || page.Posts[0].Author != "alice" {
		t.Errorf("page = %+v", page)
	}
}

func TestUserPostsDefaultsSortNew(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sort") != "new" {
			t.Errorf("default sort = %q, want new", r.URL.Query().Get("sort"))
		}
		if r.URL.Query().Get("limit") != "" || r.URL.Query().Get("after") != "" {
			t.Errorf("unexpected paging params: %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"data":{"children":[]}}`))
	})
	if _, err := c.UserPosts(context.Background(), "/u/spez", "", ListingOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestUserPostsEmptyName(t *testing.T) {
	_, err := NewClient().UserPosts(context.Background(), "  u/  ", SortNew, ListingOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty user name") {
		t.Fatalf("want empty-name error, got %v", err)
	}
}

func TestUserPostsPropagatesError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := c.UserPosts(context.Background(), "ghost", SortNew, ListingOptions{}); err == nil {
		t.Fatal("want error on 404")
	}
}
