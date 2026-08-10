// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// SubredditInfo is a subreddit's metadata as returned by the search and listing
// endpoints (a Reddit "t5" thing). It carries what a discovery UI needs to show
// a result and let the user subscribe by Name.
type SubredditInfo struct {
	Name              string // display_name, e.g. "golang" (subscribe as r/<Name>)
	Title             string // human title
	PublicDescription string // short public blurb
	Subscribers       int64  // subscriber count
	Over18            bool   // NSFW flag
	URL               string // path, e.g. "/r/golang/"
	Type              string // subreddit_type: public / restricted / private
}

// subredditThing is the "kind"+"data" envelope for a t5 (subreddit) object.
type subredditThing struct {
	Kind string `json:"kind"`
	Data struct {
		DisplayName       string `json:"display_name"`
		Title             string `json:"title"`
		PublicDescription string `json:"public_description"`
		Subscribers       int64  `json:"subscribers"`
		Over18            bool   `json:"over_18"`
		URL               string `json:"url"`
		SubredditType     string `json:"subreddit_type"`
	} `json:"data"`
}

// info flattens a t5 envelope into the public SubredditInfo shape.
func (t subredditThing) info() SubredditInfo {
	d := t.Data
	return SubredditInfo{
		Name:              d.DisplayName,
		Title:             d.Title,
		PublicDescription: d.PublicDescription,
		Subscribers:       d.Subscribers,
		Over18:            d.Over18,
		URL:               d.URL,
		Type:              d.SubredditType,
	}
}

// subredditListing is the top-level Listing envelope for a page of subreddits.
type subredditListing struct {
	Data struct {
		After    string           `json:"after"`
		Before   string           `json:"before"`
		Children []subredditThing `json:"children"`
	} `json:"data"`
}

// SubredditPage is a page of subreddit search results. After is the fullname to
// pass back via [ListingOptions.After] for the next page ("" when exhausted).
type SubredditPage struct {
	Subreddits []SubredditInfo
	After      string
	Before     string
}

// SearchSubreddits searches Reddit for subreddits whose name, title or
// description matches query, returning a page of results. Subreddit names cannot
// be enumerated, so this search endpoint is how a UI discovers them; a caller
// can further filter the returned names/descriptions locally (e.g. by a regular
// expression). A blank query is rejected. ListingOptions.Limit (Reddit clamps to
// 100) and After paginate. Works anonymously or with a configured session cookie
// / OAuth.
func (c *Client) SearchSubreddits(ctx context.Context, query string, opts ListingOptions) (*SubredditPage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, &APIError{Status: "empty search query"}
	}
	v := url.Values{}
	v.Set("q", query)
	if opts.Limit > 0 {
		v.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.After != "" {
		v.Set("after", opts.After)
	}
	var lr subredditListing
	if err := c.get(ctx, "/subreddits/search.json?"+v.Encode(), &lr); err != nil {
		return nil, err
	}
	page := &SubredditPage{
		After:      lr.Data.After,
		Before:     lr.Data.Before,
		Subreddits: make([]SubredditInfo, 0, len(lr.Data.Children)),
	}
	for _, ch := range lr.Data.Children {
		if ch.Kind != "t5" {
			continue // skip anything that is not a subreddit
		}
		page.Subreddits = append(page.Subreddits, ch.info())
	}
	return page, nil
}

// mySubredditsMaxPages bounds MySubreddits' pagination so a misbehaving endpoint
// that always returns a non-empty After cannot loop forever; 100 pages of 100
// covers ~10k subscriptions, far beyond any real account.
const mySubredditsMaxPages = 100

// MySubreddits fetches every subreddit the authenticated user is subscribed to,
// paging through Reddit's /subreddits/mine/subscriber listing until it is
// exhausted, so a reader can import a logged-in account's subscriptions in one
// call. Authentication is required — a session cookie (see [WithSessionCookie])
// or OAuth — and an anonymous client is rejected before any request is made.
// Results come back in Reddit's own order (roughly by subscriber count). At most
// [mySubredditsMaxPages] pages are fetched.
func (c *Client) MySubreddits(ctx context.Context) ([]SubredditInfo, error) {
	if c.sessionCookie == "" && c.auth == nil {
		return nil, &APIError{Status: "authentication required: connect a Reddit account first"}
	}
	var out []SubredditInfo
	after := ""
	for page := 0; page < mySubredditsMaxPages; page++ {
		v := url.Values{}
		v.Set("limit", "100")
		if after != "" {
			v.Set("after", after)
		}
		var lr subredditListing
		if err := c.get(ctx, "/subreddits/mine/subscriber.json?"+v.Encode(), &lr); err != nil {
			return nil, err
		}
		for _, ch := range lr.Data.Children {
			if ch.Kind != "t5" {
				continue
			}
			out = append(out, ch.info())
		}
		if lr.Data.After == "" {
			break
		}
		after = lr.Data.After
	}
	return out, nil
}
