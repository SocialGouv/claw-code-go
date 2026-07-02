package context

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/SocialGouv/claw-code-go/internal/config"
)

// AssembleOptions selects which context sections Assemble emits. The zero
// value disables everything — construct via DefaultAssembleOptions (all on)
// and flip sections off, or use NewAssembler which defaults to all-on.
type AssembleOptions struct {
	Environment         bool
	GitStatus           bool
	ProjectInstructions bool
	// Memory configures CLAUDE.md discovery (walk-up, imports, size cap)
	// when ProjectInstructions is enabled.
	Memory MemoryOptions
}

// DefaultAssembleOptions returns the all-on default (Claude Code parity).
func DefaultAssembleOptions() AssembleOptions {
	return AssembleOptions{
		Environment:         true,
		GitStatus:           true,
		ProjectInstructions: true,
		Memory:              DefaultMemoryOptions(),
	}
}

// Assembler collects and caches project context for injection into the system prompt.
type Assembler struct {
	WorkDir string

	opts AssembleOptions

	mu            sync.Mutex
	memCache      string
	memMtimes     map[string]int64 // files actually read on the last load (incl. imports)
	memCandidates map[string]int64 // discovery candidates present on the last check
	frontmatter   *config.FrontmatterConfig
}

// NewAssembler creates an Assembler for the given working directory with all
// context sections enabled.
func NewAssembler(workDir string) *Assembler {
	return NewAssemblerWithOptions(workDir, DefaultAssembleOptions())
}

// NewAssemblerWithOptions creates an Assembler emitting only the sections
// enabled in opts.
func NewAssemblerWithOptions(workDir string, opts AssembleOptions) *Assembler {
	return &Assembler{WorkDir: workDir, opts: opts}
}

// Assemble returns a formatted context block combining environment info, git status,
// and CLAUDE.md memory files. Any individual failure is silently skipped.
func (a *Assembler) Assemble() string {
	var sections []string

	if a.opts.Environment {
		if info := SystemInfo(a.WorkDir); info != "" {
			sections = append(sections, "# Environment\n\n"+info)
		}
	}

	if a.opts.GitStatus {
		if git := GitStatus(a.WorkDir); git != "" {
			sections = append(sections, "# Git Status\n\n"+git)
		}
	}

	if a.opts.ProjectInstructions {
		if mem := a.loadMemory(); mem != "" {
			sections = append(sections, "# Project Instructions (CLAUDE.md)\n\n"+mem)
		}
	}

	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n")
}

// loadMemory returns cached CLAUDE.md content, re-reading only when files
// change. Revalidation is two-part: re-stat the discovery candidates (notices
// created/deleted memory files) and the files actually read on the last load
// (notices edited roots AND imports).
func (a *Assembler) loadMemory() string {
	a.mu.Lock()
	defer a.mu.Unlock()

	candidates := MemoryCandidateMtimes(a.WorkDir, a.opts.Memory)
	if mtimesEqual(candidates, a.memCandidates) && mtimesEqual(currentMtimes(a.memMtimes), a.memMtimes) {
		return a.memCache
	}
	a.memCache, a.memMtimes = LoadMemory(a.WorkDir, a.opts.Memory)
	a.memCandidates = candidates
	return a.memCache
}

// currentMtimes re-stats the paths in prev, returning their current mtimes
// (absent paths are omitted, so a deleted import invalidates the cache).
func currentMtimes(prev map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(prev))
	for p := range prev {
		if info, err := os.Stat(p); err == nil {
			result[p] = info.ModTime().UnixNano()
		}
	}
	return result
}

// LoadFrontmatter reads the primary CLAUDE.md in the work directory and parses
// any YAML frontmatter config overrides. Returns nil if no frontmatter found.
func (a *Assembler) LoadFrontmatter() *config.FrontmatterConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.frontmatter != nil {
		return a.frontmatter
	}
	data, err := os.ReadFile(filepath.Join(a.WorkDir, "CLAUDE.md"))
	if err != nil {
		return nil
	}
	fm, _, err := config.ParseFrontmatter(data)
	if err != nil {
		return nil
	}
	if fm.HasOverrides() {
		a.frontmatter = &fm
	}
	return a.frontmatter
}

// Frontmatter returns the parsed frontmatter config from CLAUDE.md, if any.
func (a *Assembler) Frontmatter() *config.FrontmatterConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.frontmatter
}

func mtimesEqual(a, b map[string]int64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
