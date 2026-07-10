package reddit

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const sampleComments = `[
  {"kind":"Listing","data":{"children":[
    {"kind":"t3","data":{"id":"abc123","name":"t3_abc123","title":"Root post","author":"op","score":10,"created_utc":1700000000}}
  ]}},
  {"kind":"Listing","data":{"children":[
    {"kind":"t1","data":{
      "id":"c1","name":"t1_c1","author":"bob","body":"top comment","score":5,"created_utc":1700000100,"stickied":true,
      "replies":{"kind":"Listing","data":{"children":[
        {"kind":"t1","data":{"id":"c2","name":"t1_c2","author":"carol","body":"nested reply","score":2,"created_utc":1700000200,"replies":""}},
        {"kind":"more","data":{"id":"more1"}}
      ]}}
    }},
    {"kind":"t1","data":{"id":"c3","name":"t1_c3","author":"dave","body":"no replies","score":1,"created_utc":1700000300,"replies":""}},
    {"kind":"more","data":{"id":"more2"}}
  ]}}
]`

func TestComments(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/r/golang/comments/abc123.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(sampleComments))
	})
	res, err := c.Comments(context.Background(), "golang", "t3_abc123", ListingOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if res.Post.Title != "Root post" || res.Post.Author != "op" {
		t.Errorf("post wrong: %+v", res.Post)
	}
	if len(res.Comments) != 2 { // two t1 at top, "more" skipped
		t.Fatalf("got %d top comments, want 2", len(res.Comments))
	}
	top := res.Comments[0]
	if top.Author != "bob" || top.Body != "top comment" || !top.Stickied {
		t.Errorf("top comment wrong: %+v", top)
	}
	if got := top.Created().Unix(); got != 1700000100 {
		t.Errorf("Created().Unix() = %d", got)
	}
	if len(top.Replies) != 1 { // nested t1, "more" skipped
		t.Fatalf("got %d replies, want 1", len(top.Replies))
	}
	if top.Replies[0].Author != "carol" || top.Replies[0].Replies != nil {
		t.Errorf("nested reply wrong: %+v", top.Replies[0])
	}
	if res.Comments[1].Replies != nil {
		t.Error(`"" replies should decode to nil`)
	}
}

func TestCommentsWithoutSubreddit(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/comments/abc123.json" {
			t.Errorf("path = %q, want no-subreddit form", r.URL.Path)
		}
		w.Write([]byte(sampleComments))
	})
	if _, err := c.Comments(context.Background(), "", "abc123", ListingOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestCommentsEmptyID(t *testing.T) {
	c := NewClient()
	_, err := c.Comments(context.Background(), "golang", "t3_", ListingOptions{})
	if err == nil || !strings.Contains(err.Error(), "empty post id") {
		t.Fatalf("want empty-id error, got %v", err)
	}
}

func TestCommentsGetError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, err := c.Comments(context.Background(), "golang", "abc123", ListingOptions{}); err == nil {
		t.Fatal("want error on 404")
	}
}

func TestCommentsMalformedHalvesTolerated(t *testing.T) {
	// A 2-tuple whose halves are individually undecodable must not crash;
	// the result is simply empty. Post half is a JSON array (not a Listing
	// object) and comment half likewise.
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[[1,2],[3,4]]`))
	})
	res, err := c.Comments(context.Background(), "golang", "abc123", ListingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Post.ID != "" || len(res.Comments) != 0 {
		t.Errorf("malformed halves should yield empty result, got %+v", res)
	}
}

func TestCommentsEmptyArray(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	})
	res, err := c.Comments(context.Background(), "golang", "abc123", ListingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Post.ID != "" || len(res.Comments) != 0 {
		t.Errorf("empty array should yield empty result, got %+v", res)
	}
}

func TestCommentsTopLevelDecodeError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"not":"an array"}`))
	})
	if _, err := c.Comments(context.Background(), "golang", "abc123", ListingOptions{}); err == nil {
		t.Fatal("want decode error on non-array body")
	}
}

func TestDecodeRepliesBadJSON(t *testing.T) {
	// A replies value that is neither "" nor a valid Listing decodes to nil.
	if got := decodeReplies([]byte(`{bad`)); got != nil {
		t.Errorf("bad replies JSON should be nil, got %+v", got)
	}
	if got := decodeReplies(nil); got != nil {
		t.Errorf("nil replies should be nil, got %+v", got)
	}
}
