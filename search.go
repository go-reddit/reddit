// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// SearchSort selects how post-search results are ordered.
type SearchSort string

const (
	// SearchRelevance is Reddit's default search order.
	SearchRelevance SearchSort = "relevance"
	// SearchHot / SearchTop / SearchNew / SearchComments mirror the feed sorts.
	SearchHot      SearchSort = "hot"
	SearchTop      SearchSort = "top"
	SearchNew      SearchSort = "new"
	SearchComments SearchSort = "comments"
)

// SearchPosts searches Reddit for posts (links and self posts) matching query,
// returning a page of posts (the same shape a listing returns) so a search can
// be shown or followed as a feed. When subreddit is non-empty the search is
// restricted to that subreddit; otherwise it is site-wide. sort selects the
// order (blank → relevance). A blank query is rejected. Limit (Reddit clamps to
// 100) and After paginate. Works anonymously or with a session cookie / OAuth.
func (c *Client) SearchPosts(ctx context.Context, query, subreddit string, sort SearchSort, opts ListingOptions) (*Page, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, &APIError{Status: "empty search query"}
	}
	v := url.Values{}
	v.Set("q", query)
	v.Set("type", "link") // posts, not subreddits/users
	if sort != "" {
		v.Set("sort", string(sort))
	}
	if opts.Limit > 0 {
		v.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.After != "" {
		v.Set("after", opts.After)
	}
	if opts.Time != "" {
		v.Set("t", string(opts.Time))
	}
	path := "/search.json"
	if sub := strings.TrimPrefix(strings.TrimSpace(subreddit), "r/"); sub != "" {
		path = "/r/" + url.PathEscape(sub) + "/search.json"
		v.Set("restrict_sr", "1")
	}
	var lr listingResponse
	if err := c.get(ctx, path+"?"+v.Encode(), &lr); err != nil {
		return nil, err
	}
	return lr.toPage(), nil
}
