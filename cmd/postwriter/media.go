package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// videoFile is a downloaded TikTok video the agent can take captures from.
type videoFile struct {
	dir      string
	path     string
	duration float64 // seconds
}

func openVideo(ctx context.Context, urls []string, durationMs int64) (*videoFile, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not installed (brew install ffmpeg)")
	}
	dir, err := os.MkdirTemp("", "postwriter-*")
	if err != nil {
		return nil, err
	}
	v := &videoFile{dir: dir, path: filepath.Join(dir, "video.mp4"), duration: float64(durationMs) / 1000}
	if err := downloadAny(ctx, urls, v.path); err != nil {
		v.Close()
		return nil, err
	}
	if v.duration <= 0 {
		v.duration = 30
	}
	return v, nil
}

func (v *videoFile) Close() {
	if v != nil {
		os.RemoveAll(v.dir)
	}
}

// frameAt returns a JPEG capture at t seconds, at most maxH pixels high.
func (v *videoFile) frameAt(ctx context.Context, t float64, maxH int) ([]byte, error) {
	if t < 0 {
		t = 0
	}
	if t > v.duration-0.1 {
		t = v.duration - 0.1
	}
	out := filepath.Join(v.dir, fmt.Sprintf("cap-%.2f-%d.jpg", t, maxH))
	cmd := exec.CommandContext(ctx, "ffmpeg", "-loglevel", "error", "-y", "-ss", fmt.Sprintf("%.2f", t), "-i", v.path,
		"-frames:v", "1", "-vf", fmt.Sprintf("scale=-2:'min(%d,ih)'", maxH), "-q:v", "3", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %v: %s", err, b)
	}
	return os.ReadFile(out)
}

type timedFrame struct {
	T   float64
	JPG []byte
}

// overview returns n evenly spaced small frames so the agent knows where to look.
func (v *videoFile) overview(ctx context.Context, n int) []timedFrame {
	var out []timedFrame
	for i := 0; i < n; i++ {
		t := v.duration * (float64(i) + 0.5) / float64(n)
		if b, err := v.frameAt(ctx, t, 640); err == nil {
			out = append(out, timedFrame{T: t, JPG: b})
		}
	}
	return out
}

func downloadAny(ctx context.Context, urls []string, path string) error {
	lastErr := fmt.Errorf("no video URL")
	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36")
		req.Header.Set("Referer", "https://www.tiktok.com/")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", res.StatusCode)
			continue
		}
		f, err := os.Create(path)
		if err != nil {
			res.Body.Close()
			return err
		}
		_, err = io.Copy(f, res.Body)
		res.Body.Close()
		f.Close()
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

// ---------- creator-provided screenshots (experiment A) ----------

type providedImage struct {
	Path      string
	MediaType string
	Data      []byte
}

// loadImages accepts a comma-separated list of files and/or directories.
func loadImages(spec string) ([]providedImage, error) {
	var paths []string
	for _, p := range strings.Split(spec, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if st.IsDir() {
			entries, _ := os.ReadDir(p)
			for _, e := range entries {
				if !e.IsDir() {
					paths = append(paths, filepath.Join(p, e.Name()))
				}
			}
		} else {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	var out []providedImage
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		mt := http.DetectContentType(b)
		switch mt {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
		default:
			continue // skip non-images silently (e.g. .DS_Store)
		}
		if len(b) > 5<<20 {
			return nil, fmt.Errorf("%s is over 5 MB, the API limit per image; resize it first", p)
		}
		out = append(out, providedImage{Path: p, MediaType: mt, Data: b})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no images found in %q", spec)
	}
	return out, nil
}
