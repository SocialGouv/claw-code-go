package runtime

import (
	clawctx "github.com/SocialGouv/claw-code-go/internal/context"
	internalrt "github.com/SocialGouv/claw-code-go/internal/runtime"
)

// PromptConfig is re-exported from internal/runtime so external hosts
// (e.g. iterion) can select which system-prompt sections claw renders.
type PromptConfig = internalrt.PromptConfig

// DefaultPromptConfig returns the all-on default (Claude Code parity).
func DefaultPromptConfig() PromptConfig {
	return internalrt.DefaultPromptConfig()
}

// MinimalPromptConfig returns the all-off preset — the small-model mode
// where only the host's own prompt is sent, with no automatic sections.
func MinimalPromptConfig() PromptConfig {
	return internalrt.MinimalPromptConfig()
}

// PromptSectionNames returns the canonical section names accepted by
// ResolvePromptSections.
func PromptSectionNames() []string {
	return internalrt.PromptSectionNames()
}

// ResolvePromptSections builds a PromptConfig from CLI-style overrides:
// a non-empty `only` list enables exclusively the listed sections (implies
// minimal); `minimal` alone disables everything; `disable` turns sections
// off, applied last. Section names match case-insensitively with "-"/"_"
// ignored. Errors on the first unknown section name.
func ResolvePromptSections(minimal bool, only, disable []string) (PromptConfig, error) {
	cfg := &internalrt.Config{}
	if err := internalrt.ApplyPromptSectionOverrides(cfg, minimal, only, disable); err != nil {
		return PromptConfig{}, err
	}
	return *cfg.Prompt, nil
}

// OperatingPosture returns claw's authored operating-posture prompt section
// so hosts can compose it into their own system prompts. Hosts that already
// ship their own posture (e.g. iterion's authored base) should NOT add this
// — use BuildSystemContext alone to avoid a double posture.
func OperatingPosture() string {
	return clawctx.OperatingPosture
}

// BuildSystemContext renders the automatic context sections (environment,
// git status, CLAUDE.md project instructions incl. walk-up/imports, auto
// memory) for workDir according to cfg. It deliberately excludes the base
// identity sentence and the operating posture — the host owns its base
// prompt and opts into each piece.
func BuildSystemContext(workDir string, cfg PromptConfig) string {
	opts := clawctx.AssembleOptions{
		Environment:         cfg.Environment,
		GitStatus:           cfg.GitStatus,
		ProjectInstructions: cfg.ProjectInstructions,
		AutoMemory:          cfg.AutoMemory,
		Memory: clawctx.MemoryOptions{
			WalkUp:   cfg.MemoryWalkUp,
			Imports:  cfg.MemoryImports,
			MaxBytes: cfg.MemoryMaxBytes,
		},
	}
	return clawctx.NewAssemblerWithOptions(workDir, opts).Assemble()
}
