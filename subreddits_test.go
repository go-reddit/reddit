// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const sampleSubredditSearch = `{"data":{"after":"t5_next","before":null,"children":[
  {"kind":"t5","data":{"display_name":"golang","title":"The Go Programming Language","public_description":"Ask questions and post articles about Go.","subscribers":300000,"over_18":false,"url":"/r/golang/","subreddit_type":"public"}},
  {"kind":"t5","data":{"display_name":"rust","title":"Rust","public_description":"A place for Rustaceans.","subscribers":250000,"over_18":false,"url":"/r/rust/","subreddit_type":"public"}},
  {"kind":"t3","data":{"title":"not a subreddit"}}
]}}`

func TestSearchSubreddits(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subreddits/search.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "go lang" {
			t.Errorf("q = %q", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("limit") != "25" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		if r.URL.Query().Get("after") != "t5_prev" {
			t.Errorf("after = %q", r.URL.Query().Get("after"))
		}
		_, _ = w.Write([]byte(sampleSubredditSearch))
	})
	page, err := c.SearchSubreddits(context.Background(), "  go lang  ", ListingOptions{Limit: 25, After: "t5_prev"})
	if err != nil {
		t.Fatal(err)
	}
	if page.After != "t5_next" {
		t.Errorf("After = %q, want t5_next", page.After)
	}
	if len(page.Subreddits) != 2 { // the t3 child is filtered out
		t.Fatalf("got %d subreddits, want 2", len(page.Subreddits))
	}
	g := page.Subreddits[0]
	if g.Name != "golang" || g.Title != "The Go Programming Language" || g.Subscribers != 300000 {
		t.Errorf("first result wrong: %+v", g)
	}
	if g.URL != "/r/golang/" || g.Type != "public" || g.Over18 {
		t.Errorf("first result flags wrong: %+v", g)
	}
	if !strings.Contains(g.PublicDescription, "Go") {
		t.Errorf("description = %q", g.PublicDescription)
	}
}

func TestSearchSubredditsEmptyQuery(t *testing.T) {
	_, err := NewClient().SearchSubreddits(context.Background(), "   ", ListingOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty search query") {
		t.Fatalf("want empty-query error, got %v", err)
	}
}

func TestSearchSubredditsMinimalQuery(t *testing.T) {
	// No limit/after set: only q is sent.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "" || r.URL.Query().Get("after") != "" {
			t.Errorf("unexpected paging params: %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"data":{"children":[]}}`))
	})
	page, err := c.SearchSubreddits(context.Background(), "go", ListingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Subreddits) != 0 {
		t.Errorf("got %d, want 0", len(page.Subreddits))
	}
}

func TestSearchSubredditsPropagatesError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	if _, err := c.SearchSubreddits(context.Background(), "go", ListingOptions{}); err == nil {
		t.Fatal("want error on 403")
	}
}

func TestMySubreddits(t *testing.T) {
	// Two pages: the first carries an After cursor, the second exhausts it. The
	// t3 child is skipped, and the second request must echo the cursor back.
	page1 := `{"data":{"after":"t5_next","children":[
	  {"kind":"t5","data":{"display_name":"golang","subscribers":300000,"url":"/r/golang/","subreddit_type":"public"}},
	  {"kind":"t3","data":{"title":"not a subreddit"}}
	]}}`
	page2 := `{"data":{"after":null,"children":[
	  {"kind":"t5","data":{"display_name":"rust","subscribers":250000,"url":"/r/rust/","subreddit_type":"public"}}
	]}}`
	calls := 0
	srv := newRawServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subreddits/mine/subscriber.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("limit = %q, want 100", r.URL.Query().Get("limit"))
		}
		if r.Header.Get("Cookie") == "" {
			t.Error("authenticated request must carry the session Cookie")
		}
		calls++
		if calls == 1 {
			if r.URL.Query().Get("after") != "" {
				t.Errorf("first page must send no after, got %q", r.URL.Query().Get("after"))
			}
			_, _ = w.Write([]byte(page1))
			return
		}
		if r.URL.Query().Get("after") != "t5_next" {
			t.Errorf("second page after = %q, want t5_next", r.URL.Query().Get("after"))
		}
		_, _ = w.Write([]byte(page2))
	}))
	c := NewClient(WithBaseURL(srv), WithUserAgent("test/1.0"), WithSessionCookie("sess"))

	subs, err := c.MySubreddits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (paginated)", calls)
	}
	if len(subs) != 2 || subs[0].Name != "golang" || subs[1].Name != "rust" {
		t.Fatalf("subs = %+v, want [golang rust]", subs)
	}
	if subs[0].URL != "/r/golang/" || subs[0].Subscribers != 300000 {
		t.Errorf("golang mapping wrong: %+v", subs[0])
	}
}

func TestMySubredditsRequiresAuth(t *testing.T) {
	// An anonymous client is rejected before any request is made.
	_, err := NewClient().MySubreddits(context.Background())
	if err == nil || !strings.Contains(err.Error(), "authentication required") {
		t.Fatalf("want authentication-required error, got %v", err)
	}
}

func TestMySubredditsPropagatesError(t *testing.T) {
	srv := newRawServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	c := NewClient(WithBaseURL(srv), WithUserAgent("test/1.0"), WithSessionCookie("sess"))
	if _, err := c.MySubreddits(context.Background()); err == nil {
		t.Fatal("want error on 403")
	}
}
