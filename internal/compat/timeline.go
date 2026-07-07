package compat

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SocialGouv/claw-code-go/internal/api"
	"github.com/SocialGouv/claw-code-go/internal/runtime"
	"github.com/SocialGouv/claw-code-go/internal/tui"

	"github.com/charmbracelet/lipgloss"
)

// RunTimeline implements the `timeline` subcommand. It loads a saved session
// from disk and renders its events chronologically. Output format is controlled
// by --format (pretty|json|md). The pretty renderer uses the TUI's
// MarkdownRenderer for assistant text bodies; json emits the canonical JSONL
// snapshot; md produces a plain markdown timeline.
func RunTimeline(args []string) {
	if err := runTimeline(args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runTimeline(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("timeline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sessionFlag := fs.String("session", "", "Session ID to render (required)")
	storeFlag := fs.String("store", "", "Session store directory (default: ~/.claw-code/sessions)")
	formatFlag := fs.String("format", "pretty", "Output format: pretty | json | md")
	limitFlag := fs.Int("limit", 0, "Limit output to the last N timeline events (0 = no limit)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: claw-code-go timeline --session <id> [--store <dir>] [--format pretty|json|md] [--limit n]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*sessionFlag) == "" {
		fs.Usage()
		return fmt.Errorf("--session is required")
	}

	dir := *storeFlag
	if dir == "" {
		dir = defaultStoreDir()
	}
	if dir == "" {
		return fmt.Errorf("could not resolve a session store directory; pass --store explicitly")
	}

	sess, err := runtime.LoadSessionAuto(dir, *sessionFlag)
	if err != nil {
		return fmt.Errorf("load session %q from %s: %w", *sessionFlag, dir, err)
	}

	switch strings.ToLower(strings.TrimSpace(*formatFlag)) {
	case "pretty", "":
		return renderPretty(stdout, sess, *limitFlag)
	case "md", "markdown":
		return renderMarkdown(stdout, sess, *limitFlag)
	case "json", "jsonl":
		return renderJSONL(stdout, sess, *limitFlag)
	default:
		return fmt.Errorf("unknown --format %q (valid: pretty, json, md)", *formatFlag)
	}
}

func defaultStoreDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".claw-code", "sessions")
	}
	return ""
}

// timelineEvent is the unified view consumed by every renderer. It collapses
// session_meta, prompt_history, message, and compaction records onto a single
// timeline ordered by best-known timestamp.
type timelineEvent struct {
	Kind      string
	Timestamp time.Time
	Role      string
	Text      string
	Tool      string
	IsError   bool
	Image     bool
}

// buildEvents flattens a session into a chronological list. Messages don't
// carry per-block timestamps, so we anchor them to the latest known prompt
// timestamp and space them between that anchor and UpdatedAt for monotonic
// ordering. This matches the slash-command renderer in
// internal/commands/session_timeline.go.
func buildEvents(s *runtime.Session) []timelineEvent {
	events := []timelineEvent{
		{Kind: "session_started", Timestamp: s.CreatedAt, Text: s.ID},
	}

	for _, ph := range s.PromptHistory {
		events = append(events, timelineEvent{
			Kind:      "prompt",
			Timestamp: time.UnixMilli(ph.TimestampMs),
			Text:      ph.Text,
		})
	}

	if len(s.Messages) > 0 {
		var anchor time.Time
		if n := len(s.PromptHistory); n > 0 {
			anchor = time.UnixMilli(s.PromptHistory[n-1].TimestampMs)
		} else {
			anchor = s.CreatedAt
		}
		span := s.UpdatedAt.Sub(anchor)
		if span < 0 {
			span = 0
		}
		step := span / time.Duration(len(s.Messages)+1)
		for i, msg := range s.Messages {
			ts := anchor.Add(time.Duration(i+1) * step)
			for _, blk := range msg.Content {
				events = append(events, blockToEvent(msg.Role, ts, blk)...)
			}
		}
	}

	if s.CompactionCount > 0 {
		events = append(events, timelineEvent{
			Kind:      "compaction",
			Timestamp: s.UpdatedAt,
			Text:      fmt.Sprintf("%d compaction event(s)", s.CompactionCount),
		})
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events
}

func blockToEvent(role string, ts time.Time, blk api.ContentBlock) []timelineEvent {
	switch blk.Type {
	case "text":
		text := strings.TrimSpace(blk.Text)
		if text == "" {
			return nil
		}
		return []timelineEvent{{Kind: "message", Timestamp: ts, Role: role, Text: text}}
	case "tool_use":
		return []timelineEvent{{Kind: "tool_call", Timestamp: ts, Role: role, Tool: blk.Name}}
	case "tool_result":
		var inner string
		for _, c := range blk.Content {
			if c.Type == "text" {
				inner = c.Text
				break
			}
		}
		return []timelineEvent{{
			Kind:      "tool_result",
			Timestamp: ts,
			Role:      role,
			Text:      inner,
			IsError:   blk.IsError,
		}}
	case "image":
		return []timelineEvent{{Kind: "image", Timestamp: ts, Role: role, Image: true}}
	}
	return nil
}

func applyLimit(events []timelineEvent, limit int) []timelineEvent {
	if limit <= 0 || len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}

// renderPretty styles the timeline using lipgloss role badges plus the TUI
// markdown renderer for assistant text bodies. The output stays readable when
// piped to a non-TTY: lipgloss falls back to plain ANSI which most pagers
// handle, and we never rely on cursor control codes.
func renderPretty(w io.Writer, s *runtime.Session, limit int) error {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "6", Dark: "6"})
	dim := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "8", Dark: "8"})
	userBadge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "5", Dark: "5"})
	assistantBadge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "4", Dark: "4"})
	systemBadge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "3", Dark: "3"})
	toolStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "2", Dark: "2"})
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "1", Dark: "1"})

	fmt.Fprintln(w, header.Render(fmt.Sprintf("Session %s", s.ID)))
	fmt.Fprintln(w, dim.Render(fmt.Sprintf("Started %s, updated %s",
		s.CreatedAt.UTC().Format(time.RFC3339),
		s.UpdatedAt.UTC().Format(time.RFC3339))))
	if s.Fork != nil && s.Fork.ParentSessionID != "" {
		line := "Forked from " + s.Fork.ParentSessionID
		if s.Fork.BranchName != "" {
			line += " (branch " + s.Fork.BranchName + ")"
		}
		fmt.Fprintln(w, dim.Render(line))
	}
	fmt.Fprintln(w)

	events := applyLimit(buildEvents(s), limit)
	md := tui.NewMarkdownRenderer()

	for _, e := range events {
		ts := dim.Render(fmt.Sprintf("[%s]", e.Timestamp.UTC().Format("15:04:05")))
		switch e.Kind {
		case "session_started":
			fmt.Fprintf(w, "%s %s\n", ts, dim.Render("session created"))
		case "prompt":
			fmt.Fprintf(w, "%s %s\n", ts, userBadge.Render("user"))
			fmt.Fprintln(w, indent(md.RenderMarkdown(e.Text), "  "))
		case "message":
			badge := assistantBadge.Render("assistant")
			if e.Role == "user" {
				badge = userBadge.Render("user")
			} else if e.Role == "system" {
				badge = systemBadge.Render("system")
			}
			fmt.Fprintf(w, "%s %s\n", ts, badge)
			fmt.Fprintln(w, indent(md.RenderMarkdown(e.Text), "  "))
		case "tool_call":
			fmt.Fprintf(w, "%s %s %s\n", ts, toolStyle.Render("tool_call"), e.Tool)
		case "tool_result":
			label := toolStyle.Render("tool_result")
			if e.IsError {
				label = errStyle.Render("tool_error")
			}
			fmt.Fprintf(w, "%s %s\n", ts, label)
			if e.Text != "" {
				fmt.Fprintln(w, indent(truncateForPretty(e.Text, 400), "  "))
			}
		case "image":
			fmt.Fprintf(w, "%s %s\n", ts, dim.Render("[image attached]"))
		case "compaction":
			fmt.Fprintf(w, "%s %s\n", ts, dim.Render(e.Text))
		}
	}
	return nil
}

func renderMarkdown(w io.Writer, s *runtime.Session, limit int) error {
	fmt.Fprintf(w, "# Session %s\n\n", s.ID)
	fmt.Fprintf(w, "- Started: %s\n", s.CreatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "- Updated: %s\n", s.UpdatedAt.UTC().Format(time.RFC3339))
	if s.Fork != nil && s.Fork.ParentSessionID != "" {
		fmt.Fprintf(w, "- Forked from: %s", s.Fork.ParentSessionID)
		if s.Fork.BranchName != "" {
			fmt.Fprintf(w, " (branch %s)", s.Fork.BranchName)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w)

	events := applyLimit(buildEvents(s), limit)
	for _, e := range events {
		ts := e.Timestamp.UTC().Format(time.RFC3339)
		switch e.Kind {
		case "session_started":
			fmt.Fprintf(w, "## %s — session started\n\n", ts)
		case "prompt":
			fmt.Fprintf(w, "## %s — user (prompt)\n\n%s\n\n", ts, e.Text)
		case "message":
			role := e.Role
			if role == "" {
				role = "assistant"
			}
			fmt.Fprintf(w, "## %s — %s\n\n%s\n\n", ts, role, e.Text)
		case "tool_call":
			fmt.Fprintf(w, "## %s — tool_call: %s\n\n", ts, e.Tool)
		case "tool_result":
			label := "tool_result"
			if e.IsError {
				label = "tool_error"
			}
			fmt.Fprintf(w, "## %s — %s\n\n", ts, label)
			if e.Text != "" {
				fmt.Fprintf(w, "```\n%s\n```\n\n", e.Text)
			}
		case "image":
			fmt.Fprintf(w, "## %s — image attached\n\n", ts)
		case "compaction":
			fmt.Fprintf(w, "## %s — %s\n\n", ts, e.Text)
		}
	}
	return nil
}

// renderJSONL emits the canonical JSONL snapshot. The --limit flag, if set,
// keeps only the last N message records (session_meta and prompt_history are
// always preserved so the output stays a valid, parseable session).
func renderJSONL(w io.Writer, s *runtime.Session, limit int) error {
	if limit > 0 && len(s.Messages) > limit {
		trimmed := *s
		trimmed.Messages = append([]api.Message(nil), s.Messages[len(s.Messages)-limit:]...)
		s = &trimmed
	}
	snapshot, err := runtime.RenderJSONLSnapshot(s)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, snapshot); err != nil {
		return err
	}
	return nil
}

// indent prefixes every non-empty line with prefix. Empty lines are emitted
// as-is so the markdown renderer's blank-line spacing survives.
func indent(text, prefix string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, l := range lines {
		if l == "" {
			continue
		}
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func truncateForPretty(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
