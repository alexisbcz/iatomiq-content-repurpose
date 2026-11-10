package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ValeAlert mirrors one entry of `vale --output JSON`.
type ValeAlert struct {
	Check    string `json:"Check"`
	Message  string `json:"Message"`
	Severity string `json:"Severity"`
	Line     int    `json:"Line"`
	Match    string `json:"Match"`
}

type LintReport struct {
	Errors      int         `json:"errors"`
	Warnings    int         `json:"warnings"`
	Suggestions int         `json:"suggestions"`
	Chars       int         `json:"chars"`
	HookChars   int         `json:"hook_chars"` // chars before LinkedIn's "see more" fold (first 2 lines)
	Alerts      []ValeAlert `json:"alerts"`
}

type linter struct {
	config string
	lang   string   // "fr", "en", ... picks the Vale style through the file extension
	leaks  []string // regexps that must not appear (reveal the TikTok origin)
}

func newLinter(config, lang, handle string) *linter {
	l := &linter{config: config, lang: strings.ToLower(lang)}
	l.leaks = []string{`(?i)tik\s*tok`, `(?i)\bfyp\b`, `(?i)\b(abonne|follow)[- ]?(toi|moi|me)\b`}
	if handle != "" {
		l.leaks = append(l.leaks, `(?i)@?`+regexp.QuoteMeta(handle))
	}
	return l
}

func (l *linter) lint(ctx context.Context, text string) (*LintReport, error) {
	dir, err := os.MkdirTemp("", "lint-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "post."+l.lang+".md")
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, "vale", "--config", l.config, "--output", "JSON", "--no-exit", file)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("vale: %s", ee.Stderr)
		}
		return nil, fmt.Errorf("vale: %w (is it installed? brew install vale)", err)
	}
	var byFile map[string][]ValeAlert
	if err := json.Unmarshal(out, &byFile); err != nil {
		return nil, fmt.Errorf("vale output: %w: %.200s", err, out)
	}

	r := &LintReport{Chars: utf8.RuneCountInString(text), Alerts: []ValeAlert{}}
	add := func(a ValeAlert) {
		switch a.Severity {
		case "error":
			r.Errors++
		case "warning":
			r.Warnings++
		default:
			r.Suggestions++
		}
		r.Alerts = append(r.Alerts, a)
	}
	for _, alerts := range byFile {
		for _, a := range alerts {
			add(a)
		}
	}

	for _, pat := range l.leaks {
		if m := regexp.MustCompile(pat).FindString(text); m != "" {
			add(ValeAlert{Check: "Postwriter.SourceLeak", Severity: "error", Match: m,
				Message: fmt.Sprintf("'%s' reveals where the content comes from. The post is published by the creator as original LinkedIn content: no TikTok, no handle, no video stats.", m)})
		}
	}
	lines := strings.SplitN(strings.TrimSpace(text), "\n", 3)
	if len(lines) > 2 {
		lines = lines[:2]
	}
	r.HookChars = utf8.RuneCountInString(strings.Join(lines, "\n"))
	if r.Chars > 3000 {
		add(ValeAlert{Check: "Postwriter.Length", Severity: "error",
			Message: fmt.Sprintf("Post is %d characters; LinkedIn's limit is 3000.", r.Chars)})
	}
	if r.HookChars > 210 {
		add(ValeAlert{Check: "Postwriter.Hook", Severity: "warning",
			Message: fmt.Sprintf("First two lines are %d characters; past ~210 the hook falls under 'see more'.", r.HookChars)})
	}
	if strings.Contains(text, "**") || regexp.MustCompile(`(?m)^#{1,6} `).MatchString(text) {
		add(ValeAlert{Check: "Postwriter.Markdown", Severity: "error",
			Message: "Markdown (** or # headings) does not render on LinkedIn. Use plain text."})
	}
	if regexp.MustCompile(`https?://`).MatchString(text) {
		add(ValeAlert{Check: "Postwriter.Link", Severity: "warning",
			Message: "External link in the body: LinkedIn cuts reach for posts with links. Put it in the first comment instead."})
	}
	return r, nil
}

func findValeConfig(flagValue string) (string, error) {
	candidates := []string{flagValue}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "..", "vale", ".vale.ini"))
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if abs, err := filepath.Abs(c); err == nil {
			if _, err := os.Stat(abs); err == nil {
				return abs, nil
			}
		}
	}
	return "", fmt.Errorf("vale config not found (looked for %s)", flagValue)
}
