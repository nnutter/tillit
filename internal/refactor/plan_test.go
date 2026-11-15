package refactor_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/nnutter/spor/internal/refactor"
)

func TestPlanPureRename(t *testing.T) {
	t.Parallel()
	before := []byte(`package p
import "fmt"
func greet() {
	name := "bob"
	fmt.Println(name)
}
`)
	after := []byte(`package p
import "fmt"
func greet() {
	user := "bob"
	fmt.Println(user)
}
`)
	steps := plan(t, before, after)
	require.Len(t, steps, 1)
	require.Equal(t, refactor.KindRenameVariable, steps[0].Refactoring.Kind())
	require.Equal(t, after, steps[0].Source)
}

func TestPlanRenameMixedWithOtherEdits(t *testing.T) {
	t.Parallel()
	before := []byte(`package p
import "fmt"
func f() {
	x := 1
	fmt.Println(x)
}
`)
	after := []byte(`package p
import "fmt"
func f() {
	y := 1
	fmt.Println(y)
	fmt.Println("extra")
}
`)
	steps := plan(t, before, after)
	require.Len(t, steps, 1)
	require.Equal(t, refactor.KindRenameVariable, steps[0].Refactoring.Kind())
	require.Equal(t, []byte(`package p
import "fmt"
func f() {
	y := 1
	fmt.Println(y)
}
`), steps[0].Source)
	require.NotEqual(t, after, steps[0].Source)
	assertPlanProperties(t, before, after)
}

func TestPlanTwoRenames(t *testing.T) {
	t.Parallel()
	before := []byte(`package p
import "fmt"
func f() {
	x := 1
	y := 2
	fmt.Println(x, y)
}
`)
	after := []byte(`package p
import "fmt"
func f() {
	a := 1
	b := 2
	fmt.Println(a, b)
}
`)
	steps := plan(t, before, after)
	require.Len(t, steps, 2)
	require.Equal(t, after, steps[1].Source)
	assertPlanProperties(t, before, after)
}

func TestPlanExtractVariable(t *testing.T) {
	t.Parallel()
	before := []byte(`package p
import "fmt"
func f(a, b int) {
	fmt.Println(a + b)
}
`)
	after := []byte(`package p
import "fmt"
func f(a, b int) {
	sum := a + b
	fmt.Println(sum)
}
`)
	steps := plan(t, before, after)
	require.Len(t, steps, 1)
	require.Equal(t, refactor.KindExtractVariable, steps[0].Refactoring.Kind())
	require.Equal(t, after, steps[0].Source)
	assertPlanProperties(t, before, after)
}

func TestPlanInlineVariable(t *testing.T) {
	t.Parallel()
	before := []byte(`package p
import "fmt"
func f(a, b int) {
	sum := a + b
	fmt.Println(sum)
}
`)
	after := []byte(`package p
import "fmt"
func f(a, b int) {
	fmt.Println(a + b)
}
`)
	steps := plan(t, before, after)
	require.Len(t, steps, 1)
	require.Equal(t, refactor.KindInlineVariable, steps[0].Refactoring.Kind())
	require.Equal(t, after, steps[0].Source)
}

func TestPlanExtractFunction(t *testing.T) {
	t.Parallel()
	before := []byte(`package p
import "fmt"
func foo() {
	a := 1
	b := 2
	fmt.Println(a + b)
}
`)
	after := []byte(`package p
import "fmt"
func foo() {
	bar()
}
func bar() {
	a := 1
	b := 2
	fmt.Println(a + b)
}
`)
	steps := plan(t, before, after)
	require.Len(t, steps, 1)
	require.Equal(t, refactor.KindExtractFunction, steps[0].Refactoring.Kind())
	require.Equal(t, after, steps[0].Source)
}

func TestPlanInlineFunction(t *testing.T) {
	t.Parallel()
	before := []byte(`package p
import "fmt"
func foo() {
	bar()
}
func bar() {
	a := 1
	b := 2
	fmt.Println(a + b)
}
`)
	after := []byte(`package p
import "fmt"
func foo() {
	a := 1
	b := 2
	fmt.Println(a + b)
}
`)
	steps := plan(t, before, after)
	require.Len(t, steps, 1)
	require.Equal(t, refactor.KindInlineFunction, steps[0].Refactoring.Kind())
	require.Equal(t, after, steps[0].Source)
}

func TestPlanHasNoSteps(t *testing.T) {
	t.Parallel()
	src := "package p\nfunc f() { x := 1; _ = x }\n"
	shadowBefore := `package p
func f() int {
	x := 1
	{
		x := 2
		return x
	}
}
`
	shadowAfter := strings.ReplaceAll(shadowBefore, "x", "y")
	cases := []struct {
		name   string
		before string
		after  string
	}{
		{name: "identical", before: src, after: src},
		{name: "type change", before: src, after: "package p\nfunc f() { y := \"hi\"; _ = y }\n"},
		{name: "shadowed name", before: shadowBefore, after: shadowAfter},
		{name: "added file", before: "", after: "package p\nfunc f() {}\n"},
		{name: "deleted file", before: "package p\nfunc f() {}\n", after: ""},
		{
			name:   "comment only",
			before: src,
			after:  "package p\n// note\nfunc f() { x := 1; _ = x }\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			steps, err := refactor.Plan("a.go", []byte(tc.before), []byte(tc.after))
			require.NoError(t, err)
			require.Empty(t, steps)
		})
	}
}

func TestPlanRenamesAcrossSyntax(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "range value",
			src: `package p
func f(xs []int) int {
	n := 0
	for _, NAME := range xs {
		n += NAME
	}
	return n
}
`,
		},
		{
			name: "for init",
			src: `package p
func f() int {
	n := 0
	for NAME := 0; NAME < 3; NAME++ {
		n += NAME
	}
	return n
}
`,
		},
		{
			name: "method",
			src: `package p
type T struct{}
func (t T) f() int {
	NAME := 1
	return NAME
}
`,
		},
		{
			name: "if",
			src: `package p
func f(ok bool) int {
	NAME := 1
	if ok {
		return NAME
	}
	return 0
}
`,
		},
		{
			name: "field stays",
			src: `package p
type box struct{ x int }
func f(item box) int {
	NAME := 1
	return NAME + item.x
}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := strings.ReplaceAll(tc.src, "NAME", "alpha")
			after := strings.ReplaceAll(tc.src, "NAME", "beta")
			steps := plan(t, []byte(before), []byte(after))
			require.Len(t, steps, 1)
			require.Equal(t, refactor.KindRenameVariable, steps[0].Refactoring.Kind())
			require.Contains(t, steps[0].Refactoring.Describe(), "alpha")
			require.Contains(t, steps[0].Refactoring.Describe(), "beta")
			require.Equal(t, []byte(after), steps[0].Source)
			assertPlanProperties(t, []byte(before), []byte(after))
		})
	}
}

func TestPlanPeelsARenameBeforeAnExtract(t *testing.T) {
	t.Parallel()
	before := []byte(`package p
import "fmt"
func f(a, b int) {
	x := 1
	fmt.Println(x)
	fmt.Println(a + b)
}
`)
	after := []byte(`package p
import "fmt"
func f(a, b int) {
	y := 1
	fmt.Println(y)
	sum := a + b
	fmt.Println(sum)
}
`)
	steps := plan(t, before, after)
	require.Len(t, steps, 2)
	require.Equal(t, refactor.KindRenameVariable, steps[0].Refactoring.Kind())
	require.Equal(t, refactor.KindExtractVariable, steps[1].Refactoring.Kind())
	require.NotEqual(t, after, steps[0].Source)
	require.Equal(t, after, steps[1].Source)
	assertPlanProperties(t, before, after)
}

func TestPlanParseError(t *testing.T) {
	t.Parallel()
	_, err := refactor.Plan("a.go", []byte("package p\nfunc"), []byte("package p\nfunc f() {}\n"))
	require.ErrorContains(t, err, "a.go")
	require.ErrorContains(t, err, "expected")
}

func FuzzPlan(f *testing.F) {
	f.Add(
		[]byte("package p\nfunc f() { x := 1; _ = x }\n"),
		[]byte("package p\nfunc f() { y := 1; _ = y }\n"),
	)
	f.Add([]byte("package p\nfunc"), []byte("package p\nfunc f() {}\n"))
	f.Add([]byte("not go"), []byte("not go"))
	f.Add([]byte(""), []byte("package p\nfunc f() {}\n"))
	f.Fuzz(func(t *testing.T, before, after []byte) {
		_, err := refactor.Plan("a.go", before, after)
		if err != nil {
			require.ErrorContains(t, err, "a.go")
			return
		}
		assertPlanProperties(t, before, after)
	})
}

func plan(t *testing.T, before, after []byte) []refactor.Step {
	t.Helper()
	steps, err := refactor.Plan("a.go", before, after)
	require.NoError(t, err)
	return steps
}

func assertPlanProperties(t *testing.T, before, after []byte) {
	t.Helper()
	first := plan(t, before, after)
	second := plan(t, before, after)
	requireRefactorings(t, first)
	requireRefactorings(t, second)
	require.Equal(t, stepKinds(first), stepKinds(second))
	require.Equal(t, stepSources(first), stepSources(second))
	require.Empty(t, plan(t, after, bytes.Clone(after)))

	current := before
	for i, step := range first {
		require.NotEmpty(t, step.Refactoring.Describe())
		require.NotEqual(t, current, step.Source)
		parseGo(t, step.Source)
		if bytes.Equal(step.Source, after) {
			require.Equal(t, len(first)-1, i)
		}
		suffix := plan(t, step.Source, after)
		require.Equal(t, stepKinds(first[i+1:]), stepKinds(suffix))
		require.Equal(t, stepSources(first[i+1:]), stepSources(suffix))
		current = step.Source
	}
}

func requireRefactorings(t *testing.T, steps []refactor.Step) {
	t.Helper()
	for _, step := range steps {
		require.NotNil(t, step.Refactoring)
	}
}

func stepKinds(steps []refactor.Step) []refactor.Kind {
	kinds := make([]refactor.Kind, len(steps))
	for i, step := range steps {
		kinds[i] = step.Refactoring.Kind()
	}
	return kinds
}

func stepSources(steps []refactor.Step) [][]byte {
	sources := make([][]byte, len(steps))
	for i, step := range steps {
		sources[i] = step.Source
	}
	return sources
}

func parseGo(t *testing.T, src []byte) {
	t.Helper()
	_, err := parser.ParseFile(token.NewFileSet(), "a.go", src, parser.SkipObjectResolution)
	require.NoError(t, err)
}
