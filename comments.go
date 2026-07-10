package reddit

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// Comment is a single "t1" thing in a comment tree. Replies is populated
// recursively; leaf comments (or the truncated "load more" frontier) have a
// nil slice.
type Comment struct {
	ID         string    `json:"id"`
	Fullname   string    `json:"name"`
	Author     string    `json:"author"`
	Body       string    `json:"body"`
	Score      int       `json:"score"`
	CreatedUTC float64   `json:"created_utc"`
	Stickied   bool      `json:"stickied"`
	Replies    []Comment `json:"-"`
}

// Created returns the comment's creation time in UTC.
func (c Comment) Created() time.Time { return time.Unix(int64(c.CreatedUTC), 0).UTC() }

// commentThing is the "kind"+"data" envelope for a comment. Replies is a
// raw message because Reddit encodes it either as "" (no replies) or as a
// nested Listing, which the two forms can't share a static type for.
type commentThing struct {
	Kind string `json:"kind"`
	Data struct {
		Comment
		Replies json.RawMessage `json:"replies"`
	} `json:"data"`
}

type commentListing struct {
	Data struct {
		Children []commentThing `json:"children"`
	} `json:"data"`
}

// decodeReplies parses the polymorphic "replies" field: an empty string when
// there are none, otherwise a nested comment Listing. "more" things (the
// collapsed comment frontier) are skipped rather than followed.
func decodeReplies(raw json.RawMessage) []Comment {
	if len(raw) == 0 {
		return nil
	}
	// Reddit uses "" (a JSON string) to mean "no replies".
	if raw[0] == '"' {
		return nil
	}
	var cl commentListing
	if err := json.Unmarshal(raw, &cl); err != nil {
		return nil
	}
	return flattenComments(cl.Data.Children)
}

func flattenComments(children []commentThing) []Comment {
	out := make([]Comment, 0, len(children))
	for _, ch := range children {
		if ch.Kind != "t1" {
			continue // skip "more" frontier markers
		}
		c := ch.Data.Comment
		c.Replies = decodeReplies(ch.Data.Replies)
		out = append(out, c)
	}
	return out
}

// PostWithComments bundles a resolved post with its (partial) comment tree,
// as returned by the /comments/<id> endpoint.
type PostWithComments struct {
	Post     Post
	Comments []Comment
}

// Comments fetches a post and its comment tree. The comments endpoint returns
// a two-element array: [ Listing(the post), Listing(the comments) ]. subreddit
// may be blank (Reddit resolves the post by id alone) but supplying it avoids
// a redirect. id is the base-36 post id ("abc123"), with or without the "t3_"
// prefix.
func (c *Client) Comments(ctx context.Context, subreddit, id string, opts ListingOptions) (*PostWithComments, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), "t3_")
	if id == "" {
		return nil, &APIError{StatusCode: 0, Status: "empty post id"}
	}
	var path string
	if sr := strings.TrimPrefix(strings.TrimSpace(subreddit), "r/"); sr != "" {
		path = "/r/" + url.PathEscape(sr) + "/comments/" + url.PathEscape(id) + ".json"
	} else {
		path = "/comments/" + url.PathEscape(id) + ".json"
	}
	path += listingQuery(opts)

	// The endpoint returns a heterogeneous 2-tuple, so decode into raw
	// messages first, then each half with its own typed decoder.
	var pair []json.RawMessage
	if err := c.get(ctx, path, &pair); err != nil {
		return nil, err
	}
	res := &PostWithComments{}
	if len(pair) > 0 {
		var lr listingResponse
		if err := json.Unmarshal(pair[0], &lr); err == nil {
			if p := lr.toPage(); len(p.Posts) > 0 {
				res.Post = p.Posts[0]
			}
		}
	}
	if len(pair) > 1 {
		var cl commentListing
		if err := json.Unmarshal(pair[1], &cl); err == nil {
			res.Comments = flattenComments(cl.Data.Children)
		}
	}
	return res, nil
}
