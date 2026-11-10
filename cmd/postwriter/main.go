// Command postwriter repurposes a creator's TikTok videos into LinkedIn posts
// they publish as their own, written in the voice of a chosen LinkedIn author
// and in the language of their choice.
//
//	postwriter -handle iatomiq -n 3 -lang fr               # experiment B: agent picks its own captures
//	postwriter -video <url> -images shots/ -lang fr        # experiment A: creator-provided screenshots
//
// Pipeline per video:
//  1. ScrapeCreators: video info + transcript; LinkedIn style corpus (cached).
//  2. ffmpeg: the agent can look at any moment of the video (view_frame).
//  3. Claude agent (tool runner): picks visuals, drafts, and calls review_post
//     (Vale linter + independent editor scoring a virality framework) until it ships.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

var languageNames = map[string]string{
	"fr": "French", "en": "English", "es": "Spanish", "de": "German", "it": "Italian",
	"pt": "Portuguese", "nl": "Dutch", "ar": "Arabic", "ja": "Japanese", "zh": "Chinese",
}

func main() {
	handle := flag.String("handle", "", "creator's TikTok account: repurpose its most-played videos")
	n := flag.Int("n", 3, "with -handle: how many videos to repurpose")
	videoURL := flag.String("video", "", "a single TikTok video URL (instead of -handle)")
	images := flag.String("images", "", "experiment A: creator screenshots (files and/or folders, comma-separated); requires -video")
	visuals := flag.String("visuals", "auto", `"auto" (agent takes its own captures), "none"; forced to "provided" with -images`)
	lang := flag.String("lang", "fr", "post language code (fr, en, es, de, ...)")
	formal := flag.Bool("formal", false, `formal address ("vous" in French, "Sie" in German, ...)`)
	styleURL := flag.String("style", "https://www.linkedin.com/in/ruben-hassid/", "LinkedIn author whose voice to imitate")
	refresh := flag.Bool("refresh-style", false, "re-scrape the LinkedIn style corpus")
	model := flag.String("model", "claude-opus-5-5", "Claude model")
	effort := flag.String("effort", "high", "writer effort: low, medium, high, xhigh, max")
	valeCfg := flag.String("vale", "vale/.vale.ini", "Vale config")
	outDir := flag.String("out", "posts", "output folder")
	force := flag.Bool("force", false, "redo videos that already have a post")
	envFile := flag.String("env", ".env", ".env file")
	visualHint := flag.String("visual-hint", "", "direction for which images to attach, e.g. \"frames showing the prompt typed in ChatGPT\"")
	overviewN := flag.Int("overview", 6, "overview frames shown to the agent up front")
	tag := flag.String("tag", "", "suffix for the output folder, so a variant run doesn't overwrite earlier posts")
	calibrate := flag.Int("calibrate", 0, "score the N best reference posts with the editor (to check the bar is reachable), then exit")
	flag.Parse()

	logf := log.Printf
	if err := loadEnv(*envFile); err != nil && !os.IsNotExist(err) {
		log.Fatalf("reading %s: %v", *envFile, err)
	}
	scKey, antKey := os.Getenv("SCRAPECREATORS_API_KEY"), os.Getenv("ANTHROPIC_API_KEY")
	if scKey == "" || antKey == "" {
		log.Fatal("SCRAPECREATORS_API_KEY and ANTHROPIC_API_KEY must be set (in .env)")
	}
	valePath, err := findValeConfig(*valeCfg)
	if err != nil {
		log.Fatal(err)
	}
	*lang = strings.ToLower(*lang)
	langName := languageNames[*lang]
	if langName == "" {
		langName = *lang
	}
	register := "Address the reader informally, the way the reference author does (\"tu\" in French, \"du\" in German...)."
	if *formal {
		register = "Address the reader formally (\"vous\" in French, \"Sie\" in German, \"usted\" in Spanish...)."
	}

	var provided []providedImage
	if *images != "" {
		if *videoURL == "" {
			log.Fatal("-images goes with one -video (the screenshots belong to that video)")
		}
		if provided, err = loadImages(*images); err != nil {
			log.Fatal(err)
		}
		*visuals = "provided"
		logf("experiment A: %d creator screenshot(s)", len(provided))
	} else if *visuals != "auto" && *visuals != "none" {
		log.Fatal(`-visuals must be "auto" or "none"`)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	sc := &scClient{key: scKey, http: &http.Client{Timeout: 90 * time.Second}}

	if *calibrate > 0 {
		style, err := sc.loadStyleCorpus(ctx, *styleURL, "cache", *refresh, logf)
		if err != nil {
			log.Fatal(err)
		}
		client := anthropic.NewClient(option.WithAPIKey(antKey))
		cfg := agentConfig{Model: *model, Language: "the language of the post", Logf: logf}
		runCalibration(ctx, client, cfg, style, *calibrate)
		return
	}

	var urls []string
	switch {
	case *videoURL != "":
		urls = []string{*videoURL}
	case *handle != "":
		*handle = strings.TrimPrefix(*handle, "@")
		if urls, err = sc.topVideos(ctx, *handle, *n, 3); err != nil {
			log.Fatal(err)
		}
		logf("@%s: repurposing its %d most-played videos", *handle, len(urls))
	default:
		fmt.Fprintln(os.Stderr, "usage: postwriter -handle <tiktok account> [-n 3] [-lang fr]\n       postwriter -video <url> [-images screenshots/]")
		flag.PrintDefaults()
		os.Exit(2)
	}

	style, err := sc.loadStyleCorpus(ctx, *styleURL, "cache", *refresh, logf)
	if err != nil {
		log.Fatal(err)
	}
	client := anthropic.NewClient(option.WithAPIKey(antKey))
	cfg := agentConfig{
		Model: *model, Effort: anthropic.BetaOutputConfigEffort(*effort),
		Language: langName, Register: register, MaxIterations: 24, Logf: logf,
		VisualHint: *visualHint, OverviewN: *overviewN, Tag: *tag,
	}

	var done, failed int
	for i, u := range urls {
		logf("[%d/%d] %s", i+1, len(urls), u)
		if err := processVideo(ctx, sc, client, cfg, style, u, provided, *visuals, *lang, valePath, *outDir, *force); err != nil {
			logf("  FAILED: %v", err)
			failed++
			continue
		}
		done++
	}
	logf("finished: %d post(s) written, %d failed", done, failed)
	if failed > 0 && done == 0 {
		os.Exit(1)
	}
}

func processVideo(ctx context.Context, sc *scClient, client anthropic.Client, cfg agentConfig, style *StyleCorpus,
	videoURL string, provided []providedImage, visualMode, lang, valePath, outDir string, force bool) error {

	aweme, err := sc.tiktokVideo(ctx, videoURL)
	if err != nil {
		return err
	}
	if aweme.URL == "" {
		aweme.URL = videoURL
	}
	handle := aweme.Author.UniqueID
	name := aweme.AwemeID + "-" + lang + "-" + visualMode
	if cfg.Tag != "" {
		name += "-" + cfg.Tag
	}
	dir := filepath.Join(outDir, handle, name)
	if _, err := os.Stat(filepath.Join(dir, "post.txt")); err == nil && !force {
		logf("  already done (%s), skipping; use -force to redo", dir)
		return nil
	}

	video := &TikTokVideo{Aweme: aweme}
	if video.Transcript, err = sc.tiktokTranscript(ctx, videoURL); err != nil {
		logf("  transcript unavailable: %v", err)
	}
	job := &postJob{Video: video, Provided: provided, VisualMode: visualMode, Linter: newLinter(valePath, lang, handle)}
	if visualMode != "none" {
		if job.File, err = openVideo(ctx, aweme.videoURLs(), aweme.Video.Duration); err != nil {
			logf("  video download failed (%v): no captures for this one", err)
		}
		defer job.File.Close()
	}
	if video.Transcript == "" && job.File == nil && len(provided) == 0 && strings.TrimSpace(aweme.Desc) == "" {
		return fmt.Errorf("nothing to write about: no transcript, video, screenshots or caption")
	}
	if visualMode == "auto" && job.File == nil {
		job.VisualMode = "none"
	}

	res, err := runAgent(ctx, client, cfg, style, job)
	if err != nil {
		return err
	}

	// Independent final check.
	lint, err := job.Linter.lint(ctx, res.Post)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "post.txt"), []byte(res.Post+"\n"), 0o644); err != nil {
		return err
	}
	var visualFiles []string
	for i, v := range res.Visuals {
		ext := map[string]string{"image/png": ".png", "image/gif": ".gif", "image/webp": ".webp"}[v.MediaType]
		if ext == "" {
			ext = ".jpg"
		}
		name := fmt.Sprintf("visual-%d%s", i+1, ext)
		if err := os.WriteFile(filepath.Join(dir, name), v.Data, 0o644); err == nil {
			visualFiles = append(visualFiles, name)
		}
	}
	// Score the final version too, so experiments can be compared.
	var final *Review
	if n := len(res.Reviews); n > 0 && res.Reviews[n-1].Draft == res.Post {
		final = res.Reviews[n-1].Editor
	} else if final, err = critique(ctx, client, cfg, styleBlock(style), sourceMaterial(video), job, res.Visuals, res.Post); err != nil {
		logf("  final scoring failed: %v", err)
	}
	if final != nil {
		final.Verdict = map[bool]string{true: "ship", false: "revise"}[final.passes()]
		s := final.Scores
		logf("  final: hook %d value %d fidelity %d voice %d human %d close %d lang %d (avg %.1f) → %s",
			s.Hook, s.Value, s.Fidelity, s.Voice, s.Human, s.Close, s.Language, final.avg(), final.Verdict)
	}
	meta, _ := json.MarshalIndent(map[string]any{
		"source_video":  aweme.URL,
		"style_from":    style.Profile,
		"language":      lang,
		"visual_mode":   job.VisualMode,
		"visual_hint":   cfg.VisualHint,
		"model":         cfg.Model,
		"created_at":    time.Now().UTC(),
		"visuals":       res.Visuals,
		"visual_files":  visualFiles,
		"final_lint":    lint,
		"final_editor":  final,
		"review_rounds": res.Reviews,
	}, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "meta.json"), meta, 0o644)

	if lint.Errors > 0 {
		logf("  WARNING: final post still has %d lint errors", lint.Errors)
	}
	logf("  saved %s (%d chars, %d visual(s), %d review round(s))", dir, lint.Chars, len(visualFiles), len(res.Reviews))
	fmt.Printf("\n===== %s =====\n%s\n", dir, res.Post)
	return nil
}

// runCalibration grades the author's own best posts. If they can't reach
// "ship", the editor's bar is miscalibrated, not the writer.
func runCalibration(ctx context.Context, client anthropic.Client, cfg agentConfig, style *StyleCorpus, n int) {
	if n > len(style.Posts) {
		n = len(style.Posts)
	}
	for i := 0; i < n; i++ {
		p := style.Posts[i]
		src := &TikTokVideo{Aweme: tiktokAweme{Desc: p.Text}}
		src.Aweme.Author.Nickname = style.Name
		r, err := critique(ctx, client, cfg, styleBlock(style), sourceMaterial(src), &postJob{}, nil, p.Text)
		if err != nil {
			logf("post %d: %v", i+1, err)
			continue
		}
		r.Verdict = map[bool]string{true: "ship", false: "revise"}[r.passes()]
		s := r.Scores
		logf("reference post %d (%d likes): hook %d value %d fidelity %d voice %d human %d close %d lang %d (avg %.1f) → %s",
			i+1, p.Likes, s.Hook, s.Value, s.Fidelity, s.Voice, s.Human, s.Close, s.Language, r.avg(), r.Verdict)
		for _, f := range r.Fixes {
			logf("   fix: %.160s", f)
		}
	}
}

func logf(format string, args ...any) { log.Printf(format, args...) }

// loadEnv reads KEY=VALUE lines without overriding already-set variables.
func loadEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimPrefix(strings.TrimSpace(s.Text()), "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		if _, set := os.LookupEnv(k); !set {
			os.Setenv(k, v)
		}
	}
	return s.Err()
}
