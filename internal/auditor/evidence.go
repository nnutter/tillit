package auditor

import (
	json "encoding/json/v2"
	"path/filepath"
	"slices"
	"strings"
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
	Kind          string         `json:"kind"`
	Package       string         `json:"package,omitempty"`
	Test          string         `json:"test,omitempty"`
	Output        string         `json:"output"`
	Reproductions []Reproduction `json:"reproductions,omitempty"`
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

type goTestEvent struct {
	Action     string
	Package    string
	Test       string
	ImportPath string
	Output     string
}

type testName struct {
	pkg  string
	test string
}

func evidenceFailures(goJSON bool, output string) []Failure {
	if goJSON {
		if failures := goFailures(output); len(failures) > 0 {
			return failures
		}
	}
	return []Failure{{Kind: "command", Output: output}}
}

func goFailures(output string) []Failure {
	failures := make([]Failure, 0)
	logs := make(map[testName][]string)
	failed := make(map[testName]bool)
	for line := range strings.SplitSeq(output, "\n") {
		var event goTestEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		name := nameOf(event)
		switch event.Action {
		case "output", "build-output":
			logs[name] = append(logs[name], event.Output)
		case "fail", "build-fail":
			if !failed[name] {
				failures = append(failures, failureOf(event, name))
				failed[name] = true
			}
		}
	}
	parents := failingParents(failures)
	failures = slices.DeleteFunc(failures, func(f Failure) bool {
		if f.Kind == "build" {
			return false
		}
		return parents[testName{f.Package, f.Test}]
	})
	for index, failure := range failures {
		failures[index].Output = strings.Join(logs[testName{failure.Package, failure.Test}], "")
	}
	return failures
}

func nameOf(event goTestEvent) testName {
	if event.ImportPath != "" {
		return testName{pkg: event.ImportPath}
	}
	return testName{pkg: event.Package, test: event.Test}
}

func failureOf(event goTestEvent, name testName) Failure {
	kind := "package"
	if event.Action == "build-fail" {
		kind = "build"
	} else if name.test != "" {
		kind = "test"
	}
	return Failure{Kind: kind, Package: name.pkg, Test: name.test}
}

func failingParents(failures []Failure) map[testName]bool {
	parents := make(map[testName]bool)
	for _, failure := range failures {
		if failure.Kind == "package" {
			continue
		}
		parents[testName{pkg: failure.Package}] = true
		for name := failure.Test; ; {
			parent, _, ok := strings.CutLast(name, "/")
			if !ok {
				break
			}
			parents[testName{failure.Package, parent}] = true
			name = parent
		}
	}
	return parents
}
