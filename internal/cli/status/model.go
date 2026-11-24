package status

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/nnutter/tillit/internal/auditor"
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
	Quit      key.Binding
	Help      key.Binding
	ForceQuit key.Binding
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.PageUp, k.PageDown, k.Top, k.Bottom},
		{k.Help, k.Quit, k.ForceQuit},
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Help, k.Quit}
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
type model struct {
	theme         ui.Theme
	branch        auditor.BranchResult
	verification  verifier.BranchResult
	daemonRunning bool

	width  int
	height int

	table table.Model
	help  help.Model
	keys  KeyMap
}

// NewModel creates a status model.
func NewModel(branch auditor.BranchResult, verification verifier.BranchResult, daemonRunning bool, theme ui.Theme) model {
	m := model{
		theme:         theme,
		branch:        branch,
		verification:  verification,
		daemonRunning: daemonRunning,
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
	return m
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

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Resize table proportionally.
		// Keep at least small size.
		w := max(40, msg.Width-4)
		h := max(8, msg.Height-12)
		m.table.SetWidth(w)
		m.table.SetHeight(h)
		m.help.SetWidth(w)
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd

	case tea.BackgroundColorMsg:
		// Adapt theme to light/dark.
		m.theme = ui.New(m.theme.ColorEnabled, msg.IsDark())
		// Rebuild styles.
		m.table.SetStyles(tableStyles(m.theme))
		m.help.Styles = help.DefaultStyles(msg.IsDark())
		return m, nil

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keys.ForceQuit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
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
	b.WriteString("\n\n")

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

	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "Tillit Status"
	return v
}

func isColorEnabled(t ui.Theme) bool  { return t.ColorEnabled }
func isColorDisabled(t ui.Theme) bool { return !t.ColorEnabled }
func hasError(s string) bool          { return s != "" }
func isPositive(n int) bool           { return n > 0 }

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
