package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SocialGouv/claw-code-go/internal/auth"
	"github.com/SocialGouv/claw-code-go/internal/runtime"
)

func TestCodexAuthFailureExitsBeforeFallback(t *testing.T) {
	if os.Getenv("CLAW_TEST_CODEX_AUTH_EXIT") == "1" {
		os.Args = []string{os.Args[0], "--codex-auth", "--prompt", "generate an image"}
		main()
		return
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "fixture-anthropic-key")
	t.Setenv("CLAW_TEST_CODEX_AUTH_EXIT", "1")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCodexAuthFailureExitsBeforeFallback$")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "Codex authentication unavailable") {
		t.Fatalf("explicit Codex auth did not fail closed: %v, output %q", err, out)
	}
}

func TestConfigureCodexAuthUsesExplicitLogin(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CODEX_HOME", root)
	data, _ := json.Marshal(map[string]any{
		"auth_mode": "chatgpt",
		"tokens":    map[string]string{"access_token": "test-access-token", "account_id": "test-account", "refresh_token": "test-refresh-token"},
	})
	if err := os.WriteFile(filepath.Join(root, "auth.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &runtime.Config{ProviderName: "anthropic", Model: runtime.DefaultModel, APIKey: "old-key", BaseURL: "https://old.example"}
	if err := configureCodexAuth(cfg, ""); err != nil {
		t.Fatal(err)
	}
	if cfg.ProviderName != "openai" || cfg.Model != "gpt-5.5" || cfg.AuthMethod != "codex_oauth" ||
		cfg.CodexAuthFile != filepath.Join(root, "auth.json") || cfg.APIKey != "" || cfg.OAuthToken != "" || cfg.BaseURL != "" {
		t.Fatalf("configured Codex client = %+v", cfg)
	}
	if _, err := runtime.NewProviderClient(cfg); err != nil {
		t.Fatalf("provider factory did not accept Codex login: %v", err)
	}
	cfg.Model = "anthropic/claude-sonnet"
	if err := configureCodexAuth(cfg, ""); err != nil || cfg.Model != "gpt-5.5" {
		t.Fatalf("inherited Anthropic model was not replaced: %q, %v", cfg.Model, err)
	}
	bad := &runtime.Config{ProviderName: "xai", Model: "grok-1"}
	if err := configureCodexAuth(bad, ""); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("provider conflict = %v", err)
	}
	bad = &runtime.Config{ProviderName: "anthropic", Model: runtime.DefaultModel}
	if err := configureCodexAuth(bad, "anthropic/claude-sonnet"); err == nil || !strings.Contains(err.Error(), "OpenAI model") {
		t.Fatalf("model conflict = %v", err)
	}
}

func TestExistingCredentialPrecedenceWithoutCodexSelection(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
	t.Setenv("OPENAI_API_KEY", "openai-key")
	provider, token, method, err := auth.ResolveCredentials()
	if err != nil || provider != "anthropic" || token != "anthropic-key" || method != "api_key" {
		t.Fatalf("existing precedence changed: %s, %s, %s, %v", provider, token, method, err)
	}
}
