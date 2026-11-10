// Command tiktokscraper lists (and optionally downloads) videos from a TikTok
// account using the ScrapeCreators API. The API key is read from .env
// (SCRAPECREATORS_API_KEY) or from the environment.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const apiURL = "https://api.scrapecreators.com/v3/tiktok/profile/videos"

// flexBool accepts true/false, 1/0 or "1"/"0".
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	*b = flexBool(s == "true" || s == "1")
	return nil
}

type urlList struct {
	URLList []string `json:"url_list"`
}

type apiVideo struct {
	AwemeID    string      `json:"aweme_id"`
	Desc       string      `json:"desc"`
	CreateTime json.Number `json:"create_time"`
	URL        string      `json:"url"`
	ShareURL   string      `json:"share_url"`
	IsTop      json.Number `json:"is_top"`
	Statistics struct {
		PlayCount    int64 `json:"play_count"`
		DiggCount    int64 `json:"digg_count"`
		CommentCount int64 `json:"comment_count"`
		ShareCount   int64 `json:"share_count"`
	} `json:"statistics"`
	Video struct {
		Duration              int64   `json:"duration"`
		PlayAddr              urlList `json:"play_addr"`
		DownloadAddr          urlList `json:"download_addr"`
		DownloadNoWatermarkAd urlList `json:"download_no_watermark_addr"`
		Cover                 urlList `json:"cover"`
	} `json:"video"`
	ImagePostInfo *json.RawMessage `json:"image_post_info"`
}

type apiResponse struct {
	Success          *bool       `json:"success"`
	Message          string      `json:"message"`
	CreditsRemaining json.Number `json:"credits_remaining"`
	AwemeList        []apiVideo  `json:"aweme_list"`
	HasMore          flexBool    `json:"has_more"`
	MaxCursor        json.Number `json:"max_cursor"`
}

// Video is the cleaned-up record we output.
type Video struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	DurationMs  int64     `json:"duration_ms"`
	Plays       int64     `json:"plays"`
	Likes       int64     `json:"likes"`
	Comments    int64     `json:"comments"`
	Shares      int64     `json:"shares"`
	Pinned      bool      `json:"pinned"`
	IsSlideshow bool      `json:"is_slideshow"`
	CoverURL    string    `json:"cover_url,omitempty"`
	VideoURLs   []string  `json:"video_urls,omitempty"` // best (no watermark) first
	File        string    `json:"file,omitempty"`
}

func main() {
	handle := flag.String("handle", "", "TikTok username, with or without @ (required)")
	limit := flag.Int("limit", 30, "max number of videos to fetch (0 = all)")
	maxPages := flag.Int("max-pages", 10, "max API pages to request (each costs 1 credit, 0 = unlimited)")
	sortBy := flag.String("sort", "latest", "sort order: latest or popular")
	region := flag.String("region", "", "optional 2-letter region code (e.g. US, FR)")
	download := flag.Bool("download", false, "download the .mp4 files")
	outDir := flag.String("out", "output", "output directory")
	envFile := flag.String("env", ".env", "path to .env file")
	flag.Parse()

	h := strings.TrimPrefix(strings.TrimSpace(*handle), "@")
	if h == "" && flag.NArg() > 0 {
		h = strings.TrimPrefix(flag.Arg(0), "@")
	}
	if h == "" {
		fmt.Fprintln(os.Stderr, "usage: tiktokscraper -handle <username> [-limit N] [-download]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	if err := loadEnv(*envFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("reading %s: %v", *envFile, err)
	}
	apiKey := os.Getenv("SCRAPECREATORS_API_KEY")
	if apiKey == "" {
		log.Fatal("SCRAPECREATORS_API_KEY not set (put it in .env)")
	}

	ctx := context.Background()
	client := &http.Client{Timeout: 60 * time.Second}

	videos, err := fetchVideos(ctx, client, apiKey, h, *sortBy, *region, *limit, *maxPages)
	if err != nil && len(videos) == 0 {
		log.Fatal(err)
	} else if err != nil {
		log.Printf("warning: stopped early: %v", err)
	}
	log.Printf("fetched %d videos from @%s", len(videos), h)

	dir := filepath.Join(*outDir, h)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}

	if *download {
		dlClient := &http.Client{Timeout: 5 * time.Minute}
		for i := range videos {
			v := &videos[i]
			if v.IsSlideshow || len(v.VideoURLs) == 0 {
				log.Printf("[%d/%d] %s: no video stream (slideshow?), skipped", i+1, len(videos), v.ID)
				continue
			}
			path := filepath.Join(dir, v.ID+".mp4")
			if st, err := os.Stat(path); err == nil && st.Size() > 0 {
				v.File = path
				log.Printf("[%d/%d] %s: already downloaded", i+1, len(videos), v.ID)
				continue
			}
			if err := downloadFirst(ctx, dlClient, v.VideoURLs, path); err != nil {
				log.Printf("[%d/%d] %s: download failed: %v", i+1, len(videos), v.ID, err)
				continue
			}
			v.File = path
			log.Printf("[%d/%d] %s: saved %s", i+1, len(videos), v.ID, path)
		}
	}

	jsonPath := filepath.Join(dir, "videos.json")
	data, _ := json.MarshalIndent(videos, "", "  ")
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("metadata written to %s", jsonPath)

	for _, v := range videos {
		fmt.Printf("%s  %s  plays=%d likes=%d  %s\n", v.CreatedAt.Format("2006-01-02"), v.ID, v.Plays, v.Likes, truncate(v.Description, 60))
	}
}

func fetchVideos(ctx context.Context, c *http.Client, apiKey, handle, sortBy, region string, limit, maxPages int) ([]Video, error) {
	var out []Video
	seen := map[string]bool{}
	cursor := ""
	for page := 1; maxPages == 0 || page <= maxPages; page++ {
		q := url.Values{}
		q.Set("handle", handle)
		q.Set("trim", "true")
		if sortBy != "" {
			q.Set("sort_by", sortBy)
		}
		if region != "" {
			q.Set("region", region)
		}
		if cursor != "" {
			q.Set("max_cursor", cursor)
		}

		resp, err := callAPI(ctx, c, apiKey, apiURL+"?"+q.Encode())
		if err != nil {
			return out, fmt.Errorf("page %d: %w", page, err)
		}
		log.Printf("page %d: %d items (credits remaining: %s)", page, len(resp.AwemeList), resp.CreditsRemaining)

		for _, a := range resp.AwemeList {
			if a.AwemeID == "" || seen[a.AwemeID] {
				continue
			}
			seen[a.AwemeID] = true
			out = append(out, toVideo(a, handle))
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}

		next := resp.MaxCursor.String()
		if !bool(resp.HasMore) || next == "" || next == "0" || next == cursor {
			break
		}
		cursor = next
	}
	return out, nil
}

func callAPI(ctx context.Context, c *http.Client, apiKey, u string) (*apiResponse, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("Accept", "application/json")
		res, err := c.Do(req)
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
			lastErr = fmt.Errorf("HTTP %d: %s", res.StatusCode, truncate(string(body), 200))
			continue
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, truncate(string(body), 300))
		}
		var r apiResponse
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		if r.Success != nil && !*r.Success {
			return nil, fmt.Errorf("API error: %s", r.Message)
		}
		return &r, nil
	}
	return nil, lastErr
}

func toVideo(a apiVideo, handle string) Video {
	v := Video{
		ID:          a.AwemeID,
		URL:         firstNonEmpty(a.URL, a.ShareURL, fmt.Sprintf("https://www.tiktok.com/@%s/video/%s", handle, a.AwemeID)),
		Description: a.Desc,
		DurationMs:  a.Video.Duration,
		Plays:       a.Statistics.PlayCount,
		Likes:       a.Statistics.DiggCount,
		Comments:    a.Statistics.CommentCount,
		Shares:      a.Statistics.ShareCount,
		Pinned:      a.IsTop.String() == "1",
		IsSlideshow: a.ImagePostInfo != nil && string(*a.ImagePostInfo) != "null",
	}
	if ts, err := a.CreateTime.Int64(); err == nil && ts > 0 {
		v.CreatedAt = time.Unix(ts, 0).UTC()
	}
	if len(a.Video.Cover.URLList) > 0 {
		v.CoverURL = a.Video.Cover.URLList[0]
	}
	// Prefer watermark-free sources, then fall back to the watermarked one.
	for _, l := range [][]string{a.Video.DownloadNoWatermarkAd.URLList, a.Video.PlayAddr.URLList, a.Video.DownloadAddr.URLList} {
		for _, u := range l {
			if u != "" && !contains(v.VideoURLs, u) {
				v.VideoURLs = append(v.VideoURLs, u)
			}
		}
	}
	return v
}

func downloadFirst(ctx context.Context, c *http.Client, urls []string, path string) error {
	var lastErr error
	for _, u := range urls {
		if err := downloadOne(ctx, c, u, path); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func downloadOne(ctx context.Context, c *http.Client, u, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.tiktok.com/")
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, res.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n == 0 {
		err = errors.New("empty response")
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// loadEnv reads KEY=VALUE lines into the environment without overriding
// variables that are already set.
func loadEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
