// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

func TestPostImagesGallery(t *testing.T) {
	p := Post{
		IsGallery: true,
		GalleryData: galleryData{Items: []struct {
			MediaID string `json:"media_id"`
		}{{MediaID: "a"}, {MediaID: "b"}, {MediaID: "missing"}}},
		MediaMetadata: map[string]mediaMetaItem{
			"a": {Status: "valid", S: struct {
				U   string `json:"u"`
				GIF string `json:"gif"`
				MP4 string `json:"mp4"`
			}{U: "https://i.redd.it/a.jpg?s=1&amp;t=2"}},
			"b": {Status: "valid", S: struct {
				U   string `json:"u"`
				GIF string `json:"gif"`
				MP4 string `json:"mp4"`
			}{GIF: "https://i.redd.it/b.gif"}},
		},
	}
	got := p.Images()
	want := []string{"https://i.redd.it/a.jpg?s=1&t=2", "https://i.redd.it/b.gif"} // &amp; unescaped, order preserved, missing skipped
	if !reflect.DeepEqual(got, want) {
		t.Errorf("gallery Images() = %v, want %v", got, want)
	}
	if p.PreviewImage() != want[0] {
		t.Errorf("PreviewImage() = %q, want %q", p.PreviewImage(), want[0])
	}
}

func TestPostImagesPreviewAndDirectAndVideo(t *testing.T) {
	// Preview source wins over the direct URL when both are present.
	pv := Post{
		URL:      "https://example.com/page",
		PostHint: "image",
	}
	pv.Preview.Images = []struct {
		Source struct {
			URL string `json:"url"`
		} `json:"source"`
	}{{Source: struct {
		URL string `json:"url"`
	}{URL: "https://preview.redd.it/x.png?width=640&amp;crop=smart"}}}
	if got := p1(pv.Images()); got != "https://preview.redd.it/x.png?width=640&crop=smart" {
		t.Errorf("preview Images()[0] = %q", got)
	}

	// No preview, no gallery: fall back to a direct-image URL.
	direct := Post{URL: "https://i.imgur.com/abc.png"}
	if got := p1(direct.Images()); got != "https://i.imgur.com/abc.png" {
		t.Errorf("direct Images()[0] = %q", got)
	}

	// A non-image external link with no preview yields nothing.
	link := Post{URL: "https://example.com/article"}
	if len(link.Images()) != 0 || link.PreviewImage() != "" {
		t.Errorf("link post should have no images; got %v", link.Images())
	}

	// A reddit-hosted video: VideoURL returns the fallback; not a video → "".
	vid := Post{IsVideo: true}
	vid.Media.RedditVideo.FallbackURL = "https://v.redd.it/xyz/DASH_720.mp4?source=fallback&amp;x=1"
	if vid.VideoURL() != "https://v.redd.it/xyz/DASH_720.mp4?source=fallback&x=1" {
		t.Errorf("VideoURL() = %q", vid.VideoURL())
	}
	if (Post{IsVideo: false}).VideoURL() != "" {
		t.Error("non-video VideoURL() should be empty")
	}
}

func TestPostImagesSkipsNonHTTP(t *testing.T) {
	// A non-http(s) or empty preview URL is dropped.
	p := Post{}
	p.Preview.Images = []struct {
		Source struct {
			URL string `json:"url"`
		} `json:"source"`
	}{{Source: struct {
		URL string `json:"url"`
	}{URL: "data:image/png;base64,AAAA"}}, {Source: struct {
		URL string `json:"url"`
	}{URL: "   "}}}
	if imgs := p.Images(); len(imgs) != 0 {
		t.Errorf("non-http preview images should be dropped; got %v", imgs)
	}
}

// TestPostMediaDecodesFromJSON ensures the new fields parse from a real listing
// through the public Subreddit path.
func TestPostMediaDecodesFromJSON(t *testing.T) {
	const body = `{"data":{"children":[{"kind":"t3","data":{
		"id":"g1","title":"gallery","is_gallery":true,
		"gallery_data":{"items":[{"media_id":"m1"}]},
		"media_metadata":{"m1":{"status":"valid","s":{"u":"https://i.redd.it/m1.jpg"}}},
		"is_video":true,"media":{"reddit_video":{"fallback_url":"https://v.redd.it/g1/DASH_480.mp4"}},
		"post_hint":"hosted:video"
	}}]}}`
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	page, err := c.Subreddit(context.Background(), "x", SortHot, ListingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 1 {
		t.Fatalf("got %d posts", len(page.Posts))
	}
	p := page.Posts[0]
	if p.PostHint != "hosted:video" || !p.IsGallery || !p.IsVideo {
		t.Errorf("flags not decoded: %+v", p)
	}
	if got := p1(p.Images()); got != "https://i.redd.it/m1.jpg" {
		t.Errorf("Images()[0] = %q", got)
	}
	if p.VideoURL() != "https://v.redd.it/g1/DASH_480.mp4" {
		t.Errorf("VideoURL() = %q", p.VideoURL())
	}
}

// p1 returns the first element of s, or "" when empty.
func p1(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
