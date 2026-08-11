// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import "context"

// Friend is one redditor the authenticated account follows (Reddit's "friends"
// list). Name is the username — subscribe to a person's submissions as
// u/<Name>. ID is their "t2_…" fullname, Date the Unix time (seconds) the
// friendship was created, and RelID the friendship relationship id.
type Friend struct {
	Name  string  // username (follow as u/<Name>)
	ID    string  // account fullname, e.g. "t2_1w72"
	Date  float64 // friendship creation time, Unix seconds
	RelID string  // relationship id
}

// userListThing is one entry of the "UserList" envelope Reddit returns from the
// friends endpoint: a flat object (no kind/data wrapper) per redditor.
type userListThing struct {
	Name  string  `json:"name"`
	ID    string  `json:"id"`
	Date  float64 `json:"date"`
	RelID string  `json:"rel_id"`
}

// userList is the "UserList" envelope: {"kind":"UserList","data":{"children":[…]}}.
type userList struct {
	Kind string `json:"kind"`
	Data struct {
		Children []userListThing `json:"children"`
	} `json:"data"`
}

// Friends fetches the redditors the authenticated account follows, via
// GET /api/v1/me/friends. Reddit returns a single "UserList" thing whose
// data.children each name one followed account, so a reader can import the
// people a logged-in account follows the same way it imports its subreddits.
// Authentication is required — a session cookie (see [WithSessionCookie]) or
// OAuth (the "mysubreddits" scope) — and an anonymous client is rejected before
// any request is made. The list is unpaginated: Reddit returns the account's
// full friends list in one response.
func (c *Client) Friends(ctx context.Context) ([]Friend, error) {
	if c.sessionCookie == "" && c.auth == nil {
		return nil, &APIError{Status: "authentication required: connect a Reddit account first"}
	}
	var ul userList
	if err := c.get(ctx, "/api/v1/me/friends", &ul); err != nil {
		return nil, err
	}
	out := make([]Friend, 0, len(ul.Data.Children))
	for _, ch := range ul.Data.Children {
		if ch.Name == "" {
			continue
		}
		out = append(out, Friend{Name: ch.Name, ID: ch.ID, Date: ch.Date, RelID: ch.RelID})
	}
	return out, nil
}
