package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/toolrunner"
)

const maxReviews = 4

type agentConfig struct {
	Model         string
	Effort        anthropic.BetaOutputConfigEffort
	Language      string // human-readable, e.g. "French"
	Register      string // extra instruction about tu/vous, formality
	MaxIterations int
	Logf          func(string, ...any)
	VisualHint    string // the creator's direction for which images to attach
	OverviewN     int    // overview frames shown up front
	Tag           string // suffix for the output folder, so variants don't overwrite each other
}

type postJob struct {
	Video      *TikTokVideo
	File       *videoFile      // nil if the video could not be downloaded
	Provided   []providedImage // experiment A
	VisualMode string          // "auto" (experiment B), "provided" (A), "none"
	Linter     *linter
	Overview   []timedFrame // frames both the writer and the editor see
}

type Visual struct {
	Source    string  `json:"source"` // "capture" or "screenshot"
	Seconds   float64 `json:"seconds,omitempty"`
	Index     int     `json:"screenshot_number,omitempty"`
	Why       string  `json:"why"`
	MediaType string  `json:"-"`
	Data      []byte  `json:"-"`
}

type Review struct {
	Scores struct {
		Hook     int `json:"hook"`
		Value    int `json:"value"`
		Fidelity int `json:"fidelity"`
		Voice    int `json:"voice"`
		Human    int `json:"human"`
		Close    int `json:"close"`
		Language int `json:"language"`
	} `json:"scores"`
	Verdict  string   `json:"verdict"`
	Fixes    []string `json:"fixes"`
	Invented []string `json:"invented"`
}

// passes is the shipping bar, calibrated with -calibrate on the reference
// author's own best posts (they average ~7.5 with this editor).
func (r *Review) passes() bool {
	s := r.Scores
	for _, v := range []int{s.Hook, s.Value, s.Fidelity, s.Voice, s.Human, s.Close, s.Language} {
		if v < 6 {
			return false
		}
	}
	return r.avg() >= 7.5 && s.Fidelity >= 8 && s.Human >= 7 && len(r.Invented) == 0
}

func (r *Review) valid() bool {
	s := r.Scores
	for _, v := range []int{s.Hook, s.Value, s.Fidelity, s.Voice, s.Human, s.Close, s.Language} {
		if v < 1 || v > 10 {
			return false
		}
	}
	return true
}

func (r *Review) avg() float64 {
	s := r.Scores
	return float64(s.Hook+s.Value+s.Fidelity+s.Voice+s.Human+s.Close+s.Language) / 7
}

type AgentResult struct {
	Post    string
	Visuals []Visual
	Reviews []ReviewRound
}

type ReviewRound struct {
	Draft  string      `json:"draft"`
	Lint   *LintReport `json:"lint"`
	Editor *Review     `json:"editor"`
	Ship   bool        `json:"ship"`
}

// ---- tool inputs ----

type ReviewInput struct {
	Post string `json:"post" jsonschema:"required,description=The complete post draft exactly as it would be published."`
}

type ViewFrameInput struct {
	Seconds float64 `json:"seconds" jsonschema:"required,description=Timestamp in seconds from the start of the video."`
}

type VisualChoice struct {
	Source           string  `json:"source" jsonschema:"required,enum=capture,enum=screenshot,description=capture = a frame from the video; screenshot = one the creator attached."`
	Seconds          float64 `json:"seconds,omitempty" jsonschema:"description=For a capture: timestamp in seconds."`
	ScreenshotNumber int     `json:"screenshot_number,omitempty" jsonschema:"description=For a screenshot: its number (1-based)."`
	Why              string  `json:"why" jsonschema:"required,description=One line: why this image makes people stop."`
}

type SetVisualsInput struct {
	Visuals []VisualChoice `json:"visuals" jsonschema:"required,description=1 to 4 images in display order."`
}

func textResult(s string) anthropic.BetaToolResultBlockParamContentUnion {
	return anthropic.BetaToolResultBlockParamContentUnion{OfText: &anthropic.BetaTextBlockParam{Text: s}}
}

func imageBlock(mediaType string, data []byte) anthropic.BetaContentBlockParamUnion {
	return anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
		Data:      base64.StdEncoding.EncodeToString(data),
		MediaType: anthropic.BetaBase64ImageSourceMediaType(mediaType),
	})
}

// runAgent drives one post: Claude explores the video, picks visuals, drafts,
// and iterates on review_post (Vale + editor) until the post ships.
func runAgent(ctx context.Context, client anthropic.Client, cfg agentConfig, style *StyleCorpus, job *postJob) (*AgentResult, error) {
	res := &AgentResult{}
	source := sourceMaterial(job.Video)
	styleText := styleBlock(style)

	// --- tools ---
	review, err := toolrunner.NewBetaToolFromJSONSchema("review_post",
		"Review a complete draft: runs the Vale prose linter (AI-writing tells, LinkedIn formatting, source leaks) and an independent editor who scores it against the framework. Returns the verdict and the fixes to make.",
		func(ctx context.Context, in ReviewInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
			if len(res.Reviews) >= maxReviews {
				return textResult(`{"verdict":"budget_exhausted","note":"No reviews left. Fix any remaining Vale errors yourself and return your best version now."}`), nil
			}
			lint, err := job.Linter.lint(ctx, in.Post)
			if err != nil {
				return anthropic.BetaToolResultBlockParamContentUnion{}, err
			}
			ed, err := critique(ctx, client, cfg, styleText, source, job, res.Visuals, in.Post)
			if err != nil {
				return anthropic.BetaToolResultBlockParamContentUnion{}, fmt.Errorf("editor failed: %w", err)
			}
			ed.Verdict = "revise"
			if ed.passes() {
				ed.Verdict = "ship"
			}
			ship := lint.Errors == 0 && ed.Verdict == "ship"
			res.Reviews = append(res.Reviews, ReviewRound{Draft: in.Post, Lint: lint, Editor: ed, Ship: ship})
			s := ed.Scores
			cfg.Logf("  review %d: vale %dE/%dW | hook %d value %d fidelity %d voice %d human %d close %d lang %d → %s",
				len(res.Reviews), lint.Errors, lint.Warnings, s.Hook, s.Value, s.Fidelity, s.Voice, s.Human, s.Close, s.Language, map[bool]string{true: "SHIP", false: "revise"}[ship])
			verdict := "revise"
			if ship {
				verdict = "ship"
			}
			out, _ := json.MarshalIndent(map[string]any{
				"verdict": verdict, "vale": lint, "editor": ed, "reviews_left": maxReviews - len(res.Reviews),
			}, "", "  ")
			return textResult(string(out)), nil
		})
	if err != nil {
		return nil, err
	}
	tools := []anthropic.BetaTool{review}

	if job.File != nil && job.VisualMode != "none" {
		viewFrame, err := toolrunner.NewBetaToolFromJSONSchema("view_frame",
			fmt.Sprintf("Look at the video at a given second, in full resolution. The video is %.1f seconds long.", job.File.duration),
			func(ctx context.Context, in ViewFrameInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
				b, err := job.File.frameAt(ctx, in.Seconds, 1024)
				if err != nil {
					return anthropic.BetaToolResultBlockParamContentUnion{}, err
				}
				cfg.Logf("  view_frame %.1fs", in.Seconds)
				img := imageBlock("image/jpeg", b)
				return anthropic.BetaToolResultBlockParamContentUnion{OfImage: img.OfImage}, nil
			})
		if err != nil {
			return nil, err
		}
		tools = append(tools, viewFrame)
	}
	if job.VisualMode != "none" {
		setVisuals, err := toolrunner.NewBetaToolFromJSONSchema("set_visuals",
			"Choose the images attached to the post, in display order (1 to 4). Calling it again replaces the previous choice.",
			func(ctx context.Context, in SetVisualsInput) (anthropic.BetaToolResultBlockParamContentUnion, error) {
				if len(in.Visuals) == 0 || len(in.Visuals) > 4 {
					return anthropic.BetaToolResultBlockParamContentUnion{}, fmt.Errorf("choose between 1 and 4 visuals")
				}
				var picked []Visual
				for _, c := range in.Visuals {
					v := Visual{Source: c.Source, Why: c.Why}
					switch c.Source {
					case "capture":
						if job.File == nil {
							return anthropic.BetaToolResultBlockParamContentUnion{}, fmt.Errorf("no video file available for captures")
						}
						b, err := job.File.frameAt(ctx, c.Seconds, 1920)
						if err != nil {
							return anthropic.BetaToolResultBlockParamContentUnion{}, err
						}
						v.Seconds, v.Data, v.MediaType = c.Seconds, b, "image/jpeg"
					case "screenshot":
						if c.ScreenshotNumber < 1 || c.ScreenshotNumber > len(job.Provided) {
							return anthropic.BetaToolResultBlockParamContentUnion{}, fmt.Errorf("screenshot_number must be 1..%d", len(job.Provided))
						}
						p := job.Provided[c.ScreenshotNumber-1]
						v.Index, v.Data, v.MediaType = c.ScreenshotNumber, p.Data, p.MediaType
					default:
						return anthropic.BetaToolResultBlockParamContentUnion{}, fmt.Errorf("source must be capture or screenshot")
					}
					picked = append(picked, v)
				}
				res.Visuals = picked
				cfg.Logf("  set_visuals: %d image(s)", len(picked))
				return textResult(fmt.Sprintf("Saved %d visual(s).", len(picked))), nil
			})
		if err != nil {
			return nil, err
		}
		tools = append(tools, setVisuals)
	}

	// --- first user message: style (cached) + material ---
	blocks := []anthropic.BetaContentBlockParamUnion{
		{OfText: &anthropic.BetaTextBlockParam{Text: styleText, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
		anthropic.NewBetaTextBlock(source),
	}
	if job.File != nil {
		n := cfg.OverviewN
		if n <= 0 {
			n = 6
		}
		job.Overview = job.File.overview(ctx, n)
		blocks = append(blocks, anthropic.NewBetaTextBlock("Overview frames (low resolution):"))
		for _, f := range job.Overview {
			blocks = append(blocks, anthropic.NewBetaTextBlock(fmt.Sprintf("Frame at %.1fs:", f.T)), imageBlock("image/jpeg", f.JPG))
		}
	}
	for i, p := range job.Provided {
		blocks = append(blocks, anthropic.NewBetaTextBlock(fmt.Sprintf("Screenshot %d attached by the creator:", i+1)), imageBlock(p.MediaType, p.Data))
	}
	blocks = append(blocks, anthropic.NewBetaTextBlock("Write the LinkedIn post now, following the framework and the way of working."))

	visualStep := map[string]string{"auto": visualsAuto, "provided": visualsProvided, "none": visualsNone}[job.VisualMode]
	if cfg.VisualHint != "" && job.VisualMode != "none" {
		visualStep += "\n   The creator gave this direction for the visuals, and it takes priority over your own taste: " + cfg.VisualHint +
			"\n   Scan the video with view_frame (between and around the overview frames) until you find those moments; they can be brief."
	}
	system := fmt.Sprintf(writerSystem, cfg.Language, cfg.Register, framework, visualStep)

	runner := client.Beta.Messages.NewToolRunner(tools, anthropic.BetaToolRunnerParams{
		BetaMessageNewParams: anthropic.BetaMessageNewParams{
			Model:        cfg.Model,
			MaxTokens:    16000,
			System:       []anthropic.BetaTextBlockParam{{Text: system, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
			OutputConfig: anthropic.BetaOutputConfigParam{Effort: cfg.Effort},
			// If a safety classifier declines, the API re-serves the request on a fallback model.
			Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
			Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
			Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(blocks...)},
		},
		MaxIterations: cfg.MaxIterations,
	})
	msg, err := runner.RunToCompletion(ctx)
	if err != nil {
		return nil, err
	}
	if msg.StopReason == anthropic.BetaStopReasonRefusal {
		return nil, fmt.Errorf("model refused the request")
	}
	var text strings.Builder
	for _, b := range msg.Content {
		if t, ok := b.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	if m := postTag.FindAllStringSubmatch(text.String(), -1); len(m) > 0 {
		res.Post = m[len(m)-1][1]
		// Never trade a draft that passed review for an unreviewed edit of it.
		if n := len(res.Reviews); n > 0 && res.Reviews[n-1].Ship && strings.TrimSpace(res.Reviews[n-1].Draft) != strings.TrimSpace(res.Post) {
			cfg.Logf("  final answer differs from the draft that shipped; keeping the shipped draft")
			res.Post = res.Reviews[n-1].Draft
		}
	} else if n := len(res.Reviews); n > 0 {
		cfg.Logf("  no <post> block in final answer, using the last reviewed draft")
		res.Post = res.Reviews[n-1].Draft
	} else {
		return nil, fmt.Errorf("agent returned no post; raw output:\n%s", text.String())
	}
	return res, nil
}

var postTag = regexp.MustCompile(`(?s)<post>\s*(.*?)\s*</post>`)

// critique asks a separate Claude call to grade the draft. It sees the same
// source and style, plus the chosen visuals, but none of the writer's reasoning.
func critique(ctx context.Context, client anthropic.Client, cfg agentConfig, styleText, source string, job *postJob, visuals []Visual, draft string) (*Review, error) {
	blocks := []anthropic.BetaContentBlockParamUnion{
		{OfText: &anthropic.BetaTextBlockParam{Text: styleText, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
		anthropic.NewBetaTextBlock(source),
	}
	for _, f := range job.Overview {
		blocks = append(blocks, anthropic.NewBetaTextBlock(fmt.Sprintf("Source video frame at %.1fs:", f.T)), imageBlock("image/jpeg", f.JPG))
	}
	for i, p := range job.Provided {
		blocks = append(blocks, anthropic.NewBetaTextBlock(fmt.Sprintf("Source screenshot %d:", i+1)), imageBlock(p.MediaType, p.Data))
	}
	for i, v := range visuals {
		if v.Source == "capture" {
			blocks = append(blocks, anthropic.NewBetaTextBlock(fmt.Sprintf("Image %d attached to the post (video capture at %.1fs, also valid source material):", i+1, v.Seconds)), imageBlock(v.MediaType, v.Data))
		}
	}
	blocks = append(blocks, anthropic.NewBetaTextBlock(fmt.Sprintf(
		"Everything above (caption, transcript, frames, screenshots, attached images) is valid source material: a claim shown in any of them is not invented.\nTarget language: %s. The post is published by the creator as original LinkedIn content; any mention of TikTok, a video or another platform is a fidelity and human failure.\n\n<draft>\n%s\n</draft>\n\nGrade the draft.", cfg.Language, draft)))

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scores": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"hook": map[string]any{"type": "integer"}, "value": map[string]any{"type": "integer"},
					"fidelity": map[string]any{"type": "integer"}, "voice": map[string]any{"type": "integer"},
					"human": map[string]any{"type": "integer"}, "close": map[string]any{"type": "integer"},
					"language": map[string]any{"type": "integer"},
				},
				"required":             []string{"hook", "value", "fidelity", "voice", "human", "close", "language"},
				"additionalProperties": false,
			},
			"verdict":  map[string]any{"type": "string", "enum": []string{"ship", "revise"}},
			"fixes":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"invented": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required":             []string{"scores", "verdict", "fixes", "invented"},
		"additionalProperties": false,
	}

	params := anthropic.BetaMessageNewParams{
		Model:     cfg.Model,
		MaxTokens: 8000,
		System:    []anthropic.BetaTextBlockParam{{Text: criticSystem, CacheControl: anthropic.NewBetaCacheControlEphemeralParam()}},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortMedium,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: schema},
		},
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(blocks...)},
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		msg, err := client.Beta.Messages.New(ctx, params)
		if err != nil {
			return nil, err
		}
		var raw strings.Builder
		for _, b := range msg.Content {
			if t, ok := b.AsAny().(anthropic.BetaTextBlock); ok {
				raw.WriteString(t.Text)
			}
		}
		var r Review
		err = json.Unmarshal([]byte(raw.String()), &r)
		if err == nil && !r.valid() {
			err = fmt.Errorf("scores out of range")
		}
		if err == nil {
			return &r, nil
		}
		lastErr = fmt.Errorf("editor output (stop=%s): %v: %.200q", msg.StopReason, err, raw.String())
		cfg.Logf("  editor attempt %d failed: %v", attempt, lastErr)
	}
	return nil, lastErr
}

func styleBlock(style *StyleCorpus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Reference LinkedIn posts by %s (style only), best performing first.\n\n", style.Name)
	for i, p := range style.Posts {
		fmt.Fprintf(&b, "<reference_post index=\"%d\" likes=\"%d\" comments=\"%d\">\n%s\n</reference_post>\n\n", i+1, p.Likes, p.Comments, p.Text)
	}
	return b.String()
}

func sourceMaterial(v *TikTokVideo) string {
	a := v.Aweme
	var b strings.Builder
	fmt.Fprintf(&b, "<source_material creator=%q duration_s=\"%d\">\n", a.Author.Nickname, a.Video.Duration/1000)
	fmt.Fprintf(&b, "<caption>\n%s\n</caption>\n", a.Desc)
	if v.Transcript != "" {
		fmt.Fprintf(&b, "<transcript note=\"auto-generated, may contain speech-to-text mistakes\">\n%s\n</transcript>\n", v.Transcript)
	} else {
		b.WriteString("<transcript>(none: the video may be music only, rely on caption and frames)</transcript>\n")
	}
	b.WriteString("</source_material>")
	return b.String()
}
