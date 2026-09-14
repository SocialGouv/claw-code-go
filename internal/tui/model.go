package tui

import (
	"context"
	"fmt"
	"github.com/SocialGouv/claw-code-go/internal/auth"
	"github.com/SocialGouv/claw-code-go/internal/config"
	"github.com/SocialGouv/claw-code-go/internal/runtime"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	appVersion   = "0.1.0"
	textareaRows = 3 // visible rows in the multi-line input area
)

// modelEntry describes a selectable AI model in the picker overlay.
type modelEntry struct {
	id   string
	desc string
}

var anthropicModels = []modelEntry{
	{"claude-opus-4-8", "Most capable — complex reasoning, long-horizon agentic coding"},
	{"claude-sonnet-4-6", "Balanced — great performance at speed"},
	{"claude-haiku-4-5-20251001", "Fast and lightweight — quick tasks"},
}

var openAIModels = []modelEntry{
	{"gpt-4o", "Most capable — multimodal tasks and analysis"},
	{"gpt-4o-mini", "Fast and affordable — everyday tasks"},
	{"o1-mini", "Reasoning model — math and logic"},
}

// loginProvider describes a selectable AI provider in the /login flow.
type loginProviderEntry struct {
	id   string
	name string
	desc string
}

var loginProviders = []loginProviderEntry{
	{"anthropic", "Anthropic", "Claude Sonnet, Opus, Haiku models"},
	{"openai", "OpenAI", "GPT-4o and GPT-4o-mini models"},
}

// loginMethodEntry describes an auth method choice shown for a given provider.
type loginMethodEntry struct {
	id   string
	name string
	desc string
}

var anthropicAuthMethods = []loginMethodEntry{
	{"oauth", "OAuth (browser)", "Log in with your Claude.ai account"},
	{"api_key", "API Key", "Enter your Anthropic API key manually"},
}

// appState tracks what the TUI is currently doing.
type appState int

const (
	stateInput         appState = iota // waiting for user input
	stateBusy                          // streaming response from API
	statePicker                        // model selection overlay
	stateHelp                          // help panel overlay
	statePermission                    // waiting for permission decision
	stateLoginProvider                 // /login: provider picker
	stateLoginMethod                   // /login: auth-method picker (Anthropic)
	stateLoginAPIKey                   // /login: API key text input
	stateLoginOAuth                    // /login: waiting for OAuth browser flow
	stateAskUser                       // agent has asked the user a question
)

// Bubble Tea messages for async streaming events.
type (
	streamDeltaMsg    struct{ text string }
	streamToolMsg     struct{ name, input string }
	streamToolDoneMsg struct{ name, result string }
	streamUsageMsg    struct{ inputTokens, outputTokens int }
	streamDoneMsg     struct{}
	streamErrMsg      struct{ err error }
	streamWarnMsg     struct{ text string }
	streamPermAskMsg  struct {
		name, input string
		reply       chan runtime.PermDecision
	}
	streamAskUserMsg struct {
		question string
		reply    chan string
	}
)

// loginCompleteMsg is sent when a /login flow finishes (success or failure).
type loginCompleteMsg struct {
	provider string // "anthropic" or "openai"
	token    string // API key or OAuth access token
	method   string // "api_key" or "oauth"
	err      error
}

// Model is the Bubble Tea application model.
type Model struct {
	state  appState
	width  int
	height int
	ready  bool

	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model

	// history for ↑/↓ input navigation
	history *inputHistory

	// model picker state
	pickerCursor int

	// content buffers
	viewBuf   string // finalized history (all complete turns)
	streamBuf string // in-progress streaming content

	// token counts for status bar
	inputTokens  int
	outputTokens int

	// whether any streaming content has arrived (suppresses spinner)
	hasStreamContent bool

	// channel from active streaming goroutine
	streamChan chan runtime.TurnEvent

	// permission ask state
	permToolName  string
	permToolInput string
	permReplyCh   chan runtime.PermDecision

	// ask_user state
	askUserQuestion string
	askUserReplyCh  chan string
	askUserInput    textinput.Model

	// /login flow state
	loginCursor   int             // cursor for provider / method pickers
	loginProvider string          // provider selected during login
	loginKeyInput textinput.Model // API key entry input (single-line, masked)

	// app deps
	loop *runtime.ConversationLoop
	cfg  *runtime.Config
}

// NewModel creates a new TUI model.
func NewModel(cfg *runtime.Config, loop *runtime.ConversationLoop) Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message or /help..."
	ta.CharLimit = 8192
	ta.ShowLineNumbers = false
	ta.SetHeight(textareaRows)
	// Focus the textarea so the cursor is visible from the first render.
	// The blink Cmd is returned from Init().
	ta.Focus() //nolint:errcheck

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(currentTheme.Primary)

	return Model{
		state:    stateInput,
		textarea: ta,
		spinner:  s,
		history:  newInputHistory(),
		loop:     loop,
		cfg:      cfg,
		viewBuf:  RenderLogo(appVersion),
	}
}

// Init is the Bubble Tea Init function.
func (m Model) Init() tea.Cmd {
	// Start cursor blink for the textarea.
	return m.textarea.Focus()
}

// --- Update -----------------------------------------------------------------

// Update is the Bubble Tea Update function.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.ready {
			m.ready = true
			m = m.initViewport()
		} else {
			m = m.resizeViewport()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case streamDeltaMsg:
		if !m.hasStreamContent {
			m.hasStreamContent = true
		}
		m.streamBuf += msg.text
		m = m.refreshViewport()
		return m, waitForStream(m.streamChan)

	case streamToolMsg:
		if !m.hasStreamContent {
			m.hasStreamContent = true
		}
		line := toolRunningStyle.Render(fmt.Sprintf("  ◆ %s: %s\n", msg.name, truncate(msg.input, 60)))
		m.streamBuf += line
		m = m.refreshViewport()
		return m, waitForStream(m.streamChan)

	case streamToolDoneMsg:
		suffix := ""
		if msg.result != "" {
			suffix = " → " + truncate(msg.result, 40)
		}
		line := toolDoneStyle.Render(fmt.Sprintf("  ✓ %s%s\n", msg.name, suffix))
		m.streamBuf += line
		m = m.refreshViewport()
		return m, waitForStream(m.streamChan)

	case streamUsageMsg:
		m.inputTokens = msg.inputTokens
		m.outputTokens = msg.outputTokens
		return m, waitForStream(m.streamChan)

	case streamDoneMsg:
		// Commit streamBuf to viewBuf with token annotation.
		if m.streamBuf != "" || m.hasStreamContent {
			tokLine := statusStyle.Render(fmt.Sprintf(
				"\n\nTokens: %s in / %s out\n\n",
				formatNum(m.inputTokens),
				formatNum(m.outputTokens),
			))
			m.viewBuf += m.streamBuf + tokLine
			m.streamBuf = ""
		}
		m.hasStreamContent = false
		m.state = stateInput
		m = m.refreshViewport()
		m.viewport.GotoBottom()
		return m, nil

	case streamWarnMsg:
		m.viewBuf += warnStyle.Render(fmt.Sprintf("Warning: %s\n\n", msg.text))
		m = m.refreshViewport()
		return m, waitForStream(m.streamChan)

	case streamPermAskMsg:
		m.state = statePermission
		m.permToolName = msg.name
		m.permToolInput = msg.input
		m.permReplyCh = msg.reply
		m = m.refreshViewport()
		return m, nil

	case streamAskUserMsg:
		ti := textinput.New()
		ti.Placeholder = "Type your answer and press Enter..."
		ti.CharLimit = 2048
		ti.Focus()
		m.askUserInput = ti
		m.askUserQuestion = msg.question
		m.askUserReplyCh = msg.reply
		m.state = stateAskUser
		m = m.refreshViewport()
		return m, nil

	case streamErrMsg:
		m.viewBuf += errorStyle.Render(fmt.Sprintf("Error: %v\n\n", msg.err))
		m.streamBuf = ""
		m.hasStreamContent = false
		m.state = stateInput
		m = m.refreshViewport()
		return m, nil

	case loginCompleteMsg:
		return m.handleLoginComplete(msg)

	case spinner.TickMsg:
		if (m.state == stateBusy && !m.hasStreamContent) || m.state == stateLoginOAuth {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	return m, nil
}

// handleKey dispatches key events based on current state.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case statePicker:
		return m.handlePickerKey(msg)
	case stateHelp:
		return m.handleHelpKey(msg)
	case statePermission:
		return m.handlePermissionKey(msg)
	case stateAskUser:
		return m.handleAskUserKey(msg)
	case stateLoginProvider:
		return m.handleLoginProviderKey(msg)
	case stateLoginMethod:
		return m.handleLoginMethodKey(msg)
	case stateLoginAPIKey:
		return m.handleLoginAPIKeyKey(msg)
	case stateLoginOAuth:
		return m.handleLoginOAuthKey(msg)
	case stateBusy:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		return m, nil
	}

	// stateInput
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyEnter:
		// Submit the message.
		return m.handleSubmit()

	case tea.KeyCtrlJ:
		// Ctrl+J inserts a real newline into the multi-line input.
		m.history.Reset()
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(tea.KeyMsg{Type: tea.KeyEnter})
		return m, cmd

	case tea.KeyUp:
		// Navigate to previous history entry when input is single-line.
		if !strings.Contains(m.textarea.Value(), "\n") {
			prev := m.history.Prev(m.textarea.Value())
			m.textarea.SetValue(prev)
			return m, nil
		}
		// Multi-line: let textarea handle cursor movement.
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyDown:
		// Navigate to next history entry when input is single-line.
		if !strings.Contains(m.textarea.Value(), "\n") {
			next := m.history.Next()
			m.textarea.SetValue(next)
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd

	case tea.KeyPgUp, tea.KeyPgDown:
		// Scroll the conversation viewport.
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	default:
		// All other keys go to the textarea. Reset history navigation on
		// any edit so the draft is not accidentally discarded.
		m.history.Reset()
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		return m, cmd
	}
}

// handleSubmit processes the current textarea value.
func (m Model) handleSubmit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.textarea.Value())
	if text == "" {
		return m, nil
	}
	m.textarea.Reset()
	m.history.Push(text)
	m.history.Reset()

	if strings.HasPrefix(text, "/") {
		return m.handleSlashCommand(text)
	}
	return m.startMessage(text)
}

// handleSlashCommand processes built-in slash commands.
func (m Model) handleSlashCommand(cmd string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return m, nil
	}

	switch parts[0] {
	case "/model":
		m.state = statePicker
		m.pickerCursor = 0
		for i, km := range m.activeModels() {
			if km.id == m.cfg.Model {
				m.pickerCursor = i
				break
			}
		}
		return m, nil

	case "/help":
		m.state = stateHelp
		return m, nil

	case "/login":
		m.state = stateLoginProvider
		m.loginCursor = 0
		return m, nil

	case "/clear":
		m.loop.ClearSession()
		m.viewBuf = statusStyle.Render("Session cleared.\n\n")
		m.streamBuf = ""
		m.inputTokens = 0
		m.outputTokens = 0
		m = m.refreshViewport()
		return m, nil

	case "/session-list":
		metas, err := m.loop.ListSessionsWithMeta()
		if err != nil {
			m.viewBuf += errorStyle.Render(fmt.Sprintf("Error listing sessions: %v\n\n", err))
		} else if len(metas) == 0 {
			m.viewBuf += statusStyle.Render("No saved sessions.\n\n")
		} else {
			m.viewBuf += statusStyle.Render(formatSessionList(metas) + "\n\n")
		}
		m = m.refreshViewport()
		return m, nil

	case "/theme":
		theme := "dark"
		if len(parts) > 1 {
			theme = parts[1]
		}
		switch theme {
		case "light":
			SetTheme(LightTheme)
			m.viewBuf += statusStyle.Render("Theme: light.\n\n")
		default:
			SetTheme(DarkTheme)
			m.viewBuf += statusStyle.Render("Theme: dark.\n\n")
		}
		m = m.refreshViewport()
		return m, nil

	case "/auth":
		sub := "status"
		if len(parts) > 1 {
			sub = parts[1]
		}
		msg := m.handleAuthSubcommand(sub)
		m.viewBuf += statusStyle.Render(msg + "\n\n")
		m = m.refreshViewport()
		return m, nil

	case "/session":
		return m.handleSessionCommand(parts)

	case "/status":
		return m.handleStatus()

	case "/init":
		return m.handleInit()

	case "/cost":
		return m.handleCost()

	case "/config":
		return m.handleConfig(parts)

	case "/exit", "/quit":
		return m, tea.Quit

	default:
		m.viewBuf += errorStyle.Render(fmt.Sprintf("Unknown command: %s  (type /help for commands)\n\n", parts[0]))
		m = m.refreshViewport()
		return m, nil
	}
}

// handleSessionCommand handles /session list|save|load <name>.
func (m Model) handleSessionCommand(parts []string) (tea.Model, tea.Cmd) {
	sub := "list"
	if len(parts) > 1 {
		sub = parts[1]
	}
	switch sub {
	case "list":
		metas, err := m.loop.ListSessionsWithMeta()
		if err != nil {
			m.viewBuf += errorStyle.Render(fmt.Sprintf("Error listing sessions: %v\n\n", err))
		} else if len(metas) == 0 {
			m.viewBuf += statusStyle.Render("No saved sessions.\n\n")
		} else {
			m.viewBuf += statusStyle.Render(formatSessionList(metas) + "\n\n")
		}
	case "save":
		name := ""
		if len(parts) > 2 {
			name = parts[2]
		}
		if name != "" {
			m.loop.Session.ID = name
		}
		if err := m.loop.SaveCurrentSession(); err != nil {
			m.viewBuf += errorStyle.Render(fmt.Sprintf("Error saving session: %v\n\n", err))
		} else {
			m.viewBuf += statusStyle.Render(fmt.Sprintf("Session saved: %s\n\n", m.loop.Session.ID))
		}
	case "load":
		if len(parts) < 3 {
			m.viewBuf += errorStyle.Render("Usage: /session load <name>\n\n")
		} else {
			id := parts[2]
			if err := m.loop.LoadNamedSession(id); err != nil {
				m.viewBuf += errorStyle.Render(fmt.Sprintf("Error loading session %q: %v\n\n", id, err))
			} else {
				m.viewBuf += statusStyle.Render(fmt.Sprintf("Session loaded: %s (%d messages)\n\n", id, m.loop.MessageCount()))
			}
		}
	default:
		m.viewBuf += errorStyle.Render(fmt.Sprintf("Unknown /session subcommand %q. Usage: /session list|save|load <name>\n\n", sub))
	}
	m = m.refreshViewport()
	return m, nil
}

// handleStatus shows current model, provider, permission mode, and session info.
func (m Model) handleStatus() (tea.Model, tea.Cmd) {
	permMode := "default"
	if m.cfg.PermissionMode != "" {
		permMode = m.cfg.PermissionMode
	}
	if m.loop.PermManager != nil {
		permMode = m.loop.PermManager.Mode.String()
	}
	lines := []string{
		fmt.Sprintf("Provider       : %s", m.cfg.ProviderName),
		fmt.Sprintf("Model          : %s", m.cfg.Model),
		fmt.Sprintf("Permission mode: %s", permMode),
		fmt.Sprintf("Session ID     : %s", m.loop.Session.ID),
		fmt.Sprintf("Messages       : %d", m.loop.MessageCount()),
		fmt.Sprintf("Tokens in/out  : %s / %s", formatNum(m.inputTokens), formatNum(m.outputTokens)),
	}
	m.viewBuf += statusStyle.Render(strings.Join(lines, "\n") + "\n\n")
	m = m.refreshViewport()
	return m, nil
}

// handleInit creates .claude/settings.json with defaults.
func (m Model) handleInit() (tea.Model, tea.Cmd) {
	err := config.InitProject(m.cfg.Model)
	switch {
	case err == nil:
		m.viewBuf += statusStyle.Render("Created .claude/settings.json with defaults.\n\n")
	case os.IsExist(err):
		m.viewBuf += statusStyle.Render(".claude/settings.json already exists — no changes made.\n\n")
	default:
		m.viewBuf += errorStyle.Render(fmt.Sprintf("init: %v\n\n", err))
	}
	m = m.refreshViewport()
	return m, nil
}

// handleCost shows token usage and best-effort cost estimate for the session.
func (m Model) handleCost() (tea.Model, tea.Cmd) {
	var report string
	if m.loop.Usage != nil && m.loop.Usage.Turns > 0 {
		report = m.loop.Usage.FormatSummary()
		if m.loop.Compaction.CompactionCount > 0 {
			report += fmt.Sprintf("Compactions    : %d\n", m.loop.Compaction.CompactionCount)
		}
	} else {
		// No turns recorded yet; fall back to compaction-state totals.
		c := m.loop.Compaction
		lines := []string{
			fmt.Sprintf("Input tokens   : %s", formatNum(c.TotalInputTokens)),
			fmt.Sprintf("Output tokens  : %s", formatNum(c.TotalOutputTokens)),
			fmt.Sprintf("Compactions    : %d", c.CompactionCount),
			"Cost           : unavailable (no completed turns yet)",
		}
		report = strings.Join(lines, "\n")
	}
	m.viewBuf += statusStyle.Render(report + "\n\n")
	m = m.refreshViewport()
	return m, nil
}

// handleConfig handles /config [key [value]].
func (m Model) handleConfig(parts []string) (tea.Model, tea.Cmd) {
	if len(parts) == 1 {
		// Show all config.
		permMode := m.cfg.PermissionMode
		if permMode == "" {
			permMode = "default"
		}
		lines := []string{
			fmt.Sprintf("model          = %s", m.cfg.Model),
			fmt.Sprintf("permissionMode = %s", permMode),
			fmt.Sprintf("maxTokens      = %d", m.cfg.MaxTokens),
			fmt.Sprintf("theme          = %s", m.cfg.Theme),
		}
		m.viewBuf += statusStyle.Render(strings.Join(lines, "\n") + "\n\n")
		m = m.refreshViewport()
		return m, nil
	}

	key := parts[1]
	if len(parts) == 2 {
		// Show single key.
		val := m.configGet(key)
		if val == "" {
			m.viewBuf += errorStyle.Render(fmt.Sprintf("Unknown config key: %s\n\n", key))
		} else {
			m.viewBuf += statusStyle.Render(fmt.Sprintf("%s = %s\n\n", key, val))
		}
		m = m.refreshViewport()
		return m, nil
	}

	// Set value.
	value := strings.Join(parts[2:], " ")
	if err := m.configSet(key, value); err != nil {
		m.viewBuf += errorStyle.Render(fmt.Sprintf("config set: %v\n\n", err))
	} else {
		m.viewBuf += statusStyle.Render(fmt.Sprintf("Set %s = %s\n\n", key, value))
	}
	m = m.refreshViewport()
	return m, nil
}

// configGet returns the string value of a config key from the active Config.
func (m Model) configGet(key string) string {
	switch key {
	case "model":
		return m.cfg.Model
	case "permissionMode":
		if m.cfg.PermissionMode == "" {
			return "default"
		}
		return m.cfg.PermissionMode
	case "maxTokens":
		return fmt.Sprintf("%d", m.cfg.MaxTokens)
	case "theme":
		return m.cfg.Theme
	default:
		return ""
	}
}

// configSet updates a config key in-memory and persists to project settings.json.
func (m *Model) configSet(key, value string) error {
	s := &config.Settings{}
	switch key {
	case "model":
		m.cfg.Model = value
		m.loop.Config.Model = value
		s.Model = value
	case "permissionMode":
		m.cfg.PermissionMode = value
		s.PermissionMode = value
	case "theme":
		m.cfg.Theme = value
		s.Theme = value
		switch value {
		case "light":
			SetTheme(LightTheme)
		default:
			SetTheme(DarkTheme)
		}
	default:
		return fmt.Errorf("unknown config key %q (valid: model, permissionMode, theme)", key)
	}
	return config.WriteProject(s)
}

// handleAuthSubcommand executes a legacy /auth subcommand and returns output text.
func (m Model) handleAuthSubcommand(sub string) string {
	switch sub {
	case "login":
		td, err := auth.StartOAuthFlow()
		if err != nil {
			return fmt.Sprintf("Auth login error: %v", err)
		}
		if err := auth.SaveTokens(td); err != nil {
			return fmt.Sprintf("Login succeeded but could not save token: %v", err)
		}
		return "Login successful. Token saved to ~/.claw-code/auth.json"

	case "logout":
		if err := auth.ClearTokens(); err != nil {
			return fmt.Sprintf("Logout error: %v", err)
		}
		return "Logged out. Stored tokens cleared."

	case "status":
		s := auth.GetStatus()
		lines := []string{
			fmt.Sprintf("Provider       : %s", m.cfg.ProviderName),
			fmt.Sprintf("Authenticated  : %v", s.Authenticated),
			fmt.Sprintf("Method         : %s", s.Method),
		}
		if s.Method == "oauth" && !s.ExpiresAt.IsZero() {
			lines = append(lines,
				fmt.Sprintf("Token expires  : %s", s.ExpiresAt.Format("2006-01-02 15:04:05 MST")),
				fmt.Sprintf("Has refresh    : %v", s.HasRefresh),
			)
		}
		return strings.Join(lines, "\n")

	default:
		return fmt.Sprintf("Unknown auth subcommand %q. Usage: /auth login | logout | status", sub)
	}
}

// --- /login flow ------------------------------------------------------------

// handleLoginProviderKey handles key input on the provider picker screen.
func (m Model) handleLoginProviderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.state = stateInput
		return m, nil
	case tea.KeyUp:
		if m.loginCursor > 0 {
			m.loginCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.loginCursor < len(loginProviders)-1 {
			m.loginCursor++
		}
		return m, nil
	case tea.KeyEnter:
		chosen := loginProviders[m.loginCursor]
		m.loginProvider = chosen.id
		m.loginCursor = 0

		switch chosen.id {
		case "anthropic":
			m.state = stateLoginMethod
		default:
			m = m.startAPIKeyInput()
		}
		return m, nil
	}
	return m, nil
}

// handleLoginMethodKey handles key input on the auth-method picker (Anthropic).
func (m Model) handleLoginMethodKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.state = stateLoginProvider
		m.loginCursor = 0
		return m, nil
	case tea.KeyUp:
		if m.loginCursor > 0 {
			m.loginCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.loginCursor < len(anthropicAuthMethods)-1 {
			m.loginCursor++
		}
		return m, nil
	case tea.KeyEnter:
		chosen := anthropicAuthMethods[m.loginCursor]
		m.loginCursor = 0
		switch chosen.id {
		case "oauth":
			return m.startOAuthLogin()
		default:
			m = m.startAPIKeyInput()
			return m, nil
		}
	}
	return m, nil
}

// startAPIKeyInput transitions to the API key entry state.
func (m Model) startAPIKeyInput() Model {
	ti := textinput.New()
	ti.Placeholder = "Paste API key and press Enter..."
	ti.EchoMode = textinput.EchoPassword
	ti.CharLimit = 512
	ti.Focus()
	m.loginKeyInput = ti
	m.state = stateLoginAPIKey
	return m
}

// handleLoginAPIKeyKey handles key input when the user is typing an API key.
func (m Model) handleLoginAPIKeyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.state = stateInput
		return m, nil
	case tea.KeyEnter:
		apiKey := strings.TrimSpace(m.loginKeyInput.Value())
		if apiKey == "" {
			m.viewBuf += errorStyle.Render("API key cannot be empty.\n\n")
			m.state = stateInput
			m = m.refreshViewport()
			return m, nil
		}
		saveErr := auth.SetProviderAPIKey(m.loginProvider, apiKey)
		if saveErr != nil {
			return m.handleLoginComplete(loginCompleteMsg{err: saveErr})
		}
		return m.handleLoginComplete(loginCompleteMsg{
			provider: m.loginProvider,
			token:    apiKey,
			method:   "api_key",
		})
	}

	var cmd tea.Cmd
	m.loginKeyInput, cmd = m.loginKeyInput.Update(msg)
	return m, cmd
}

// startOAuthLogin prepares the OAuth session, shows the URL, and waits in background.
func (m Model) startOAuthLogin() (Model, tea.Cmd) {
	session, err := auth.PrepareOAuthFlow()
	if err != nil {
		m.viewBuf += errorStyle.Render(fmt.Sprintf("OAuth setup failed: %v\n\n", err))
		m.state = stateInput
		m = m.refreshViewport()
		return m, nil
	}

	m.state = stateLoginOAuth
	m.viewBuf += statusStyle.Render(fmt.Sprintf(
		"Opening browser for Anthropic OAuth login...\n"+
			"If your browser doesn't open, visit:\n  %s\n\n"+
			"Waiting for callback… (5-minute timeout)\n\n",
		session.AuthURL,
	))
	m = m.refreshViewport()

	return m, tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			td, err := session.Complete()
			if err != nil {
				return loginCompleteMsg{err: err}
			}
			if err := auth.SetProviderOAuth("anthropic", td); err != nil {
				return loginCompleteMsg{err: fmt.Errorf("save token: %w", err)}
			}
			return loginCompleteMsg{
				provider: "anthropic",
				token:    td.AccessToken,
				method:   "oauth",
			}
		},
	)
}

// handleLoginOAuthKey lets the user Ctrl+C to abort the OAuth wait.
func (m Model) handleLoginOAuthKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	return m, nil
}

// handleLoginComplete is called when a /login flow finishes (success or error).
func (m Model) handleLoginComplete(result loginCompleteMsg) (tea.Model, tea.Cmd) {
	m.state = stateInput

	if result.err != nil {
		m.viewBuf += errorStyle.Render(fmt.Sprintf("Login failed: %v\n\n", result.err))
		m = m.refreshViewport()
		return m, nil
	}

	m.cfg.ProviderName = result.provider
	m.cfg.AuthMethod = result.method
	m.cfg.CodexAuthFile = ""
	m.cfg.OpenAIChatGPTAccountID = ""
	if result.method == "oauth" {
		m.cfg.OAuthToken = result.token
		m.cfg.APIKey = ""
	} else {
		m.cfg.APIKey = result.token
		m.cfg.OAuthToken = ""
	}

	switch result.provider {
	case "openai":
		m.cfg.Model = "gpt-4o"
	default:
		m.cfg.Model = runtime.DefaultModel
	}
	m.loop.Config.Model = m.cfg.Model

	client, err := runtime.NewProviderClient(m.cfg)
	if err != nil {
		m.viewBuf += errorStyle.Render(fmt.Sprintf(
			"Login succeeded but could not create provider client: %v\n\n", err))
		m = m.refreshViewport()
		return m, nil
	}
	m.loop.Client = client

	m.viewBuf += statusStyle.Render(fmt.Sprintf(
		"Logged in to %s via %s. Model set to %s. Ready!\n\n",
		result.provider, result.method, m.cfg.Model,
	))
	m = m.refreshViewport()
	return m, nil
}

// handleAskUserKey handles key input when the agent is waiting for a user answer.
func (m Model) handleAskUserKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		// Cancel — send empty string so the agent loop can proceed.
		ch := m.askUserReplyCh
		m.askUserReplyCh = nil
		m.askUserQuestion = ""
		m.state = stateBusy
		m = m.refreshViewport()
		return m, tea.Batch(
			func() tea.Msg { ch <- ""; return nil },
			waitForStream(m.streamChan),
		)
	case tea.KeyEnter:
		answer := strings.TrimSpace(m.askUserInput.Value())
		ch := m.askUserReplyCh
		m.askUserReplyCh = nil
		m.askUserQuestion = ""
		m.state = stateBusy
		m = m.refreshViewport()
		return m, tea.Batch(
			func() tea.Msg { ch <- answer; return nil },
			waitForStream(m.streamChan),
		)
	}
	var cmd tea.Cmd
	m.askUserInput, cmd = m.askUserInput.Update(msg)
	return m, cmd
}

// viewAskUser renders the ask-user question overlay.
func (m Model) viewAskUser() string {
	q := m.askUserQuestion
	if len(q) > 200 {
		q = q[:200] + "..."
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render("Agent Question"),
		"",
		"  "+q,
		"",
		"  "+m.askUserInput.View(),
		"",
		statusStyle.Render("  Enter to answer  •  Esc to skip  •  Ctrl+C to quit"),
	)
	box := helpBoxStyle.Width(min(72, m.width-4)).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// startMessage begins a streaming conversation turn.
func (m Model) startMessage(text string) (tea.Model, tea.Cmd) {
	m.viewBuf += userLabelStyle.Render("You") + ": " + text + "\n\n"
	m.viewBuf += assistantLabelStyle.Render("Claude") + ": "
	m.state = stateBusy
	m.hasStreamContent = false

	ch := make(chan runtime.TurnEvent, 64)
	m.streamChan = ch

	loop := m.loop
	go func() {
		defer close(ch)
		loop.SendMessageStreaming(context.Background(), text, ch) //nolint:errcheck
	}()

	m = m.refreshViewport()
	return m, tea.Batch(
		m.spinner.Tick,
		waitForStream(ch),
	)
}

// handlePickerKey handles keys when the model picker overlay is shown.
func (m Model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	models := m.activeModels()
	switch msg.Type {
	case tea.KeyEsc:
		m.state = stateInput
		return m, nil
	case tea.KeyEnter:
		chosen := models[m.pickerCursor]
		m.cfg.Model = chosen.id
		m.loop.Config.Model = chosen.id
		m.viewBuf += statusStyle.Render(fmt.Sprintf("Model changed to %s\n\n", chosen.id))
		m.state = stateInput
		m = m.refreshViewport()
		return m, nil
	case tea.KeyUp:
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.pickerCursor < len(models)-1 {
			m.pickerCursor++
		}
		return m, nil
	case tea.KeyCtrlC:
		return m, tea.Quit
	}
	return m, nil
}

// handleHelpKey handles keys when the help overlay is shown.
func (m Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc, msg.Type == tea.KeyEnter, msg.String() == "q":
		m.state = stateInput
		return m, nil
	case msg.Type == tea.KeyCtrlC:
		return m, tea.Quit
	}
	return m, nil
}

// handlePermissionKey handles keys when a permission decision is pending.
func (m Model) handlePermissionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var decision runtime.PermDecision
	handled := true

	switch msg.String() {
	case "y", "Y":
		decision = runtime.PermDecisionAllowOnce
	case "a", "A":
		decision = runtime.PermDecisionAllowAlways
	case "n", "N":
		decision = runtime.PermDecisionDeny
	default:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		handled = false
	}

	if !handled {
		return m, nil
	}

	ch := m.permReplyCh
	m.permReplyCh = nil
	m.permToolName = ""
	m.permToolInput = ""
	m.state = stateBusy
	m = m.refreshViewport()

	return m, tea.Batch(
		func() tea.Msg {
			ch <- decision
			return nil
		},
		waitForStream(m.streamChan),
	)
}

// --- View -------------------------------------------------------------------

// View is the Bubble Tea View function.
func (m Model) View() string {
	if !m.ready {
		return "Initializing…\n"
	}

	switch m.state {
	case statePicker:
		return m.viewPicker()
	case stateHelp:
		return m.viewHelp()
	case statePermission:
		return m.viewPermission()
	case stateAskUser:
		return m.viewAskUser()
	case stateLoginProvider:
		return m.viewLoginProvider()
	case stateLoginMethod:
		return m.viewLoginMethod()
	case stateLoginAPIKey:
		return m.viewLoginAPIKey()
	case stateLoginOAuth:
		return m.viewLoginOAuth()
	}

	header := m.renderHeader()
	divider := dividerStyle.Render(strings.Repeat("─", m.width))
	hint := statusStyle.Render("Enter=send  Ctrl+J=newline  ↑↓=history  PgUp/PgDn=scroll")
	statusLine := m.renderStatusBar()
	inputArea := m.renderInputArea()

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		m.viewport.View(),
		divider,
		inputArea,
		hint,
		statusLine,
	)
}

func (m Model) renderHeader() string {
	title := headerStyle.Render("Claw Code v" + appVersion)
	tag := modelTagStyle.Render(fmt.Sprintf("  [%s] %s", m.cfg.ProviderName, m.cfg.Model))
	return title + tag
}

func (m Model) renderStatusBar() string {
	if m.inputTokens > 0 || m.outputTokens > 0 {
		return statusStyle.Render(fmt.Sprintf(
			"Tokens: %s in / %s out  │  Session: %s",
			formatNum(m.inputTokens), formatNum(m.outputTokens),
			m.loop.Session.ID,
		))
	}
	return statusStyle.Render("Session: " + m.loop.Session.ID)
}

// renderInputArea renders the multi-line input with a "> " prefix on the first line.
func (m Model) renderInputArea() string {
	prompt := inputPromptStyle.Render("> ")
	tv := m.textarea.View()
	lines := strings.Split(tv, "\n")
	result := make([]string, len(lines))
	for i, line := range lines {
		if i == 0 {
			result[i] = prompt + line
		} else {
			result[i] = "  " + line
		}
	}
	return strings.Join(result, "\n")
}

// activeModels returns the model list appropriate for the current provider.
func (m Model) activeModels() []modelEntry {
	if m.cfg.ProviderName == "openai" {
		return openAIModels
	}
	return anthropicModels
}

func (m Model) viewPicker() string {
	var b strings.Builder
	b.WriteString(pickerHeaderStyle.Render("Select Model") + "\n")
	b.WriteString(statusStyle.Render("  ↑/↓ navigate  Enter select  Esc cancel") + "\n\n")

	for i, km := range m.activeModels() {
		cursor := "  "
		style := unselectedModelStyle
		if i == m.pickerCursor {
			cursor = "▶ "
			style = selectedModelStyle
		}
		b.WriteString(cursor + style.Render(km.id) + "\n")
		b.WriteString("    " + statusStyle.Render(km.desc) + "\n")
	}

	return b.String()
}

func (m Model) viewHelp() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render("Claw Code — Commands"),
		"",
		statusStyle.Render("Auth & Provider"),
		"  "+userLabelStyle.Render("/login")+"                          Multi-provider login flow",
		"  "+userLabelStyle.Render("/auth")+" login|logout|status       Legacy OAuth commands",
		"",
		statusStyle.Render("Model & Config"),
		"  "+userLabelStyle.Render("/model")+"                          Change the active model (picker)",
		"  "+userLabelStyle.Render("/config")+"                         Show all config values",
		"  "+userLabelStyle.Render("/config")+" <key>                   Show one config value",
		"  "+userLabelStyle.Render("/config")+" <key> <value>           Set config value (saves to project)",
		"  "+userLabelStyle.Render("/init")+"                           Create .claude/settings.json",
		"  "+userLabelStyle.Render("/theme")+" dark|light               Switch TUI color theme",
		"  "+userLabelStyle.Render("/status")+"                         Show model/provider/session info",
		"  "+userLabelStyle.Render("/cost")+"                           Show token usage this session",
		"",
		statusStyle.Render("Session"),
		"  "+userLabelStyle.Render("/clear")+"                          Clear conversation history",
		"  "+userLabelStyle.Render("/session")+" list                   List saved sessions",
		"  "+userLabelStyle.Render("/session")+" save [name]            Save current session",
		"  "+userLabelStyle.Render("/session")+" load <name>            Load a saved session",
		"  "+userLabelStyle.Render("/session-list")+"                   Alias for /session list",
		"",
		statusStyle.Render("Other"),
		"  "+userLabelStyle.Render("/help")+"                           Show this help",
		"  "+userLabelStyle.Render("/exit")+" / "+userLabelStyle.Render("/quit")+"                     Exit (session auto-saved)",
		"",
		statusStyle.Render("Input:"),
		"  "+userLabelStyle.Render("Enter")+"          Send message",
		"  "+userLabelStyle.Render("Ctrl+J")+"         Insert newline (multi-line input)",
		"  "+userLabelStyle.Render("↑ / ↓")+"          Navigate input history (single-line mode)",
		"  "+userLabelStyle.Render("PgUp / PgDn")+"    Scroll conversation",
		"  "+userLabelStyle.Render("Ctrl+C")+"         Exit",
		"",
		statusStyle.Render("Esc / Enter / q to close this panel"),
	)
	box := helpBoxStyle.Width(min(80, m.width-4)).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) viewPermission() string {
	tool := userLabelStyle.Render(m.permToolName)
	inp := m.permToolInput
	if len(inp) > 60 {
		inp = inp[:60] + "..."
	}
	prompt := fmt.Sprintf("Allow %s: %s?", tool, inp)
	hint := statusStyle.Render("[y]es-once  [a]lways  [n]o")
	content := lipgloss.JoinVertical(lipgloss.Left,
		headerStyle.Render("Permission Required"),
		"",
		"  "+prompt,
		"",
		"  "+hint,
	)
	box := helpBoxStyle.Width(min(72, m.width-4)).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// viewLoginProvider renders the provider selection screen.
func (m Model) viewLoginProvider() string {
	var b strings.Builder
	b.WriteString(pickerHeaderStyle.Render("Login — Choose Provider") + "\n")
	b.WriteString(statusStyle.Render("  ↑/↓ navigate  Enter select  Esc cancel") + "\n\n")
	for i, p := range loginProviders {
		cursor := "  "
		style := unselectedModelStyle
		if i == m.loginCursor {
			cursor = "▶ "
			style = selectedModelStyle
		}
		b.WriteString(cursor + style.Render(p.name) + "\n")
		b.WriteString("    " + statusStyle.Render(p.desc) + "\n")
	}
	box := helpBoxStyle.Width(min(60, m.width-4)).Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// viewLoginMethod renders the auth-method selection screen (Anthropic).
func (m Model) viewLoginMethod() string {
	var b strings.Builder
	b.WriteString(pickerHeaderStyle.Render("Login — Anthropic Auth Method") + "\n")
	b.WriteString(statusStyle.Render("  ↑/↓ navigate  Enter select  Esc back") + "\n\n")
	for i, meth := range anthropicAuthMethods {
		cursor := "  "
		style := unselectedModelStyle
		if i == m.loginCursor {
			cursor = "▶ "
			style = selectedModelStyle
		}
		b.WriteString(cursor + style.Render(meth.name) + "\n")
		b.WriteString("    " + statusStyle.Render(meth.desc) + "\n")
	}
	box := helpBoxStyle.Width(min(60, m.width-4)).Render(b.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// viewLoginAPIKey renders the API key entry screen.
func (m Model) viewLoginAPIKey() string {
	providerName := m.loginProvider
	if providerName == "" {
		providerName = "provider"
	}
	content := lipgloss.JoinVertical(lipgloss.Left,
		pickerHeaderStyle.Render(fmt.Sprintf("Login — %s API Key", strings.Title(providerName))), //nolint:staticcheck
		"",
		"  "+statusStyle.Render("Paste your API key below (input is masked):"),
		"  "+m.loginKeyInput.View(),
		"",
		"  "+statusStyle.Render("Enter to confirm  •  Esc to cancel"),
	)
	box := helpBoxStyle.Width(min(70, m.width-4)).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// viewLoginOAuth renders the OAuth waiting screen.
func (m Model) viewLoginOAuth() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		pickerHeaderStyle.Render("Login — Anthropic OAuth"),
		"",
		"  "+m.spinner.View()+" "+statusStyle.Render("Waiting for browser login…"),
		"",
		"  "+statusStyle.Render("Complete the login in your browser, then return here."),
		"  "+statusStyle.Render("Ctrl+C to quit."),
	)
	box := helpBoxStyle.Width(min(70, m.width-4)).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// --- Helpers ----------------------------------------------------------------

// waitForStream returns a tea.Cmd that reads the next event from the stream channel.
func waitForStream(ch <-chan runtime.TurnEvent) tea.Cmd {
	return func() tea.Msg {
		for {
			ev, ok := <-ch
			if !ok {
				return streamDoneMsg{}
			}
			switch ev.Type {
			case runtime.TurnEventTextDelta:
				return streamDeltaMsg{text: ev.Text}
			case runtime.TurnEventToolStart:
				return streamToolMsg{name: ev.ToolName, input: ev.ToolInput}
			case runtime.TurnEventToolDone:
				return streamToolDoneMsg{name: ev.ToolName, result: ev.ToolResult}
			case runtime.TurnEventUsage:
				return streamUsageMsg{inputTokens: ev.InputTokens, outputTokens: ev.OutputTokens}
			case runtime.TurnEventDone:
				return streamDoneMsg{}
			case runtime.TurnEventError:
				return streamErrMsg{err: ev.Err}
			case runtime.TurnEventPermissionAsk:
				return streamPermAskMsg{name: ev.ToolName, input: ev.ToolInput, reply: ev.PermReply}
			case runtime.TurnEventAskUser:
				return streamAskUserMsg{question: ev.ToolInput, reply: ev.AskUserReply}
			}
		}
	}
}

// initViewport creates the viewport and sets textarea width on first resize.
func (m Model) initViewport() Model {
	vpHeight := m.viewportHeight()
	m.viewport = viewport.New(m.width, vpHeight)
	m.viewport.SetContent(m.viewBuf)
	m.viewport.GotoBottom()
	m.textarea.SetWidth(max(m.width-2, 10))
	return m
}

// resizeViewport updates dimensions after a window resize.
func (m Model) resizeViewport() Model {
	m.viewport.Width = m.width
	m.viewport.Height = m.viewportHeight()
	m.textarea.SetWidth(max(m.width-2, 10))
	return m
}

// viewportHeight calculates the viewport height from the terminal height.
// Layout overhead: header(1) + divider(1) + textarea(textareaRows) + hint(1) + status(1).
func (m Model) viewportHeight() int {
	overhead := 4 + textareaRows // header + divider + textarea + hint + status
	h := m.height - overhead
	if h < 1 {
		h = 1
	}
	return h
}

// refreshViewport rebuilds viewport content from current buffers.
func (m Model) refreshViewport() Model {
	content := m.viewBuf + m.streamBuf
	if m.state == stateBusy && !m.hasStreamContent {
		content += m.spinner.View() + statusStyle.Render(" Thinking…\n")
	}
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
	return m
}

// truncate shortens s to at most n runes, appending "…" if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// formatNum formats an integer with comma separators.
// formatSessionList renders a table-style listing of session metadata.
func formatSessionList(metas []runtime.SessionMeta) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%-40s  %-19s  %6s  %8s  %8s\n",
		"ID", "Updated", "Msgs", "In tok", "Out tok"))
	sb.WriteString(strings.Repeat("-", 90) + "\n")
	for _, m := range metas {
		ts := m.UpdatedAt.Format("2006-01-02 15:04:05")
		id := m.ID
		if len(id) > 38 {
			id = id[:35] + "..."
		}
		sb.WriteString(fmt.Sprintf("%-40s  %-19s  %6d  %8s  %8s\n",
			id, ts, m.MessageCount,
			formatNum(m.TotalInputTokens),
			formatNum(m.TotalOutputTokens)))
	}
	return sb.String()
}

func formatNum(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, c)
	}
	return string(result)
}
