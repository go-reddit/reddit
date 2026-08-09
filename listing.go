package reddit

import (
	"context"
	"net/url"
	"strings"
	"time"
)

// Sort selects the ordering of a listing endpoint.
type Sort string

// Listing sort orders understood by Reddit. Best is only meaningful on the
// logged-out front page; the rest apply to any subreddit.
const (
	SortHot        Sort = "hot"
	SortNew        Sort = "new"
	SortTop        Sort = "top"
	SortRising     Sort = "rising"
	SortControvers Sort = "controversial"
	SortBest       Sort = "best"
)

// TimeRange scopes SortTop / SortControvers to a window. Ignored by other
// sorts.
type TimeRange string

// Time windows for the "top" and "controversial" sorts.
const (
	TimeHour  TimeRange = "hour"
	TimeDay   TimeRange = "day"
	TimeWeek  TimeRange = "week"
	TimeMonth TimeRange = "month"
	TimeYear  TimeRange = "year"
	TimeAll   TimeRange = "all"
)

// ListingOptions tunes a listing request. The zero value asks Reddit for its
// default page size starting at the top of the listing.
type ListingOptions struct {
	// Limit caps the number of posts returned (Reddit clamps to 100).
	Limit int
	// After is the fullname ("t3_xxxxxx") to page from; use [Page.After].
	After string
	// Time scopes SortTop / SortControvers; ignored otherwise.
	Time TimeRange
}

// Post is a single link or self-post ("t3" thing). Fields mirror the subset
// of Reddit's JSON that a reader UI needs; unmapped fields are dropped.
type Post struct {
	ID          string  `json:"id"`
	Fullname    string  `json:"name"` // e.g. "t3_abc123", used for paging/After
	Title       string  `json:"title"`
	Author      string  `json:"author"`
	Subreddit   string  `json:"subreddit"`
	Permalink   string  `json:"permalink"`
	URL         string  `json:"url"`
	Domain      string  `json:"domain"`
	SelfText    string  `json:"selftext"`
	Thumbnail   string  `json:"thumbnail"`
	Score       int     `json:"score"`
	Ups         int     `json:"ups"`
	NumComments int     `json:"num_comments"`
	CreatedUTC  float64 `json:"created_utc"`
	IsSelf      bool    `json:"is_self"`
	Over18      bool    `json:"over_18"`
	Stickied    bool    `json:"stickied"`
	Flair       string  `json:"link_flair_text"`
	// Media fields. Reddit exposes a post's image(s)/video through several shapes;
	// the Images / VideoURL accessors (see media.go) resolve them uniformly.
	PostHint      string                   `json:"post_hint"` // "image","hosted:video","rich:video","link",…
	IsVideo       bool                     `json:"is_video"`
	IsGallery     bool                     `json:"is_gallery"`
	Preview       postPreview              `json:"preview"`
	GalleryData   galleryData              `json:"gallery_data"`
	MediaMetadata map[string]mediaMetaItem `json:"media_metadata"`
	Media         postMedia                `json:"media"`
}

// Created returns the post's creation time in UTC.
func (p Post) Created() time.Time { return time.Unix(int64(p.CreatedUTC), 0).UTC() }

// FullPermalink joins the site-relative Permalink onto the canonical host so
// it is directly openable. Reddit's permalinks already begin with "/".
func (p Post) FullPermalink() string {
	if p.Permalink == "" {
		return ""
	}
	return DefaultBaseURL + p.Permalink
}

// HasThumbnail reports whether Thumbnail is a real image URL rather than one
// of Reddit's sentinel strings ("self", "default", "nsfw", "spoiler", "").
func (p Post) HasThumbnail() bool {
	switch p.Thumbnail {
	case "", "self", "default", "nsfw", "spoiler", "image":
		return false
	}
	return strings.HasPrefix(p.Thumbnail, "http")
}

// Page is a decoded listing: the posts plus the paging cursors Reddit
// returns. After is the fullname to pass back via [ListingOptions.After] to
// fetch the next page ("" when the listing is exhausted).
type Page struct {
	Posts  []Post
	After  string
	Before string
}

// thing is the generic "kind"+"data" envelope Reddit wraps every object in.
// For a listing of links, Kind is "t3" and Data decodes into a Post.
type thing struct {
	Kind string `json:"kind"`
	Data Post   `json:"data"`
}

// listingResponse is the top-level Listing envelope for a page of things.
type listingResponse struct {
	Data struct {
		After    string  `json:"after"`
		Before   string  `json:"before"`
		Children []thing `json:"children"`
	} `json:"data"`
}

func (lr listingResponse) toPage() *Page {
	p := &Page{
		After:  lr.Data.After,
		Before: lr.Data.Before,
		Posts:  make([]Post, 0, len(lr.Data.Children)),
	}
	for _, ch := range lr.Data.Children {
		if ch.Kind == "t3" {
			p.Posts = append(p.Posts, ch.Data)
		}
	}
	return p
}

// Subreddit fetches a page of posts from r/<name> under the given sort.
// A blank sort defaults to [SortHot]; a blank name is rejected. The special
// name "all" and "popular" resolve to Reddit's aggregate feeds.
func (c *Client) Subreddit(ctx context.Context, name string, sort Sort, opts ListingOptions) (*Page, error) {
	name = strings.TrimPrefix(strings.TrimSpace(name), "r/")
	if name == "" {
		return nil, &APIError{StatusCode: 0, Status: "empty subreddit name"}
	}
	if sort == "" {
		sort = SortHot
	}
	path := "/r/" + url.PathEscape(name) + "/" + string(sort) + ".json" + listingQuery(opts)
	var lr listingResponse
	if err := c.get(ctx, path, &lr); err != nil {
		return nil, err
	}
	return lr.toPage(), nil
}

// Frontpage fetches the logged-out front page under the given sort (blank
// defaults to [SortHot]). This is r/all-ish aggregate content, not a
// personalised feed (anonymous access has no account).
func (c *Client) Frontpage(ctx context.Context, sort Sort, opts ListingOptions) (*Page, error) {
	if sort == "" {
		sort = SortHot
	}
	path := "/" + string(sort) + ".json" + listingQuery(opts)
	var lr listingResponse
	if err := c.get(ctx, path, &lr); err != nil {
		return nil, err
	}
	return lr.toPage(), nil
}
