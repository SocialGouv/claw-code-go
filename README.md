# claw-code-go

<p align="center">
  <img src="assets/claw-code-go.png" alt="claw-code-go" width="360" />
</p>

<p align="center">
  <strong>An agentic coding runtime in Go — multi-provider, MCP-native, plugin-extensible.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=flat-square&logo=go" />
  <img src="https://img.shields.io/badge/Providers-Anthropic%20%7C%20OpenAI%20%7C%20Bedrock%20%7C%20Vertex%20%7C%20Foundry-orange?style=flat-square" />
  <img src="https://img.shields.io/badge/MCP-3%20transports%20%2B%20OAuth-green?style=flat-square" />
  <img src="https://img.shields.io/badge/Plugins-marketplace%20%2B%20cosign-blue?style=flat-square" />
  <img src="https://img.shields.io/badge/Sandbox-linux%20namespaces-purple?style=flat-square" />
  <img src="https://img.shields.io/badge/Telemetry-OTLP%20HTTP%20%2B%20gRPC-9cf?style=flat-square" />
  <img src="https://img.shields.io/badge/status-experimental-red?style=flat-square" />
</p>

> ⚠️ **Experimental.** The code compiles and passes `go vet`, but most ported features have not been manually validated. Use at your own risk. Contributions and testing are very welcome.

---

## ✨ What is this?

**claw-code-go** is a Go-native runtime for [Claude Code](https://docs.anthropic.com/en/docs/claude-code)–style agentic coding sessions: a streaming model loop, a permissions engine, a tool registry, MCP integration, plugins, sandboxing, and persistence — all callable from Go *and* runnable as a single CLI binary.

It is a fork of [daolmedo/claw-code-go](https://github.com/daolmedo/claw-code-go) (the original Go port) augmented with features ingested from [ultraworkers/claw-code](https://github.com/ultraworkers/claw-code) (a Rust port that emerged after the source leak): hooks, plugins, MCP lifecycle + OAuth, sandbox, classifier-driven permissions, multi-provider routing, prompt caching, OTLP telemetry, and more.

The Rust → Go porting was driven end-to-end by [**Iterion**](https://github.com/SocialGouv/iterion), a workflow engine for multi-agent LLM pipelines. See [How this was built](#%EF%B8%8F-how-this-was-built) below for the run stats.

---

## 🚀 Features

### 🌐 Multi-provider LLM client

Five providers are wired through a unified `<provider>/<model-id>` addressing scheme (e.g. `openai/gpt-5.4-mini`, `anthropic/claude-sonnet-4-6`, `bedrock/anthropic.claude-sonnet-4-6`):

- **Anthropic** & **OpenAI** — validated end-to-end (OpenAI routes through both `/v1/chat/completions` and `/v1/responses` for reasoning + tools).
- **AWS Bedrock**, **Google Vertex AI**, **Azure AI Foundry** — real implementations on top of the official SDKs, with live smoke tests under build tag `live`.
- Capability-aware routing, fallback chains, and per-provider `reasoning_effort` translation in `internal/apikit`.
- Typed `api.APIError` (`StatusCode`, `Retryable`) so callers drive retry classification via `errors.As`.

### 🛠️ Built-in tools

A rich tool surface re-exported from [`pkg/api/tools`](pkg/api/tools/) — pair of `XxxTool() api.Tool` (schema) + `ExecuteXxx(ctx, input)` (runtime):

- 📄 **File I/O** — `ReadFile`, `WriteFile`, `FileEdit`, `Glob`, `Grep`, `ReadImage`, `NotebookEdit`, PDF extraction.
- 💻 **Execution** — `Bash` (with workspace validation), `WebFetch` (size cap, header filtering).
- 🖱️ **Computer use** — full Anthropic action surface (`screenshot`, `*_click`, `type`, `key`, `mouse_move`, `cursor_position`, `left_click_drag`) backed by `xdotool` + ImageMagick on Linux/X11. Returns typed `ErrComputerUseUnavailable` when display/binaries are missing.
- 🗣️ **Interaction** — `AskUser` with pluggable `Asker` interface (`StdinAsker`, `ProgrammaticAsker`, `TUIAsker`) and structured options; `RemoteTrigger` HTTP wrapper with timeout, body cap, header allow/deny lists, CRLF guard.
- 🤖 **Multi-agent** — `Worker*`, `Team*`, `Task*`, `Cron*` tools (create / get / list / update / stop) for spawning isolated agents, scheduling, and parallel orchestration.
- 🔌 **MCP & LSP** — `ListMcpResources`, `ReadMcpResource`, `McpAuth`, plus first-class LSP queries.
- 🧠 **Workflow** — `TodoWrite`, `Sleep`, `StructuredOutput`, `REPL`, `Skill` (slash command dispatch), `ToolSearch` (discover tools by description), plan-mode toggles.

### 🔐 Permissions engine — 7 modes + pluggable classifier

| Mode | Behavior |
|------|----------|
| `ModeAllow` | Permits all operations without prompting. |
| `ModePrompt` | Consults the ruleset; asks the prompter when no rule matches. |
| `ModeReadOnly` | Allows read-only operations only; denies writes/exec. |
| `ModeWorkspaceWrite` | Allows writes within the workspace directory; denies outside. |
| `ModeDangerFullAccess` | Allows arbitrary command execution and system access. |
| `ModeDontAsk` | Strict allow-list — never prompts; denies anything not explicitly listed. |
| `ModeAuto` | Delegates to a `Classifier` (default safe-list permits read-only ops, prompts on writes). |

Pluggable classifiers in [`internal/permissions/`](internal/permissions/):

- **`RuleClassifier`** — rule-based safe-list.
- **`LLMClassifier`** — small-model (Haiku-backed) classifier with TTL+FIFO decision cache (1024 entries, 1h TTL by default) and fail-safe-to-Ask invariant on transport errors. Untrusted args are wrapped in `<tool_invocation>` tags so payloads cannot hijack the decision.

### 🪝 Lifecycle hooks — 11 events, in-process or shell

Programmatic [`Runner`](internal/hooks/runner.go) with sequential dispatch and "first non-Continue wins" semantics:

| Tool events | Session events | Plugin events |
|---|---|---|
| `PreToolUse` | `UserPromptSubmit` | `PrePluginInstall` |
| `PostToolUse` | `PreCompact` | `PostPluginInstall` |
| `PostToolUseFailure` | `PostCompact` | `PrePluginUninstall` |
| | `Stop` | `PostPluginUninstall` |

Subprocess shell-script hooks ([`hooks/runner.go`](hooks/runner.go)) cover the same lifecycle for ops who prefer scripts — Unix and Windows exec adapters, `exec.CommandContext` so cancelling the conversation kills any in-flight hook script.

### 🔌 MCP — three transports + OAuth

Full Model Context Protocol support in [`internal/mcp/`](internal/mcp/):

- 📡 **Transports** — stdio, SSE, WebSocket. RPC protocol layer is shared.
- 🔑 **OAuth Authorization Code + PKCE broker** with atomic disk-backed token storage. RFC 6749 / 7636 compliant: loopback callback for browser flows, automatic refresh near expiry, typed `ErrReauthRequired` for headless contexts (where opening a browser is impossible). Bearer header injection via `TransportConfig.AuthFunc`.
- Public façade: `pkg/api/mcp/oauth` re-exports `Broker`, `NewBroker`, `Token`, `Storage`, `ErrReauthRequired`.

### 📦 Plugin system + remote marketplace

Local plugin manager in [`plugin/`](plugin/) with install / uninstall / list / dispatch, plus a **remote marketplace** with end-to-end verification:

- 🗺️ **Two-tier layout** — `<base>/index.json` advertises plugins; `<base>/<name>/manifest.json` describes each.
- 🔏 **SHA-256 + cosign** signature verification, delegated to a single path so the slash command and the public API agree.
- 🛡️ HTTPS-only by default, opt-in `--insecure-marketplace` escape, 1 MiB metadata size cap.
- 🪝 Lifecycle hooks fire around every install/uninstall (`Pre*` can block, `Post*` always fires with error info on failure).
- 🧰 CLI: `claw-code-go plugin install --marketplace <url> [--require-signed] <name>`. Env vars: `CLAW_MARKETPLACE_URL`, `CLAW_PLUGIN_PUBLIC_KEY`, `CLAW_REQUIRE_SIGNED`.

### 🧱 Sandbox — Linux namespace isolation

[`internal/runtime/sandbox/`](internal/runtime/sandbox/) provides per-session isolation with graceful fallback on non-Linux hosts or inside containers:

- **Filesystem modes** — `Off`, `WorkspaceOnly`, `AllowList`.
- **Namespaces** — PID, UTS, IPC, network restrictions on Linux.
- **Auto-detection** — recognizes `/.dockerenv`, `/run/.containerenv`, and cgroup state to skip nesting.
- Status reporting via the runtime so callers can observe whether isolation is active.

### 💾 Session continuity

[`internal/runtime/`](internal/runtime/) persists every turn as JSONL events (legacy single-JSON sessions auto-loaded via `LoadSessionAuto`):

- 🔁 **Fork / inherit** sessions to preserve KV cache across related phases (this is what made the Iterion port converge fast).
- ⏯️ **Resume** any session by ID, `latest`, or path.
- 📜 **Timeline** subcommand renders chronological event view (`pretty` | `json` | `md`).
- 🧹 **Compaction** with `Pre/PostCompact` hooks, summary compression, and recent-N-turns preservation.

### 🧠 Memory & prompt sections

Claude Code-parity memory ([`internal/context/`](internal/context/)) with every automatic system-prompt section individually toggleable — heavy prompt engineering hurts small models, so everything is opt-out:

- 📖 **CLAUDE.md discovery** — `~/.claude/CLAUDE.md`, ancestor directories (recursive walk-up), `<cwd>/CLAUDE.md`, `<cwd>/.claude/CLAUDE.md`; `@path` imports expand recursively (5 hops, cycle-safe, code fences skipped); 48KB combined cap.
- 💾 **Auto memory** — per-workspace persistent `MEMORY.md` under `~/.claw-code/memory/<fingerprint>/` (override: `CLAW_MEMORY_DIR`), injected at session start with maintenance instructions; the model updates it with the standard file tools.
- 🧭 **Operating posture + behavioral sections** — authored sections covering the qualities Claude Code teaches natively: the posture core (read-before-edit, git safety, concision, verification honesty) plus `communication` (final-message discipline, `file:line` refs, the `<system-reminder>` contract), `task-management` (todo hygiene incl. post-compaction reseed), `doing-tasks` (anti-over-engineering, objectivity, no time estimates), `tool-policy` (parallel calls, dedicated tools over shell, tool_search before "unavailable"), `git-safety` (the full git protocol) and `context-management` (don't wrap up early + end-of-turn self-check). Original text, not a Claude Code copy; embedding hosts compose via `pkg/runtime.OperatingPosture()` / `BuildSystemContext()`.
- 🎛️ **Toggles everywhere** — tri-state `"prompt"` block in `settings.json` (`{"prompt": {"minimal": true, "projectInstructions": true}}` = CLAUDE.md only), CLI `--minimal-prompt` / `--prompt-sections` / `--disable-prompt-sections`, CLAUDE.md frontmatter `minimalPrompt` / `promptSections` / `disablePromptSections`. Sections: `environment`, `git-status`, `project-instructions`, `mcp-tools`, `compaction-summary`, `memory-walk-up`, `memory-imports`, `auto-memory`, `posture`, `communication`, `task-management`, `doing-tasks`, `tool-policy`, `git-safety`, `context-management`. Defaults all-on; `--system-prompt` replaces the authored base, `--append-system-prompt` suffixes the assembled prompt; inspect with `claw-code-go print-system-prompt --minimal-prompt`.

### ⚡ Prompt caching

Anthropic-native [`cache_control` breakpoint manager](internal/apikit/prompt_cache.go) with session-scoped fingerprints, cache-break detection (unexpected drops in `cache_read_input_tokens`), and persistent stats (hits / misses / writes / unexpected breaks). Cache-aware retry logic in `internal/apikit/retry.go`.

### 📡 Telemetry — OTLP/HTTP + OTLP/gRPC

Two exporters built on the official OpenTelemetry SDKs:

- 📨 `internal/apikit/telemetry/otlp` — OTLP/HTTP-JSON (`CLAWD_OTLP_HTTP_ENDPOINT`).
- 📨 `internal/apikit/telemetry/otlpgrpc` — OTLP/gRPC via `otlploggrpc` (`CLAWD_OTLP_GRPC_ENDPOINT`, `CLAWD_OTLP_GRPC_INSECURE`, `CLAWD_OTLP_GRPC_HEADERS`).
- 🏷️ Standard resource attributes: `service.name=claw-code-go` (override with `CLAWD_SERVICE_NAME`), `service.version` via `CLAWD_SERVICE_VERSION`.
- ⚠️ The legacy `ITERION_*` env vars are **deprecated** — use `CLAWD_*`.

### 🖥️ TUI

[`internal/tui/`](internal/tui/) ships a [Bubble Tea](https://github.com/charmbracelet/bubbletea)-backed interface: markdown rendering, syntax-highlighted code blocks, themes, REPL history, logo. The same renderer powers the `timeline` subcommand.

### 🧠 LSP integration

A first-class LSP tool ([`internal/lsp/`](internal/lsp/) + [`pkg/api/lsp`](pkg/api/lsp/)) lets the model query language servers for diagnostics, hover, and definitions during a coding session.

### 🤖 Multi-agent orchestration

Beyond single-agent loops, runtime primitives in [`internal/runtime/`](internal/runtime/) support multi-agent fleets:

- 👷 **Workers** — observable state machine (`Spawning` → `TrustRequired` → `ReadyForPrompt` → `Running` → `Finished`/`Failed`) with failure classification (TrustGate, PromptDelivery, Protocol, Provider).
- 👥 **Teams** — sub-agent registry and coordination primitives.
- 🗂️ **Tasks** — background task lifecycle (`create` / `get` / `list` / `update` / `output` / `stop`).
- ⏰ **Cron** — scheduled recurring tasks.
- 🚦 **Lanes / Policies / Recovery** — declarative rule engine for branch-locked workflows, freshness analysis, and 7 named failure-recovery scenarios.

### 🧰 CLI

```text
claw-code-go [--prompt | --repl | --session ID] ...
claw-code-go timeline --session <id> [--format pretty|json|md] [--limit n]
claw-code-go plugin install --marketplace <url> [--require-signed] <name>
claw-code-go dump-manifests [--json]
claw-code-go bootstrap-plan
claw-code-go print-system-prompt [--cwd ...] [--date ...] [--minimal-prompt] [--prompt-sections ...]
claw-code-go resume-session <file> [commands...]
```

Main-mode flags include `--model`, `--permission-mode`, `--allowed-tools`, `--reasoning-effort`, `--output-format`, `--session-dir`, `--work-dir`, `--dangerously-skip-permissions`, the prompt-section toggles `--minimal-prompt` / `--prompt-sections` / `--disable-prompt-sections`, and `--system-prompt` / `--append-system-prompt`.

---

## 🔧 Quick start

```go
import (
    "context"
    "github.com/SocialGouv/claw-code-go/pkg/api"
    "github.com/SocialGouv/claw-code-go/pkg/api/tools"
)

defs := []api.Tool{
    tools.ReadFileTool(),
    tools.WriteFileTool(),
    tools.GlobTool(),
    tools.GrepTool(),
    tools.FileEditTool(),
    tools.WebFetchTool(),
    tools.BashTool(),
    tools.ReadImageTool(),
    tools.ComputerUseTool(),
}

// Dispatch a tool call from the model:
out, err := tools.ExecuteReadFile(ctx, map[string]any{"path": "README.md"})
```

`ExecuteBash` additionally takes a `workspace string` for command validation (pass `""` to skip). The wrapper pins permissions to `ModeAllow`; gate invocations upstream (e.g. via an Iterion workflow's `allowed_tools` list).

---

## 📚 Reference

### Providers

| Provider | Status | Path |
|----------|--------|------|
| Anthropic | 🟢 validated end-to-end | `pkg/api/providers/anthropic` |
| OpenAI | 🟢 validated end-to-end (`/v1/chat/completions` + `/v1/responses`) | `pkg/api/providers/openai` |
| Bedrock | 🟡 available, untested in production (built on `aws-sdk-go-v2`) | `pkg/api/providers/bedrock` |
| Vertex AI | 🟡 available, untested in production (Google ADC + canonical `MapModelID`) | `pkg/api/providers/vertex` |
| Azure Foundry | 🟡 available, untested in production (OpenAI-wire compatible) | `pkg/api/providers/foundry` |

#### Running live provider tests

Each cloud provider ships a smoke test gated by the `live` Go build tag. The tests skip cleanly when the relevant credentials are not set, so the default `go test ./...` is unaffected.

```bash
# AWS Bedrock — uses the standard AWS SDK credentials chain.
AWS_REGION=us-east-1 \
AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... \
BEDROCK_MODEL=anthropic.claude-3-5-sonnet-20241022-v2:0 \
go test -tags live -run TestLiveStreamSmokeBedrock -v ./internal/api/providers/bedrock/...

# Google Vertex AI — uses Application Default Credentials.
GOOGLE_CLOUD_PROJECT=my-project \
GOOGLE_CLOUD_REGION=us-east5 \
GOOGLE_APPLICATION_CREDENTIALS=/path/to/sa.json \
VERTEX_MODEL=claude-sonnet-4-20250514 \
go test -tags live -run TestLiveStreamSmokeVertex -v ./internal/api/providers/vertex/...

# Azure AI Foundry — api-key auth, or DefaultAzureCredential when key is unset.
AZURE_OPENAI_ENDPOINT=https://my-resource.openai.azure.com \
AZURE_OPENAI_API_KEY=... \
FOUNDRY_MODEL=my-deployment \
go test -tags live -run TestLiveStreamSmokeFoundry -v ./internal/api/providers/foundry/...
```

Each test asserts only that the stream produced at least one text delta and a `message_stop` event — enough to confirm authentication, request shape, and SSE decoding work end-to-end.

### Lifecycle hooks example

```go
runner := hooks.NewRunner()
runner.Register(hooks.PreToolUse, func(ctx context.Context, hctx hooks.Context) (hooks.Decision, error) {
    if hctx.ToolName == "bash" {
        return hooks.Decision{Action: hooks.ActionBlock, Reason: "no shell"}, nil
    }
    return hooks.Decision{Action: hooks.ActionContinue}, nil
})
decision, _ := runner.Fire(ctx, hooks.Context{Event: hooks.PreToolUse, ToolName: "bash"})
```

The runner is wired into `internal/runtime/conversation.go`; nil runners are a documented no-op.

---

## 🏗️ How this was built

The Rust-to-Go feature porting was orchestrated by [**Iterion**](https://github.com/SocialGouv/iterion) — a workflow engine for complex multi-agent LLM pipelines, using Claude Code, Codex, and other backends. This repo is the real-world example that drove Iterion's development:

- 📜 [**rust_to_go_port.iter**](https://github.com/SocialGouv/iterion/blob/main/examples/rust_to_go_port.iter) — the Iterion workflow definition (DSL).
- 📝 [**rust_to_go_port.md**](https://github.com/SocialGouv/iterion/blob/main/examples/rust_to_go_port.md) — design decisions, lessons learned, empirical data.

### Run stats

| Metric | Value |
|--------|-------|
| Refinement runs | 4 (workflow iterated alongside the engine) |
| Outer loop iterations | 25+ batches across all runs |
| Commits added | 48 (47 by Iterion, 1 by Claude Code) |
| Go code generated | 37,995 lines across 173 files + 96 test files |
| New Go packages | 30+ (hooks, plugin, apikit, worker, lane, policy, recovery, sandbox, lsp, task, team, …) |
| Feature parity | 100% (37/37 features) |
| Final run | 100% parity in 41 min, single batch, zero fix loops, zero interventions |
| Longest autonomous stretch | 2h25m without human intervention |
| Dual-judge verdicts | 30+ across all runs (Claude + Codex) |

### How the workflow operates

The workflow breaks the porting into dependency-ordered batches. Each batch goes through: **plan → implement → simplify → commit → test → parity scan → dual-judge review → fix loop**. Session continuity (fork/inherit) preserves KV cache across related phases. A human gate pauses for high-risk batches and auto-approves routine ones. See the [full writeup](https://github.com/SocialGouv/iterion/blob/main/examples/rust_to_go_port.md) for details on convergence strategies, stagnation detection, and model allocation.

---

## Status

**Experimental.** The code compiles and passes `go vet`, but most of it has not been manually tested. The latest commit (Anthropic prompt caching with `cache_control` breakpoints) was done directly with Claude Code outside the Iterion workflow.

Contributions, testing, and feedback are welcome.

For the running list of changes, see [**CHANGELOG.md**](./CHANGELOG.md). For the parity matrix versus upstream Claude Code, see [**docs/parity.md**](./docs/parity.md).

## Credits

- [**daolmedo/claw-code-go**](https://github.com/daolmedo/claw-code-go) — the original Go port of Claude Code (upstream).
- [**ultraworkers/claw-code**](https://github.com/ultraworkers/claw-code) — the Rust port of Claude Code (feature source).
- [**Iterion**](https://github.com/SocialGouv/iterion) — the workflow orchestration engine that performed the porting.

## License

MIT
