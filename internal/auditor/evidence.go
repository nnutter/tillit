package auditor

import (
	"path/filepath"
	"slices"
)

// Evidence records the command and output from a validation attempt.
type Evidence struct {
	Command         []string  `json:"command"`
	Output          string    `json:"output"`
	OutputTruncated bool      `json:"output_truncated"`
	Failures        []Failure `json:"failures"`
}

// Failure identifies the smallest failing unit reported by the validation command.
type Failure struct {
	Kind            string         `json:"kind"`
	Package         string         `json:"package,omitempty"`
	Test            string         `json:"test,omitempty"`
	Output          string         `json:"output"`
	OutputTruncated bool           `json:"output_truncated"`
	Reproductions   []Reproduction `json:"reproductions,omitempty"`
}

func evidenceCommand(command []string) ([]string, bool) {
	if !isGoTest(command) {
		return command, false
	}
	for _, arg := range command[2:] {
		switch arg {
		case "-json", "-json=true", "--json", "--json=true":
			return command, true
		case "-json=false", "--json=false":
			return command, false
		case "-args", "--args", "--":
			return slices.Insert(slices.Clone(command), 2, "-json"), true
		}
	}
	return slices.Insert(slices.Clone(command), 2, "-json"), true
}

func isGoTest(command []string) bool {
	if len(command) < 2 {
		return false
	}
	if filepath.Base(command[0]) != "go" {
		return false
	}
	return command[1] == "test"
}

func evidenceFailures(output commandOutput) []Failure {
	if len(output.Failures) > 0 {
		return output.Failures
	}
	return []Failure{{Kind: "command", Output: output.Text, OutputTruncated: output.Truncated}}
}
