package auditor

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Reproduction records an isolated rerun without changing the validation outcome.
type Reproduction struct {
	Command             []string `json:"command"`
	State               string   `json:"state"`
	Output              string   `json:"output"`
	OutputTruncated     bool     `json:"output_truncated"`
	TestOutput          string   `json:"test_output,omitempty"`
	TestOutputTruncated bool     `json:"test_output_truncated,omitzero"`
	Error               string   `json:"error,omitempty"`
}

const (
	reproductionBudget = 30 * time.Second
	maxReproductions   = 5
)

var errReproductionBudget = errors.New("isolated reproduction time budget exhausted")

func collectReproductions(ctx context.Context, gitDir, dest, hash string, evidence *Evidence) bool {
	plan, ok := reproductionPlan(evidence.Command)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeoutCause(ctx, reproductionBudget, errReproductionBudget)
	defer cancel()
	runner := reproducer{gitDir: gitDir, dest: dest, hash: hash, plan: plan, remaining: maxReproductions}
	for i := range evidence.Failures {
		runner.collect(ctx, &evidence.Failures[i])
	}
	return runner.remaining != maxReproductions
}

type reproducer struct {
	gitDir    string
	dest      string
	hash      string
	plan      goTestPlan
	remaining int
}

func (r *reproducer) collect(ctx context.Context, failure *Failure) {
	if failure.Kind != "test" {
		return
	}
	if !validPackage(failure.Package) {
		return
	}
	if r.exhausted(ctx) {
		return
	}
	isolated := r.run(ctx, *failure, selector(failure.Test))
	failure.Reproductions = append(failure.Reproductions, isolated)
	if isolated.State == "passed" {
		if !r.exhausted(ctx) {
			failure.Reproductions = append(failure.Reproductions, r.run(ctx, *failure, r.plan.run))
		}
	}
}

func (r *reproducer) exhausted(ctx context.Context) bool {
	if r.remaining <= 0 {
		return true
	}
	return ctx.Err() != nil
}

func (r *reproducer) run(ctx context.Context, failure Failure, pattern string) Reproduction {
	r.remaining--
	args := slices.Clone(r.plan.base)
	if pattern != "" {
		args = append(args, "-run", pattern)
	}
	args = append(args, failure.Package)
	result := Reproduction{Command: args}
	if err := prepareBranchCheckout(ctx, r.gitDir, r.dest, r.hash); err != nil {
		return reproductionError(result, err)
	}
	passed, output, err := runCommand(ctx, r.gitDir, args, true)
	result.Output, result.OutputTruncated = output.Text, output.Truncated
	if err != nil {
		return reproductionError(result, err)
	}
	if passed {
		result.State = "passed"
		return result
	}
	return reproduceResult(failure, output.Failures, result)
}

func reproductionError(result Reproduction, err error) Reproduction {
	result.State, result.Error = "error", err.Error()
	return result
}

func reproduceResult(target Failure, failures []Failure, result Reproduction) Reproduction {
	for _, failure := range failures {
		if failure.Package != target.Package {
			continue
		}
		if failure.Test == target.Test {
			result.State = "reproduced"
			result.TestOutput, result.TestOutputTruncated = failure.Output, failure.OutputTruncated
			return result
		}
	}
	result.State = "other_failure"
	return result
}

func validPackage(path string) bool {
	switch path {
	case "", ".", "..", "all", "std", "cmd", "command-line-arguments":
		return false
	}
	if strings.HasPrefix(path, "-") || strings.HasPrefix(path, "/") {
		return false
	}
	return !strings.ContainsAny(path, " \t\n\r\\@") && !strings.Contains(path, "...")
}

func selector(name string) string {
	var result strings.Builder
	for part := range strings.SplitSeq(name, "/") {
		if result.Len() > 0 {
			result.WriteByte('/')
		}
		result.WriteByte('^')
		result.WriteString(regexp.QuoteMeta(part))
		result.WriteByte('$')
	}
	return result.String()
}

type goTestPlan struct {
	base []string
	run  string
}

// reproductionPlan keeps only commands whose arguments can be classified.
// Unknown flags can consume the next word, so guessing can change the test run.
func reproductionPlan(command []string) (goTestPlan, bool) {
	if !isGoTest(command) {
		return goTestPlan{}, false
	}
	plan := goTestPlan{base: []string{command[0], "test", "-json", "-count=1"}}
	args := command[2:]
	for len(args) > 0 {
		flag, hasValue, consumed, ok := classifyTestArg(args)
		if !ok {
			return goTestPlan{}, false
		}
		plan.addFlag(flag, args[:consumed], hasValue)
		args = args[consumed:]
	}
	return plan, true
}

func (p *goTestPlan) addFlag(flag string, args []string, separateValue bool) {
	switch flag {
	case "run":
		if separateValue {
			p.run = args[1]
		} else {
			_, p.run, _ = strings.Cut(args[0], "=")
		}
	case "", "json", "count":
	default:
		p.base = append(p.base, args...)
	}
}

func classifyTestArg(args []string) (flag string, separateValue bool, consumed int, ok bool) {
	arg := args[0]
	if !strings.HasPrefix(arg, "-") {
		return "", false, 1, !strings.HasSuffix(arg, ".go")
	}
	flag, _, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
	flag = strings.TrimPrefix(flag, "test.")
	switch flag {
	case "json", "v", "race", "cover", "failfast", "short", "modcacherw", "trimpath", "vetall":
		return flag, false, 1, true
	case "count", "run", "parallel", "timeout", "shuffle", "skip", "tags", "mod", "modfile", "vet", "cpu",
		"covermode", "coverpkg", "gcflags", "asmflags", "ldflags", "p":
		if inline {
			return flag, false, 1, true
		}
		return flag, true, 2, len(args) > 1
	default:
		return "", false, 0, false
	}
}
