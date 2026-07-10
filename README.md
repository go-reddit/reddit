# go-reddit/reddit

A dependency-free (stdlib-only, **CGO=0**) Go client for Reddit's public JSON
API. It powers the [go-reddit/reader](https://github.com/go-reddit/reader)
macOS app and the [go-ruby-reddit](https://github.com/go-ruby-reddit) Ruby
binding, but stands on its own.

- **Zero dependencies** — only the Go standard library, so it builds for every
  Go target (all six 64-bit arches) with `CGO_ENABLED=0`.
- **Anonymous or OAuth** — read public listings anonymously, or authenticate
  with the app-only (`client_credentials`) or script (`password`) grant.
  Reddit increasingly blocks the anonymous `.json` endpoints with a 403; OAuth
  is the reliable path for server-side and datacenter callers.
- **100% test coverage**, including every error branch.

## Install

```sh
go get github.com/go-reddit/reddit
```

## Usage

```go
package main

import (
	"context"
	"fmt"

	"github.com/go-reddit/reddit"
)

func main() {
	// A descriptive User-Agent is required; Reddit rate-limits generic ones.
	c := reddit.NewClient(reddit.WithUserAgent("myapp/1.0 (by /u/you)"))

	page, err := c.Subreddit(context.Background(), "golang", reddit.SortHot,
		reddit.ListingOptions{Limit: 25})
	if err != nil {
		panic(err)
	}
	for _, p := range page.Posts {
		fmt.Printf("%5d  %s  (u/%s)\n", p.Score, p.Title, p.Author)
	}

	// Page with the returned cursor:
	next, _ := c.Subreddit(context.Background(), "golang", reddit.SortHot,
		reddit.ListingOptions{Limit: 25, After: page.After})
	_ = next
}
```

### OAuth (recommended for anything server-side)

Register an app at <https://www.reddit.com/prefs/apps> to get a client id and
secret, then:

```go
// Application-only: read public content, no user account.
c := reddit.NewClient(
	reddit.WithUserAgent("myapp/1.0 (by /u/you)"),
	reddit.WithOAuth(clientID, clientSecret),
)

// Or the "script app" grant, acting as a specific user:
c = reddit.NewClient(
	reddit.WithUserAgent("myapp/1.0 (by /u/you)"),
	reddit.WithOAuthScript(clientID, clientSecret, username, password),
)
```

The client fetches and caches the bearer token automatically, refreshing it
before expiry, and routes requests through `oauth.reddit.com`.

### Comments

```go
res, _ := c.Comments(context.Background(), "golang", "abc123",
	reddit.ListingOptions{Limit: 100})
fmt.Println(res.Post.Title)
for _, cm := range res.Comments {   // Replies nested recursively
	fmt.Printf("u/%s: %s\n", cm.Author, cm.Body)
}
```

## API

| Method | Endpoint |
|--------|----------|
| `Subreddit(ctx, name, sort, opts)` | `r/<name>/<sort>.json` |
| `Frontpage(ctx, sort, opts)` | `/<sort>.json` (logged-out front page) |
| `Comments(ctx, subreddit, id, opts)` | `r/<sub>/comments/<id>.json` |

Sorts: `SortHot`, `SortNew`, `SortTop`, `SortRising`, `SortControvers`,
`SortBest`. Time windows for top/controversial: `TimeHour` … `TimeAll`.

Non-2xx responses surface as a typed `*reddit.APIError` carrying the status
code, so callers can distinguish a 429 (rate limit) from a 403 (blocked) or a
404 (gone).

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-reddit/reddit authors.
