package auditor

import (
	"bytes"
	json "encoding/json/v2"
	"strings"
)

type goTestEvent struct {
	Action     string
	Package    string
	Test       string
	ImportPath string
	Output     string
	OutputType string
}

type testName struct {
	pkg   string
	test  string
	build bool
}

type testLog struct {
	lines     []string
	size      int
	truncated bool
	ownError  bool
}

// goEvents reads the full stream even after the transcript reaches its limit.
// Passing and skipped tests release their output budget for later tests.
type goEvents struct {
	line     []byte
	discard  bool
	logs     map[testName]testLog
	failed   map[testName]bool
	failures []testName
	used     int
}

func (g *goEvents) Write(p []byte) (int, error) {
	size := len(p)
	for len(p) > 0 {
		line, rest, complete := bytes.Cut(p, []byte{'\n'})
		if len(g.line)+len(line) > outputLimit {
			g.discard = true
		}
		if !g.discard {
			g.line = append(g.line, line...)
		}
		if complete {
			g.flush()
		}
		p = rest
	}
	return size, nil
}

func (g *goEvents) consume(event goTestEvent) {
	name := nameOf(event)
	switch event.Action {
	case "output", "build-output":
		g.record(name, event)
	case "fail", "build-fail":
		if !g.failed[name] {
			g.failures = append(g.failures, name)
			g.failed[name] = true
		}
	case "pass", "skip":
		if !g.failed[name] {
			g.used -= g.logs[name].size
			delete(g.logs, name)
		}
	}
}

func (g *goEvents) flush() {
	if !g.discard {
		var event goTestEvent
		if err := json.Unmarshal(g.line, &event); err == nil {
			if g.logs == nil {
				g.logs = make(map[testName]testLog)
				g.failed = make(map[testName]bool)
			}
			g.consume(event)
		}
	}
	g.line, g.discard = g.line[:0], false
}

func (g *goEvents) logFull(log testLog, output string) bool {
	const testOutputLimit = 64 << 10
	if log.size+len(output) > testOutputLimit {
		return true
	}
	return g.used+len(output) > outputLimit
}

func (g *goEvents) record(name testName, event goTestEvent) {
	log := g.logs[name]
	if event.OutputType == "error" {
		log.ownError = true
	}
	if g.logFull(log, event.Output) {
		log.truncated = true
	} else {
		log.lines = append(log.lines, event.Output)
		log.size += len(event.Output)
		g.used += len(event.Output)
	}
	g.logs[name] = log
}

func (g *goEvents) result() []Failure {
	if len(g.line) > 0 {
		g.flush()
	}
	parents := failingParents(g.failures)
	failures := make([]Failure, 0, len(g.failures))
	for _, name := range g.failures {
		log := g.logs[name]
		if parents[name] {
			if !log.ownError {
				continue
			}
		}
		failures = append(failures, failureOf(name, log))
	}
	return failures
}

func nameOf(event goTestEvent) testName {
	if event.ImportPath != "" {
		return testName{pkg: event.ImportPath, build: true}
	}
	return testName{pkg: event.Package, test: event.Test}
}

func failureOf(name testName, log testLog) Failure {
	kind := "package"
	if name.build {
		kind = "build"
	} else if name.test != "" {
		kind = "test"
	}
	return Failure{
		Kind: kind, Package: name.pkg, Test: name.test,
		Output: strings.Join(log.lines, ""), OutputTruncated: log.truncated,
	}
}

func failingParents(failures []testName) map[testName]bool {
	parents := make(map[testName]bool)
	for _, failure := range failures {
		if failure.build {
			parents[testName{pkg: failure.pkg}] = true
		}
		if failure.test != "" {
			parents[testName{pkg: failure.pkg}] = true
		}
		for name := failure.test; ; {
			parent, _, ok := strings.CutLast(name, "/")
			if !ok {
				break
			}
			parents[testName{pkg: failure.pkg, test: parent}] = true
			name = parent
		}
	}
	return parents
}
