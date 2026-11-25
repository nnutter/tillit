package status

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/nnutter/tillit/internal/auditor"
	"github.com/nnutter/tillit/internal/daemon"
	"github.com/nnutter/tillit/internal/ui"
	"github.com/nnutter/tillit/internal/verifier"
)

// KeyMap implements help.KeyMap for the status view.
type KeyMap struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Top       key.Binding
	Bottom    key.Binding
	Detail    key.Binding
	Quit      key.Binding
	Help      key.Binding
	ForceQuit key.Binding
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Top, k.Bottom},
		{k.Detail, k.Help, k.Quit, k.ForceQuit},
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Detail, k.Help}
}

func defaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("b", "pgup"),
			key.WithHelp("b/pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("f", "pgdown", "space"),
			key.WithHelp("f/pgdn", "page down"),
		),
		Top: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g/home", "go to start"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G/end", "go to end"),
		),
		Detail: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "view feedback"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "esc"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "full help"),
		),
		ForceQuit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "force quit"),
		),
	}
}

// model is the Bubble Tea model for `tillit status`.
// It periodically refreshes from the daemon so that pending/running states
// transition to passed/failed without manual restart.
type model struct {
	theme         ui.Theme
	branch        auditor.BranchResult
	verification  verifier.BranchResult
	daemonRunning bool

	repo      string
	interval  time.Duration
	lastFetch time.Time
	fetchErr  error

	width  int
	height int

	table table.Model
	help  help.Model
	keys  KeyMap

	// Pop-over feedback detail.
	showDetail     bool
	detailHash     string
	detailOutcome  auditor.BranchOutcome
	detailViewport viewport.Model
	detailLoading  bool
	pendingG       bool // for vim-style gg
}

type refreshMsg struct {
	branch        auditor.BranchResult
	verification  verifier.BranchResult
	daemonRunning bool
	fetchErr      error
	at            time.Time
}

type feedbackMsg struct {
	hash    string
	outcome auditor.BranchOutcome
	err     error
}

// NewModel creates a status model.
func NewModel(branch auditor.BranchResult, verification verifier.BranchResult, daemonRunning bool, theme ui.Theme) model {
	return NewModelWithRepo(branch, verification, daemonRunning, theme, "", 250*time.Millisecond)
}

// NewModelWithRepo creates a status model with periodic refresh.
func NewModelWithRepo(branch auditor.BranchResult, verification verifier.BranchResult, daemonRunning bool, theme ui.Theme, repo string, interval time.Duration) model {
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	m := model{
		theme:         theme,
		branch:        branch,
		verification:  verification,
		daemonRunning: daemonRunning,
		repo:          repo,
		interval:      interval,
		lastFetch:     time.Now(),
		keys:          defaultKeyMap(),
	}

	// Build table columns/rows.
	cols, rows := buildTableData(branch, verification, theme)
	m.table = table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithWidth(80),
		table.WithHeight(20),
		table.WithFocused(true),
		table.WithStyles(tableStyles(theme)),
		table.WithKeyMap(table.DefaultKeyMap()),
	)
	// Apply our keymap to help.
	m.help = help.New()
	m.help.SetWidth(80)
	m.detailViewport = viewport.New(viewport.WithWidth(70), viewport.WithHeight(16))
	m.detailViewport.SoftWrap = true
	m.detailViewport.KeyMap = detailViewportKeyMap()
	return m
}

func detailViewportKeyMap() viewport.KeyMap {
	km := viewport.DefaultKeyMap()
	km.PageUp = key.NewBinding(
		key.WithKeys("ctrl+b", "b", "pgup"),
		key.WithHelp("^b/b/pgup", "page up"),
	)
	km.PageDown = key.NewBinding(
		key.WithKeys("ctrl+f", "f", "pgdown"),
		key.WithHelp("^f/f/pgdn", "page down"),
	)
	return km
}

func fetchStatusMsg(ctx context.Context, repo string, at time.Time) refreshMsg {
	running, err := daemon.Running(repo)
	if err != nil {
		return refreshMsg{fetchErr: fmt.Errorf("report daemon: %w", err), at: at}
	}
	branch, err := auditor.ReportBranch(ctx, repo)
	if err != nil {
		return refreshMsg{fetchErr: err, at: at}
	}
	normalizeValidationRunning(&branch, running)
	verification, err := verifier.ReportBranch(ctx, repo)
	if err != nil {
		return refreshMsg{fetchErr: fmt.Errorf("report verification: %w", err), at: at}
	}
	if !sameSnapshot(branch.Snapshot, verification.Snapshot) {
		return refreshMsg{fetchErr: fmt.Errorf("branch changed while collecting status; retry"), at: at}
	}
	return refreshMsg{
		branch:        branch,
		verification:  verification,
		daemonRunning: running,
		at:            at,
	}
}

func fetchFeedbackMsg(ctx context.Context, repo, hash string) feedbackMsg {
	result, err := auditor.ReportEvidence(ctx, repo)
	if err != nil {
		return feedbackMsg{hash: hash, err: err}
	}
	for _, outcome := range result.Outcomes {
		if outcome.Hash == hash {
			return feedbackMsg{hash: hash, outcome: outcome}
		}
	}
	return feedbackMsg{hash: hash, err: fmt.Errorf("commit not found in feedback: %s", hash)}
}

func isNonEmpty(s string) bool { return s != "" }
func hasFailureTitle(f auditor.Failure) bool {
	return isNonEmpty(f.Kind) || isNonEmpty(f.Package) || isNonEmpty(f.Test)
}

func formatFeedbackHeader(outcome auditor.BranchOutcome, theme ui.Theme) string {
	var b strings.Builder
	b.WriteString(theme.DetailTitle.Render(fmt.Sprintf("Commit %s  %s", ui.ShortHash(outcome.Hash), outcome.Subject)))
	b.WriteString("\n")
	b.WriteString(theme.Subtitle.Render(fmt.Sprintf("hash %s  validation: %s", outcome.Hash, outcome.State)))
	b.WriteString("\n")
	if hasError(outcome.Error) {
		b.WriteString(outcome.Error)
		b.WriteString("\n")
	}
	return b.String()
}

func formatFeedbackBody(ev *auditor.Evidence) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  Command: %q\n", ev.Command))
	if len(ev.Failures) == 0 {
		b.WriteString(strings.TrimRight(ev.Output, "\n"))
		b.WriteString("\n")
	}
	for _, f := range ev.Failures {
		if hasFailureTitle(f) {
			b.WriteString(fmt.Sprintf(" %s %s %s\n", f.Kind, f.Package, f.Test))
		}
		if isNonEmpty(f.Output) {
			b.WriteString(strings.TrimRight(f.Output, "\n"))
			b.WriteString("\n")
		}
		if f.OutputTruncated {
			b.WriteString("    Failure output truncated\n")
		}
		for _, r := range f.Reproductions {
			b.WriteString(fmt.Sprintf("    Rerun (%s): %q\n", r.State, r.Command))
		}
	}
	if ev.OutputTruncated {
		b.WriteString("  Command transcript truncated\n")
	}
	return b.String()
}

func formatFeedback(outcome auditor.BranchOutcome, theme ui.Theme) string {
	var b strings.Builder
	b.WriteString(formatFeedbackHeader(outcome, theme))
	if outcome.Evidence == nil {
		b.WriteString("\n")
		b.WriteString("No feedback recorded for this commit.\n")
		b.WriteString("It is either pending/running (daemon still validating)\nor has passed without evidence (check `tillit feedback --json`).\n")
		return b.String()
	}
	b.WriteString(formatFeedbackBody(outcome.Evidence))
	return b.String()
}

func tableStyles(theme ui.Theme) table.Styles {
	// Derive from theme; fall back to defaults when plain.
	if !theme.ColorEnabled {
		return table.DefaultStyles()
	}
	return table.Styles{
		Header:   theme.TableHeader,
		Cell:     theme.TableCell,
		Selected: theme.TableSelected,
	}
}

func buildTableData(branch auditor.BranchResult, verification verifier.BranchResult, theme ui.Theme) ([]table.Column, []table.Row) {
	cols := []table.Column{
		{Title: "Commit", Width: 10},
		{Title: "Subject", Width: 32},
		{Title: "Validation", Width: 14},
		{Title: "Verification", Width: 28},
	}

	byHash := make(map[string]verifier.BranchOutcome, len(verification.Outcomes))
	for _, out := range verification.Outcomes {
		byHash[out.Hash] = out
	}

	var rows []table.Row
	for _, item := range branch.Outcomes {
		hash := ui.ShortHash(item.Hash)
		subject := truncateSubject(item.Subject, 32)
		val := theme.ValidationBadge(item.State)
		// For table we want badge text (icon + state) when colored, plain when not.
		// ValidationBadge already returns plain when !ColorEnabled.
		// Append error if present.
		if hasError(item.Error) {
			if isColorDisabled(theme) {
				val = fmt.Sprintf("%s: %s", item.State, item.Error)
			} else if isColorEnabled(theme) {
				// Show error style inside badge already via theme; append as muted second line is complex.
				// Instead embed error in badge string.
				val = fmt.Sprintf("%s %s", theme.ValidationBadge(item.State), theme.Subtitle.Render(item.Error))
			}
		}

		vOut := byHash[item.Hash]
		verif := verificationBadge(vOut, theme)

		rows = append(rows, table.Row{hash, subject, val, verif})
	}
	if len(rows) == 0 {
		rows = []table.Row{{"", "No commits selected for this branch", "", ""}}
	}
	return cols, rows
}

func verificationBadge(out verifier.BranchOutcome, theme ui.Theme) string {
	if out.Error != "" {
		return theme.VerificationBadge("", "", out.Error)
	}
	if out.Result == nil {
		return theme.VerificationBadge("", "", "")
	}
	return theme.VerificationBadge(string(out.Result.Status), out.Result.Reason, "")
}

func truncateSubject(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func overlayWith(base, overlay string, width, height int) string {
	// Use lipgloss.Place to center overlay over base when we have dimensions.
	if !shouldOverlay(width, height) {
		return overlay
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, overlay,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle()),
	)
}

func (m model) Init() tea.Cmd {
	if m.repo == "" {
		return nil
	}
	return m.refreshCmd()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case refreshMsg:
		return updateRefreshMsg(&m, msg)
	case feedbackMsg:
		return updateFeedbackMsg(&m, msg)
	case tea.WindowSizeMsg:
		return updateWindowSizeMsg(&m, msg)
	case tea.BackgroundColorMsg:
		return updateBackgroundMsg(&m, msg)
	case tea.KeyPressMsg:
		return updateKeyMsg(&m, msg)
	default:
		return updateOtherMsg(&m, msg)
	}
}

func (m model) View() tea.View {
	var b strings.Builder

	// Header - semantic.
	title := m.theme.Title.Render(" Tillit Status ")
	b.WriteString(title)
	b.WriteString("\n")

	if len(m.branch.Snapshot.Branch) > 0 {
		branchLine := fmt.Sprintf("Branch: %s   Head: %s   Base: %s",
			m.branch.Snapshot.Branch,
			ui.ShortHash(m.branch.Snapshot.Head),
			ui.ShortHash(m.branch.Snapshot.Base),
		)
		b.WriteString(m.theme.BranchInfo.Render(branchLine))
		b.WriteString("\n")
	}

	// Daemon + summary inline.
	daemonStr := m.theme.DaemonBadge(m.daemonRunning)
	if m.theme.ColorEnabled {
		daemonStr = fmt.Sprintf("Daemon %s", daemonStr)
	} else {
		daemonStr = fmt.Sprintf("daemon: %s", daemonStr)
	}
	summary := buildSummary(m.branch, m.verification, m.theme)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Left,
		daemonStr,
		"   ",
		summary,
	))
	b.WriteString("\n")

	// Refresh indicator - shows periodic polling is active.
	if m.repo != "" {
		refreshLine := fmt.Sprintf("auto-refresh %s • last updated %s", m.interval, m.lastFetch.Format("15:04:05"))
		if m.fetchErr != nil {
			refreshLine += fmt.Sprintf(" • %v", m.fetchErr)
		}
		b.WriteString(m.theme.Subtitle.Render(refreshLine))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// Table.
	b.WriteString(m.table.View())
	b.WriteString("\n\n")

	// Help footer.
	b.WriteString(m.help.View(m.keys))

	// Apply document padding.
	content := b.String()
	if m.theme.ColorEnabled {
		content = m.theme.Document.Render(content)
	}

	// Overlay the feedback pop-over when a commit is selected.
	if m.showDetail {
		overlay := m.renderDetail()
		if overlay != "" {
			content = overlayWith(content, overlay, m.width, m.height)
		}
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "Tillit Status"
	return v
}

func (m *model) rebuildTableLocked() {
	cols, rows := buildTableData(m.branch, m.verification, m.theme)
	m.table.SetColumns(cols)
	m.table.SetRows(rows)
}

func (m model) refreshCmd() tea.Cmd {
	if m.repo == "" {
		return nil
	}
	repo := m.repo
	interval := m.interval
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		msg := fetchStatusMsg(ctx, repo, t)
		return msg
	})
}

func (m model) renderDetail() string {
	// Title for the pop-over.
	outcome := m.detailOutcome
	// Fall back to the branch outcome if feedback not yet loaded.
	if outcome.Hash == "" && isDetailHashNonEmpty(&m) {
		for _, o := range m.branch.Outcomes {
			if o.Hash == m.detailHash {
				outcome = o
				break
			}
		}
	}
	title := fmt.Sprintf("Feedback — %s  %s", ui.ShortHash(outcome.Hash), outcome.Subject)
	if m.detailLoading {
		title += "  (loading…)"
	}

	// Build the pop-over body with a scrollable viewport.
	inner := strings.Builder{}
	inner.WriteString(m.theme.DetailTitle.Render(title))
	inner.WriteString("\n")
	if m.detailLoading {
		inner.WriteString(m.detailViewport.View())
	} else {
		inner.WriteString(m.detailViewport.View())
	}
	inner.WriteString("\n")
	footer := "gg/G top/bottom  •  ^b/^f page  •  ↑/↓ scroll  •  esc to close"
	if !m.theme.ColorEnabled {
		footer = "gg/G top/bottom  ^b/^f page  up/down scroll  esc to close"
	}
	inner.WriteString(m.theme.DetailFooter.Render(footer))

	body := inner.String()
	if m.theme.ColorEnabled {
		body = m.theme.DetailBorder.Render(body)
		// Also render with a faint background dim via lipgloss?
	}
	return body
}

func updateRefreshMsg(m *model, msg refreshMsg) (tea.Model, tea.Cmd) {
	m.lastFetch = msg.at
	m.fetchErr = msg.fetchErr
	if msg.fetchErr == nil {
		m.branch = msg.branch
		m.verification = msg.verification
		m.daemonRunning = msg.daemonRunning
		m.rebuildTableLocked()
		if isShowingDetail(m) {
			return *m, m.refreshCmd()
		}
	}
	return *m, m.refreshCmd()
}

func updateFeedbackMsg(m *model, msg feedbackMsg) (tea.Model, tea.Cmd) {
	m.detailLoading = false
	m.detailOutcome = msg.outcome
	if msg.err != nil {
		m.detailViewport.SetContent(fmt.Sprintf("Error loading feedback: %v", msg.err))
	} else {
		m.detailViewport.SetContent(formatFeedback(msg.outcome, m.theme))
		m.detailViewport.GotoTop()
	}
	return *m, nil
}

func updateWindowSizeMsg(m *model, msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	w := max(40, msg.Width-4)
	h := max(8, msg.Height-12)
	m.table.SetWidth(w)
	m.table.SetHeight(h)
	m.help.SetWidth(w)
	vw := max(40, w-10)
	vh := max(8, h-8)
	m.detailViewport.SetWidth(vw)
	m.detailViewport.SetHeight(vh)
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return *m, cmd
}

func updateBackgroundMsg(m *model, msg tea.BackgroundColorMsg) (tea.Model, tea.Cmd) {
	m.theme = ui.New(m.theme.ColorEnabled, msg.IsDark())
	m.table.SetStyles(tableStyles(m.theme))
	m.help.Styles = help.DefaultStyles(msg.IsDark())
	if m.showDetail {
		m.detailViewport.SetContent(formatFeedback(m.detailOutcome, m.theme))
		m.detailViewport.GotoTop()
	}
	return *m, nil
}

func updateOtherMsg(m *model, msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.showDetail {
		m.detailViewport, cmd = m.detailViewport.Update(msg)
	} else {
		m.table, cmd = m.table.Update(msg)
	}
	return *m, cmd
}

func updateKeyMsg(m *model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.showDetail {
		return updateDetailKey(m, msg)
	}
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		return *m, tea.Quit
	case key.Matches(msg, m.keys.Quit):
		return *m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return *m, nil
	case key.Matches(msg, m.keys.Detail):
		return openDetail(m)
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return *m, cmd
}

func updateDetailKey(m *model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		return *m, tea.Quit
	case key.Matches(msg, m.keys.Quit):
		m.showDetail = false
		m.detailHash = ""
		m.pendingG = false
		return *m, nil
	case key.Matches(msg, m.keys.Detail):
		m.showDetail = false
		m.detailHash = ""
		m.pendingG = false
		return *m, nil
	}
	// gg goes to top, G goes to bottom.
	if msg.String() == "g" {
		if m.pendingG {
			m.pendingG = false
			m.detailViewport.GotoTop()
			return *m, nil
		}
		m.pendingG = true
		return *m, nil
	}
	if msg.String() == "G" {
		m.pendingG = false
		m.detailViewport.GotoBottom()
		return *m, nil
	}
	// Reset pendingG on any other key (vim-style cancels gg).
	m.pendingG = false
	var cmd tea.Cmd
	m.detailViewport, cmd = m.detailViewport.Update(msg)
	return *m, cmd
}

func openDetail(m *model) (tea.Model, tea.Cmd) {
	if m.repo == "" {
		return *m, nil
	}
	cursor := m.table.Cursor()
	if !isCursorValid(cursor, len(m.branch.Outcomes)) {
		return *m, nil
	}
	m.showDetail = true
	m.detailHash = m.branch.Outcomes[cursor].Hash
	m.detailLoading = true
	m.detailViewport.SetContent("Loading feedback…")
	m.detailViewport.GotoTop()
	if m.width > 0 {
		m.detailViewport.SetWidth(max(40, m.width-10))
		m.detailViewport.SetHeight(max(8, m.height-8))
	}
	return *m, tea.Batch(
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return fetchFeedbackMsg(ctx, m.repo, m.detailHash)
		},
		nil,
	)
}

func isColorEnabled(t ui.Theme) bool  { return t.ColorEnabled }
func isColorDisabled(t ui.Theme) bool { return !t.ColorEnabled }
func hasError(s string) bool          { return s != "" }
func isPositive(n int) bool           { return n > 0 }
func isDetailHashNonEmpty(m *model) bool {
	return m != nil && isNonEmpty(m.detailHash)
}

func isShowingDetail(m *model) bool {
	return m.showDetail && isDetailHashNonEmpty(m)
}
func isNonNegative(n int) bool { return n >= 0 }
func isLessThan(a, b int) bool { return a < b }
func isCursorValid(cursor, length int) bool {
	return isNonNegative(cursor) && isLessThan(cursor, length)
}

func shouldOverlay(width, height int) bool {
	return isPositive(width) && isPositive(height)
}

func countValidation(outcomes []auditor.BranchOutcome) (passed, failed, pending, running, errCount int) {
	for _, o := range outcomes {
		switch o.State {
		case "passed":
			passed++
		case "failed":
			failed++
		case "error":
			errCount++
		case "running":
			running++
		default:
			pending++
		}
	}
	return
}

func countVerification(outcomes []verifier.BranchOutcome) (verified, rejected, unsupported, vpending, verr int) {
	for _, o := range outcomes {
		if o.Error != "" {
			verr++
			continue
		}
		if o.Result == nil {
			vpending++
			continue
		}
		switch o.Result.Status {
		case verifier.Verified:
			verified++
		case verifier.Rejected:
			rejected++
		case verifier.Unsupported:
			unsupported++
		default:
			vpending++
		}
	}
	return
}

func buildSummary(branch auditor.BranchResult, verification verifier.BranchResult, theme ui.Theme) string {
	passed, failed, pending, running, errCount := countValidation(branch.Outcomes)
	verified, rejected, unsupported, vpending, verr := countVerification(verification.Outcomes)

	if isColorDisabled(theme) {
		return fmt.Sprintf("Validation: %d passed, %d failed, %d pending, %d running, %d error  •  Verification: %d verified, %d rejected, %d unsupported, %d pending, %d error",
			passed, failed, pending, running, errCount,
			verified, rejected, unsupported, vpending, verr)
	}
	return buildColoredSummary(passed, failed, pending, running, errCount, verified, rejected, unsupported, vpending, verr, theme)
}

func buildColoredSummary(passed, failed, pending, running, errCount, verified, rejected, unsupported, vpending, verr int, theme ui.Theme) string {
	label := theme.SummaryLabel

	validParts := lipgloss.JoinHorizontal(lipgloss.Left,
		label.Render("Validation: "),
		theme.ValidationPassed.Render(fmt.Sprintf("%d passed", passed)), ", ",
		theme.ValidationFailed.Render(fmt.Sprintf("%d failed", failed)), ", ",
		label.Render(fmt.Sprintf("%d pending", pending)),
		", ", theme.ValidationRunning.Render(fmt.Sprintf("%d running", running)),
	)
	if isPositive(errCount) {
		validParts = lipgloss.JoinHorizontal(lipgloss.Left,
			validParts, ", ", theme.ValidationError.Render(fmt.Sprintf("%d error", errCount)),
		)
	}
	verifParts := lipgloss.JoinHorizontal(lipgloss.Left,
		label.Render("Verification: "),
		theme.VerificationVerified.Render(fmt.Sprintf("%d verified", verified)), ", ",
		theme.VerificationRejected.Render(fmt.Sprintf("%d rejected", rejected)), ", ",
		theme.VerificationUnsupported.Render(fmt.Sprintf("%d unsupported", unsupported)),
	)
	verifParts = appendPendingVerif(verifParts, vpending, verr, theme, label)
	return lipgloss.JoinHorizontal(lipgloss.Left, validParts, "  │  ", verifParts)
}

func appendPendingVerif(parts string, vpending, verr int, theme ui.Theme, label lipgloss.Style) string {
	if isPositive(vpending) || isPositive(verr) {
		parts = lipgloss.JoinHorizontal(lipgloss.Left,
			parts, ", ", label.Render(fmt.Sprintf("%d pending", vpending)),
		)
		if isPositive(verr) {
			parts = lipgloss.JoinHorizontal(lipgloss.Left,
				parts, ", ", theme.VerificationError.Render(fmt.Sprintf("%d error", verr)),
			)
		}
	}
	return parts
}

// Ensure imports used.
var (
	_ = auditor.BranchResult{}
	_ = verifier.BranchResult{}
)
