package config

import "fmt"

// Thinking is the per-attempt effort level for a fixup run.
//
// It is sealed: only the levels declared in this package implement it.
// Switch over concrete types with go-check-sumtype exhaustiveness.
//
//sumtype:decl
type Thinking interface {
	String() string
	sealedThinking()
}

type thinkingOff struct{}

func (thinkingOff) String() string  { return "off" }
func (thinkingOff) sealedThinking() {}

type thinkingMinimal struct{}

func (thinkingMinimal) String() string  { return "minimal" }
func (thinkingMinimal) sealedThinking() {}

type thinkingLow struct{}

func (thinkingLow) String() string  { return "low" }
func (thinkingLow) sealedThinking() {}

type thinkingMedium struct{}

func (thinkingMedium) String() string  { return "medium" }
func (thinkingMedium) sealedThinking() {}

type thinkingHigh struct{}

func (thinkingHigh) String() string  { return "high" }
func (thinkingHigh) sealedThinking() {}

type thinkingXHigh struct{}

func (thinkingXHigh) String() string  { return "xhigh" }
func (thinkingXHigh) sealedThinking() {}

type thinkingMax struct{}

func (thinkingMax) String() string  { return "max" }
func (thinkingMax) sealedThinking() {}

// Thinking levels in pi order.
var (
	ThinkingOff     Thinking = thinkingOff{}
	ThinkingMinimal Thinking = thinkingMinimal{}
	ThinkingLow     Thinking = thinkingLow{}
	ThinkingMedium  Thinking = thinkingMedium{}
	ThinkingHigh    Thinking = thinkingHigh{}
	ThinkingXHigh   Thinking = thinkingXHigh{}
	ThinkingMax     Thinking = thinkingMax{}
)

// ParseThinking accepts a pi thinking level.
func ParseThinking(value string) (Thinking, error) {
	switch value {
	case "off":
		return ThinkingOff, nil
	case "minimal":
		return ThinkingMinimal, nil
	case "low":
		return ThinkingLow, nil
	case "medium":
		return ThinkingMedium, nil
	case "high":
		return ThinkingHigh, nil
	case "xhigh":
		return ThinkingXHigh, nil
	case "max":
		return ThinkingMax, nil
	default:
		return nil, fmt.Errorf("auditor fixup model effort %q is not off, minimal, low, medium, high, xhigh, or max", value)
	}
}
