package ui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// Theme encodes the Tillit design language.
//
// It is intentionally semantic: callers describe meaning
// (validation passed, verification unsupported, daemon running)
// and the theme renders a consistent visual.
//
// All colors are chosen as Light/Dark pairs and selected with
// lipgloss.LightDark so the same code adapts to light and dark
// terminals. When ColorEnabled is false every style is a no-op
// and Render returns plain text - useful for piping and tests.
type Theme struct {
	ColorEnabled bool
	HasDark      bool

	// Title is the main title style.
	Title lipgloss.Style
	// Subtitle is muted secondary text.
	Subtitle lipgloss.Style
	// BranchInfo renders branch, tip and boundary metadata.
	BranchInfo lipgloss.Style
	// DaemonRunning renders the running daemon badge.
	DaemonRunning lipgloss.Style
	// DaemonStopped renders the stopped daemon badge.
	DaemonStopped lipgloss.Style
	// SummaryLabel is the small label for summary items.
	SummaryLabel lipgloss.Style
	// SummaryValue is the numeric/value part of a summary.
	SummaryValue lipgloss.Style
	// Document is the outer padding for the whole view.
	Document lipgloss.Style
	// Section is inner section padding.
	Section lipgloss.Style
	// Help is the footer help text.
	Help lipgloss.Style

	// Validation styles.
	ValidationPassed  lipgloss.Style
	ValidationFailed  lipgloss.Style
	ValidationPending lipgloss.Style
	ValidationRunning lipgloss.Style
	ValidationError   lipgloss.Style

	// Verification styles.
	VerificationVerified    lipgloss.Style
	VerificationRejected    lipgloss.Style
	VerificationUnsupported lipgloss.Style
	VerificationPending     lipgloss.Style
	VerificationError       lipgloss.Style

	// Table styles.
	TableHeader   lipgloss.Style
	TableCell     lipgloss.Style
	TableSelected lipgloss.Style
	TableCursor   lipgloss.Style

	// Pop-over / detail styles for feedback view.
	DetailBorder  lipgloss.Style
	DetailTitle   lipgloss.Style
	DetailContent lipgloss.Style
	DetailFooter  lipgloss.Style

	// Semantic symbols.
	SymPassed            string
	SymFailed            string
	SymPending           string
	SymRunningValidation string
	SymError             string
	SymVerified          string
	SymRejected          string
	SymUnsupported       string
	SymRunning           string
	SymStopped           string
}

// New creates a theme.
//
// colorEnabled controls whether any color or decoration is emitted.
// hasDark selects the light/dark palette. When colorEnabled is false
// hasDark is ignored.
func New(colorEnabled, hasDark bool) Theme {
	t := Theme{
		ColorEnabled: colorEnabled,
		HasDark:      hasDark,

		SymPassed:            "✓",
		SymFailed:            "✗",
		SymPending:           "○",
		SymRunningValidation: "◐",
		SymError:             "⚠",
		SymVerified:          "✓",
		SymRejected:          "✗",
		SymUnsupported:       "○",
		SymRunning:           "●",
		SymStopped:           "○",
	}

	if !colorEnabled {
		// All styles remain no-op. Keep them as empty lipgloss styles
		// so Render is plain text.
		t.Title = lipgloss.NewStyle()
		t.Subtitle = lipgloss.NewStyle()
		t.BranchInfo = lipgloss.NewStyle()
		t.DaemonRunning = lipgloss.NewStyle()
		t.DaemonStopped = lipgloss.NewStyle()
		t.SummaryLabel = lipgloss.NewStyle()
		t.SummaryValue = lipgloss.NewStyle()
		t.Document = lipgloss.NewStyle()
		t.Section = lipgloss.NewStyle()
		t.Help = lipgloss.NewStyle().Faint(true)
		t.ValidationPassed = lipgloss.NewStyle()
		t.ValidationFailed = lipgloss.NewStyle()
		t.ValidationPending = lipgloss.NewStyle()
		t.ValidationRunning = lipgloss.NewStyle()
		t.ValidationError = lipgloss.NewStyle()
		t.VerificationVerified = lipgloss.NewStyle()
		t.VerificationRejected = lipgloss.NewStyle()
		t.VerificationUnsupported = lipgloss.NewStyle()
		t.VerificationPending = lipgloss.NewStyle()
		t.VerificationError = lipgloss.NewStyle()
		t.TableHeader = lipgloss.NewStyle()
		t.TableCell = lipgloss.NewStyle()
		t.TableSelected = lipgloss.NewStyle()
		t.TableCursor = lipgloss.NewStyle()
		t.DetailBorder = lipgloss.NewStyle()
		t.DetailTitle = lipgloss.NewStyle()
		t.DetailContent = lipgloss.NewStyle()
		t.DetailFooter = lipgloss.NewStyle()
		return t
	}

	ld := lipgloss.LightDark(hasDark)

	// Palette - semantic, not literal hex names.
	// Each pair is (light, dark) to keep contrast on both backgrounds.
	cSuccess := ld(lipgloss.Color("#15803D"), lipgloss.Color("#4ADE80"))
	cDanger := ld(lipgloss.Color("#B91C1C"), lipgloss.Color("#F87171"))
	cWarning := ld(lipgloss.Color("#92400E"), lipgloss.Color("#FBBF24"))
	cMuted := ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF"))
	cPrimary := ld(lipgloss.Color("#6D28D9"), lipgloss.Color("#A78BFA"))
	cError := ld(lipgloss.Color("#DC2626"), lipgloss.Color("#EF4444"))
	cInfo := ld(lipgloss.Color("#0369A1"), lipgloss.Color("#38BDF8"))

	t.Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(cPrimary).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder(), false, false, false, false).
		BorderForeground(cPrimary)

	t.Subtitle = lipgloss.NewStyle().
		Foreground(cMuted).
		Italic(true)

	t.BranchInfo = lipgloss.NewStyle().
		Foreground(cMuted)

	t.DaemonRunning = lipgloss.NewStyle().
		Bold(true).
		Foreground(cSuccess)

	t.DaemonStopped = lipgloss.NewStyle().
		Foreground(cMuted)

	t.SummaryLabel = lipgloss.NewStyle().
		Foreground(cMuted)

	t.SummaryValue = lipgloss.NewStyle().Bold(true)

	t.Document = lipgloss.NewStyle().Padding(1, 2)
	t.Section = lipgloss.NewStyle().Padding(0, 0)
	// Faint help.
	t.Help = lipgloss.NewStyle().
		Foreground(cMuted).
		Italic(true)

	// Validation - semantic state colors.
	t.ValidationPassed = lipgloss.NewStyle().
		Bold(true).
		Foreground(cSuccess)
	t.ValidationFailed = lipgloss.NewStyle().
		Bold(true).
		Foreground(cDanger)
	t.ValidationPending = lipgloss.NewStyle().
		Foreground(cWarning)
	t.ValidationRunning = lipgloss.NewStyle().
		Bold(true).
		Foreground(cInfo)
	t.ValidationError = lipgloss.NewStyle().
		Bold(true).
		Foreground(cError)

	// Verification - distinct hues so validation vs verification are scannable.
	t.VerificationVerified = lipgloss.NewStyle().
		Bold(true).
		Foreground(cSuccess)
	t.VerificationRejected = lipgloss.NewStyle().
		Bold(true).
		Foreground(cDanger)
	t.VerificationUnsupported = lipgloss.NewStyle().
		Foreground(cWarning)
	t.VerificationPending = lipgloss.NewStyle().
		Foreground(cMuted)
	t.VerificationError = lipgloss.NewStyle().
		Bold(true).
		Foreground(cError)

	// Table - keep readable, not noisy.
	t.TableHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(cPrimary).
		Padding(0, 1)
	t.TableCell = lipgloss.NewStyle().
		Padding(0, 1)
	t.TableSelected = lipgloss.NewStyle().
		Bold(true).
		Foreground(cInfo).
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(cInfo).
		Padding(0, 0, 0, 1)
	t.TableCursor = lipgloss.NewStyle().
		Foreground(cInfo)

	// Pop-over detail - centered feedback window.
	t.DetailBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cPrimary).
		Background(ld(lipgloss.Color("#FFFFFF"), lipgloss.Color("#0F0F0F"))).
		Padding(1, 2)
	t.DetailTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(cPrimary)
	t.DetailContent = lipgloss.NewStyle().
		Foreground(ld(lipgloss.Color("#1a1a1a"), lipgloss.Color("#dddddd")))
	t.DetailFooter = lipgloss.NewStyle().
		Foreground(cMuted).
		Italic(true)

	return t
}

// Default returns a theme auto-detecting color and background.
// It is the same as calling New with detection from the environment.
func Default() Theme {
	colorEnabled := true // lipgloss will downsample when piping via colorprofile
	hasDark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	return New(colorEnabled, hasDark)
}

// DaemonBadge renders daemon state.
func (t Theme) DaemonBadge(running bool) string {
	if running {
		if !t.ColorEnabled {
			return "running"
		}
		return t.DaemonRunning.Render(t.SymRunning + " running")
	}
	if !t.ColorEnabled {
		return "stopped"
	}
	return t.DaemonStopped.Render(t.SymStopped + " stopped")
}

// SectionTitle renders a section heading.
func (t Theme) SectionTitle(title string) string {
	if !t.ColorEnabled {
		return title
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#6D28D9")).
		Render(title)
}

// ValidationBadge returns "✓ passed" style badge.
func (t Theme) ValidationBadge(state string) string {
	label := state
	if label == "" {
		label = "pending"
	}
	icon := t.ValidationIcon(state)
	style := t.ValidationStyle(state)
	if !t.ColorEnabled {
		// Plain mode: keep triple "hash subject state" possible
		// callers that need plain triple should use raw state.
		// Here we return icon-less plain for minimal surprise.
		// But for interactive we want icon.
		// Since colorEnabled false, we just return plain label.
		return label
	}
	return style.Render(fmt.Sprintf("%s %s", icon, label))
}

// ValidationIcon returns the symbol for a validation state.
func (t Theme) ValidationIcon(state string) string {
	switch state {
	case "passed":
		return t.SymPassed
	case "failed":
		return t.SymFailed
	case "error":
		return t.SymError
	case "running":
		return t.SymRunningValidation
	case "pending":
		fallthrough
	default:
		return t.SymPending
	}
}

// ValidationStyle returns the style for a validation state.
func (t Theme) ValidationStyle(state string) lipgloss.Style {
	switch state {
	case "passed":
		return t.ValidationPassed
	case "failed":
		return t.ValidationFailed
	case "error":
		return t.ValidationError
	case "running":
		return t.ValidationRunning
	case "pending":
		fallthrough
	default:
		return t.ValidationPending
	}
}

// VerificationBadge returns a "✓ verified" style badge for a verification status.
func (t Theme) VerificationBadge(status string) string {
	label := status
	if label == "" {
		label = "pending"
	}
	icon := t.VerificationIcon(label)
	style := t.VerificationStyle(label)
	if !t.ColorEnabled {
		return label
	}
	return style.Render(fmt.Sprintf("%s %s", icon, label))
}

// VerificationIcon returns the symbol for a verification status.
func (t Theme) VerificationIcon(status string) string {
	switch status {
	case "verified":
		return t.SymVerified
	case "rejected":
		return t.SymRejected
	case "unsupported":
		return t.SymUnsupported
	case "error":
		return t.SymError
	case "pending":
		fallthrough
	default:
		return t.SymPending
	}
}

// VerificationStyle returns the style for a verification status.
func (t Theme) VerificationStyle(status string) lipgloss.Style {
	switch status {
	case "verified":
		return t.VerificationVerified
	case "rejected":
		return t.VerificationRejected
	case "unsupported":
		return t.VerificationUnsupported
	case "error":
		return t.VerificationError
	case "pending":
		fallthrough
	default:
		return t.VerificationPending
	}
}

// ShortHash trims a hash to 7 chars for compact display.
func ShortHash(hash string) string {
	if len(hash) <= 7 {
		return hash
	}
	return hash[:7]
}

// JoinHorizontal joins strings with a separator, handling styled strings not specially.
func JoinHorizontal(sep string, parts ...string) string {
	return strings.Join(parts, sep)
}
