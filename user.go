// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// UserPosts fetches a page of a redditor's submissions (their "submitted" posts,
// not their comments), so a reader can follow a person the way it follows a
// subreddit. A leading "u/" or "/u/" on name is tolerated. sort is new / hot /
// top (blank → new, the natural order for a person's posts); Time scopes top.
// A blank name is rejected. Limit (Reddit clamps to 100) and After paginate.
// Works anonymously or with a session cookie / OAuth.
func (c *Client) UserPosts(ctx context.Context, name string, sort Sort, opts ListingOptions) (*Page, error) {
	name = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(name), "/u/"), "u/")
	if name == "" {
		return nil, &APIError{Status: "empty user name"}
	}
	if sort == "" {
		sort = SortNew
	}
	v := url.Values{}
	v.Set("sort", string(sort))
	if opts.Limit > 0 {
		v.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.After != "" {
		v.Set("after", opts.After)
	}
	if opts.Time != "" {
		v.Set("t", string(opts.Time))
	}
	var lr listingResponse
	if err := c.get(ctx, "/user/"+url.PathEscape(name)+"/submitted.json?"+v.Encode(), &lr); err != nil {
		return nil, err
	}
	return lr.toPage(), nil
}
