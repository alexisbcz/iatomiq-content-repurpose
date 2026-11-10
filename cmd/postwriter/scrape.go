package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const scBase = "https://api.scrapecreators.com"

// scClient is a minimal ScrapeCreators client (1 credit per call).
type scClient struct {
	key  string
	http *http.Client
}

func (c *scClient) get(ctx context.Context, path string, q url.Values, out any) error {
	u := scBase + path + "?" + q.Encode()
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("x-api-key", c.key)
		res, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if res.StatusCode == 429 || res.StatusCode >= 500 {
			lastErr = fmt.Errorf("%s: HTTP %d", path, res.StatusCode)
			continue
		}
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("%s: HTTP %d: %.300s", path, res.StatusCode, body)
		}
		return json.Unmarshal(body, out)
	}
	return lastErr
}

// ---------- TikTok ----------

type urlList struct {
	URLList []string `json:"url_list"`
}

type tiktokAweme struct {
	AwemeID    string `json:"aweme_id"`
	Desc       string `json:"desc"`
	URL        string `json:"url"`
	Statistics struct {
		PlayCount    int64 `json:"play_count"`
		DiggCount    int64 `json:"digg_count"`
		CommentCount int64 `json:"comment_count"`
		ShareCount   int64 `json:"share_count"`
		CollectCount int64 `json:"collect_count"`
	} `json:"statistics"`
	Author struct {
		Nickname string `json:"nickname"`
		UniqueID string `json:"unique_id"`
	} `json:"author"`
	Video struct {
		Duration            int64   `json:"duration"` // ms
		PlayAddr            urlList `json:"play_addr"`
		DownloadAddr        urlList `json:"download_addr"`
		DownloadNoWatermark urlList `json:"download_no_watermark_addr"`
	} `json:"video"`
	ImagePostInfo json.RawMessage `json:"image_post_info"`
}

func (a tiktokAweme) videoURLs() []string {
	var out []string
	for _, l := range [][]string{a.Video.DownloadNoWatermark.URLList, a.Video.PlayAddr.URLList, a.Video.DownloadAddr.URLList} {
		out = append(out, l...)
	}
	return out
}

// TikTokVideo is what the agent gets to know about the source video.
type TikTokVideo struct {
	Aweme      tiktokAweme
	Transcript string
}

func (c *scClient) tiktokVideo(ctx context.Context, videoURL string) (tiktokAweme, error) {
	var r struct {
		AwemeDetail tiktokAweme `json:"aweme_detail"`
	}
	err := c.get(ctx, "/v2/tiktok/video", url.Values{"url": {videoURL}, "trim": {"true"}}, &r)
	if err == nil && r.AwemeDetail.AwemeID == "" {
		err = fmt.Errorf("no video found at %s", videoURL)
	}
	return r.AwemeDetail, err
}

// topVideos returns up to n of the account's most-played real videos (no
// slideshows), looking through up to `pages` pages of its latest posts.
func (c *scClient) topVideos(ctx context.Context, handle string, n, pages int) ([]string, error) {
	var all []tiktokAweme
	cursor := ""
	for p := 0; p < pages; p++ {
		q := url.Values{"handle": {handle}, "trim": {"true"}}
		if cursor != "" {
			q.Set("max_cursor", cursor)
		}
		var r struct {
			AwemeList []tiktokAweme   `json:"aweme_list"`
			HasMore   json.RawMessage `json:"has_more"`
			MaxCursor json.Number     `json:"max_cursor"`
		}
		if err := c.get(ctx, "/v3/tiktok/profile/videos", q, &r); err != nil {
			if len(all) > 0 {
				break
			}
			return nil, err
		}
		all = append(all, r.AwemeList...)
		hm := strings.Trim(string(r.HasMore), `"`)
		if (hm != "1" && hm != "true") || r.MaxCursor.String() == "" || r.MaxCursor.String() == cursor {
			break
		}
		cursor = r.MaxCursor.String()
	}
	seen := map[string]bool{}
	var vids []tiktokAweme
	for _, a := range all {
		if seen[a.AwemeID] || (len(a.ImagePostInfo) > 0 && string(a.ImagePostInfo) != "null") {
			continue
		}
		seen[a.AwemeID] = true
		vids = append(vids, a)
	}
	sort.Slice(vids, func(i, j int) bool { return vids[i].Statistics.PlayCount > vids[j].Statistics.PlayCount })
	var out []string
	for _, a := range vids {
		if len(out) == n {
			break
		}
		u := a.URL
		if u == "" {
			u = fmt.Sprintf("https://www.tiktok.com/@%s/video/%s", handle, a.AwemeID)
		}
		out = append(out, u)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no videos found on @%s", handle)
	}
	return out, nil
}

var vttTimestamp = regexp.MustCompile(`^\d{2}:\d{2}[:.]\d{2}.*-->`)

func (c *scClient) tiktokTranscript(ctx context.Context, videoURL string) (string, error) {
	var r struct {
		Transcript string `json:"transcript"`
	}
	if err := c.get(ctx, "/v1/tiktok/video/transcript", url.Values{"url": {videoURL}}, &r); err != nil {
		return "", err
	}
	// Strip WEBVTT header and timestamps, keep the spoken lines.
	var lines []string
	for _, l := range strings.Split(r.Transcript, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || l == "WEBVTT" || vttTimestamp.MatchString(l) {
			continue
		}
		if len(lines) > 0 && lines[len(lines)-1] == l {
			continue
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, " "), nil
}

// ---------- LinkedIn style corpus ----------

// StylePost is one LinkedIn post used as a voice reference.
type StylePost struct {
	URL      string `json:"url"`
	Date     string `json:"date"`
	Text     string `json:"text"`
	Likes    int64  `json:"likes"`
	Comments int64  `json:"comments"`
}

type StyleCorpus struct {
	Name    string      `json:"name"`
	Profile string      `json:"profile"`
	Fetched time.Time   `json:"fetched"`
	Posts   []StylePost `json:"posts"`
}

// loadStyleCorpus fetches the profile's recent posts plus their engagement,
// sorted by likes, and caches the result (it costs 1 + N credits).
func (c *scClient) loadStyleCorpus(ctx context.Context, profileURL, cacheDir string, refresh bool, logf func(string, ...any)) (*StyleCorpus, error) {
	slug := strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(profileURL), "-"), "-")
	cachePath := filepath.Join(cacheDir, "style-"+slug+".json")
	if !refresh {
		if data, err := os.ReadFile(cachePath); err == nil {
			var sc StyleCorpus
			if json.Unmarshal(data, &sc) == nil && len(sc.Posts) > 0 {
				logf("style: using cached corpus %s (%d posts, fetched %s)", cachePath, len(sc.Posts), sc.Fetched.Format("2006-01-02"))
				return &sc, nil
			}
		}
	}

	var prof struct {
		Name        string `json:"name"`
		RecentPosts []struct {
			Link          string `json:"link"`
			Title         string `json:"title"`
			DatePublished string `json:"datePublished"`
		} `json:"recentPosts"`
	}
	if err := c.get(ctx, "/v1/linkedin/profile", url.Values{"url": {profileURL}}, &prof); err != nil {
		return nil, fmt.Errorf("linkedin profile: %w", err)
	}
	sc := &StyleCorpus{Name: prof.Name, Profile: profileURL, Fetched: time.Now().UTC()}
	for _, p := range prof.RecentPosts {
		sp := StylePost{URL: p.Link, Date: p.DatePublished, Text: p.Title}
		var post struct {
			Description  string `json:"description"`
			LikeCount    int64  `json:"likeCount"`
			CommentCount int64  `json:"commentCount"`
		}
		if err := c.get(ctx, "/v1/linkedin/post", url.Values{"url": {p.Link}}, &post); err == nil {
			if len(post.Description) > len(sp.Text) {
				sp.Text = post.Description
			}
			sp.Likes, sp.Comments = post.LikeCount, post.CommentCount
		} else {
			logf("style: could not fetch engagement for %s: %v", p.Link, err)
		}
		if strings.TrimSpace(sp.Text) != "" {
			sc.Posts = append(sc.Posts, sp)
		}
	}
	if len(sc.Posts) == 0 {
		return nil, fmt.Errorf("no posts found on %s", profileURL)
	}
	sort.Slice(sc.Posts, func(i, j int) bool { return sc.Posts[i].Likes > sc.Posts[j].Likes })

	if err := os.MkdirAll(cacheDir, 0o755); err == nil {
		data, _ := json.MarshalIndent(sc, "", "  ")
		_ = os.WriteFile(cachePath, data, 0o644)
	}
	logf("style: fetched %d posts from %s", len(sc.Posts), sc.Name)
	return sc, nil
}
