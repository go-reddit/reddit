// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestSearchPostsSiteWide(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search.json" {
			t.Errorf("path = %q, want site-wide /search.json", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("q") != "go generics" || q.Get("type") != "link" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		if q.Get("sort") != "new" || q.Get("limit") != "10" || q.Get("t") != "week" {
			t.Errorf("params = %q", r.URL.RawQuery)
		}
		if q.Get("restrict_sr") != "" {
			t.Errorf("site-wide search must not restrict_sr; got %q", q.Get("restrict_sr"))
		}
		_, _ = w.Write([]byte(sampleListing))
	})
	page, err := c.SearchPosts(context.Background(), "  go generics ", "", SearchNew, ListingOptions{Limit: 10, Time: TimeWeek})
	if err != nil {
		t.Fatal(err)
	}
	if page.After != "t3_next" || len(page.Posts) != 1 || page.Posts[0].Title != "Hello world" {
		t.Errorf("page = %+v", page)
	}
}

func TestSearchPostsRestrictedToSubreddit(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/r/golang/search.json" {
			t.Errorf("path = %q, want restricted /r/golang/search.json", r.URL.Path)
		}
		if r.URL.Query().Get("restrict_sr") != "1" {
			t.Errorf("restrict_sr = %q, want 1", r.URL.Query().Get("restrict_sr"))
		}
		if r.URL.Query().Get("after") != "t3_prev" {
			t.Errorf("after = %q", r.URL.Query().Get("after"))
		}
		_, _ = w.Write([]byte(`{"data":{"children":[]}}`))
	})
	if _, err := c.SearchPosts(context.Background(), "generics", "r/golang", "", ListingOptions{After: "t3_prev"}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchPostsEmptyQuery(t *testing.T) {
	_, err := NewClient().SearchPosts(context.Background(), "   ", "", "", ListingOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty search query") {
		t.Fatalf("want empty-query error, got %v", err)
	}
}

func TestSearchPostsPropagatesError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	if _, err := c.SearchPosts(context.Background(), "go", "", "", ListingOptions{}); err == nil {
		t.Fatal("want error on 403")
	}
}
