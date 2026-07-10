package reddit

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const sampleListing = `{
  "kind": "Listing",
  "data": {
    "after": "t3_next",
    "before": null,
    "children": [
      {"kind": "t3", "data": {
        "id": "abc123", "name": "t3_abc123", "title": "Hello world",
        "author": "alice", "subreddit": "golang", "permalink": "/r/golang/comments/abc123/hello/",
        "url": "https://go.dev", "domain": "go.dev", "selftext": "",
        "thumbnail": "https://b.thumbs.redditmedia.com/x.jpg",
        "score": 42, "ups": 42, "num_comments": 7, "created_utc": 1700000000,
        "is_self": false, "over_18": false, "stickied": true, "link_flair_text": "News"}},
      {"kind": "t1", "data": {"id": "skip", "body": "not a post"}}
    ]
  }
}`

func TestSubreddit(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/r/golang/top.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("limit") != "5" || r.URL.Query().Get("t") != "week" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		w.Write([]byte(sampleListing))
	})
	page, err := c.Subreddit(context.Background(), "r/golang", SortTop, ListingOptions{Limit: 5, Time: TimeWeek})
	if err != nil {
		t.Fatal(err)
	}
	if page.After != "t3_next" {
		t.Errorf("After = %q", page.After)
	}
	if len(page.Posts) != 1 { // t1 child filtered out
		t.Fatalf("got %d posts, want 1", len(page.Posts))
	}
	p := page.Posts[0]
	if p.Title != "Hello world" || p.Author != "alice" || p.Score != 42 || p.NumComments != 7 {
		t.Errorf("post fields wrong: %+v", p)
	}
	if !p.Stickied || p.Flair != "News" {
		t.Errorf("flags/flair wrong: %+v", p)
	}
	if got := p.Created().Year(); got != 2023 {
		t.Errorf("Created().Year() = %d, want 2023", got)
	}
	if p.FullPermalink() != DefaultBaseURL+"/r/golang/comments/abc123/hello/" {
		t.Errorf("FullPermalink = %q", p.FullPermalink())
	}
	if !p.HasThumbnail() {
		t.Error("HasThumbnail() = false, want true")
	}
}

func TestSubredditDefaultsSortHot(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/hot.json") {
			t.Errorf("path = %q, want hot.json", r.URL.Path)
		}
		w.Write([]byte(`{"data":{"children":[]}}`))
	})
	if _, err := c.Subreddit(context.Background(), "golang", "", ListingOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestSubredditEmptyName(t *testing.T) {
	c := NewClient()
	_, err := c.Subreddit(context.Background(), "  r/  ", SortHot, ListingOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty subreddit") {
		t.Fatalf("want empty-name error, got %v", err)
	}
}

func TestSubredditPropagatesGetError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	if _, err := c.Subreddit(context.Background(), "golang", SortHot, ListingOptions{}); err == nil {
		t.Fatal("want error on 403")
	}
}

func TestFrontpage(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/best.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(sampleListing))
	})
	page, err := c.Frontpage(context.Background(), SortBest, ListingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 1 {
		t.Fatalf("got %d posts", len(page.Posts))
	}
}

func TestFrontpageDefaultsSortHot(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hot.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{"data":{"children":[]}}`))
	})
	if _, err := c.Frontpage(context.Background(), "", ListingOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestFrontpageError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.Frontpage(context.Background(), SortHot, ListingOptions{}); err == nil {
		t.Fatal("want error on 500")
	}
}

func TestPostHelpers(t *testing.T) {
	var empty Post
	if empty.FullPermalink() != "" {
		t.Error("empty permalink should map to empty string")
	}
	for _, thumb := range []string{"", "self", "default", "nsfw", "spoiler", "image", "notaurl"} {
		if (Post{Thumbnail: thumb}).HasThumbnail() {
			t.Errorf("HasThumbnail(%q) = true, want false", thumb)
		}
	}
	if !(Post{Thumbnail: "http://x/y.png"}).HasThumbnail() {
		t.Error("real thumbnail should be true")
	}
}
