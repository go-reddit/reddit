// Copyright (c) the go-reddit/reddit authors.
// SPDX-License-Identifier: BSD-3-Clause

package reddit

import (
	"html"
	"strings"
)

// postPreview is Reddit's resolved preview block: one entry per image, whose
// Source is the full-resolution version. Reddit provides it for direct image
// posts, external links (the article's card image) and as a video poster.
type postPreview struct {
	Images []struct {
		Source struct {
			URL string `json:"url"`
		} `json:"source"`
	} `json:"images"`
}

// galleryData lists a gallery post's items in display order; each references a
// media_metadata entry by id.
type galleryData struct {
	Items []struct {
		MediaID string `json:"media_id"`
	} `json:"items"`
}

// mediaMetaItem is one entry of a gallery's media_metadata map. S is the largest
// source; a still image is in U, an animated one in GIF/MP4.
type mediaMetaItem struct {
	Status string `json:"status"`
	S      struct {
		U   string `json:"u"`
		GIF string `json:"gif"`
		MP4 string `json:"mp4"`
	} `json:"s"`
}

// postMedia carries a reddit-hosted video's playable fallback.
type postMedia struct {
	RedditVideo struct {
		FallbackURL string `json:"fallback_url"`
	} `json:"reddit_video"`
}

// cleanMediaURL trims, HTML-unescapes (Reddit encodes & as &amp; in these URLs)
// and validates a media URL, returning "" for a non-http(s) or empty value.
func cleanMediaURL(u string) string {
	u = html.UnescapeString(strings.TrimSpace(u))
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return ""
}

// directImageExts are the URL suffixes that denote an image by extension.
var directImageExts = []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}

// isDirectImageURL reports whether u points straight at an image, by extension
// or by a known image host.
func isDirectImageURL(u string) bool {
	l := strings.ToLower(u)
	for _, ext := range directImageExts {
		if strings.HasSuffix(l, ext) {
			return true
		}
	}
	return strings.Contains(l, "i.redd.it/") || strings.Contains(l, "i.imgur.com/")
}

// Images returns the post's display image URLs in display order, best-effort and
// HTML-unescaped, de-duplicated: a gallery's items first (in order), else the
// resolved preview source image(s), else a direct-image URL. It is empty when
// the post carries no image the caller can show. A reddit-hosted video's poster
// comes through the preview, so a video post returns its poster here too (use
// [Post.VideoURL] for the playable stream).
func (p Post) Images() []string {
	var out []string
	seen := map[string]bool{}
	add := func(raw string) {
		u := cleanMediaURL(raw)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}

	if p.IsGallery {
		for _, it := range p.GalleryData.Items {
			m, ok := p.MediaMetadata[it.MediaID]
			if !ok {
				continue
			}
			switch {
			case m.S.U != "":
				add(m.S.U)
			case m.S.GIF != "":
				add(m.S.GIF)
			}
		}
		if len(out) > 0 {
			return out
		}
	}

	for _, im := range p.Preview.Images {
		add(im.Source.URL)
	}
	if len(out) > 0 {
		return out
	}

	if isDirectImageURL(p.URL) {
		add(p.URL)
	}
	return out
}

// PreviewImage returns the single best display image for the post (the first of
// [Post.Images]), or "" when there is none.
func (p Post) PreviewImage() string {
	if imgs := p.Images(); len(imgs) > 0 {
		return imgs[0]
	}
	return ""
}

// VideoURL returns the reddit-hosted video's playable MP4 fallback URL, or ""
// when the post is not a reddit-hosted video.
func (p Post) VideoURL() string {
	if !p.IsVideo {
		return ""
	}
	return cleanMediaURL(p.Media.RedditVideo.FallbackURL)
}
