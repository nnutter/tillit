package refactor

import (
	"bytes"
	"cmp"
	"go/ast"
	"go/token"
	"slices"
)

const maxPlanSteps = 32

// Step is one refactoring replayed onto the base source.
// Source is the full file after that refactoring and none of the other edits.
type Step struct {
	Refactoring Refactoring
	Source      []byte
}

// Plan replays basic refactorings from before toward after.
//
// Renames are peeled even when the file also has other edits.
// Extract and inline refactorings are peeled only when replaying them
// reproduces after exactly, so a commit is not a reformatted guess.
// Steps stop at the first refactoring that cannot be replayed.
//
//constable:nonmutating
//phase:nonmutating
func Plan(path string, before, after []byte) ([]Step, error) {
	if bytes.Equal(before, after) {
		return nil, nil
	}
	current := bytes.Clone(before)
	var steps []Step
	for len(steps) < maxPlanSteps {
		if bytes.Equal(current, after) {
			return steps, nil
		}
		found, err := Detect(path, current, after)
		if err != nil {
			return nil, err
		}
		step, ok := nextStep(current, after, found)
		if !ok {
			return steps, nil
		}
		steps = append(steps, step)
		current = step.Source
	}
	return steps, nil
}

//constable:nonmutating
//phase:nonmutating
func nextStep(current, after []byte, found []Refactoring) (Step, bool) {
	if step, ok := peelMatching(current, after, found, isRename); ok {
		return step, true
	}
	return peelMatching(current, after, found, isStructural)
}

//constable:nonmutating
func isRename(r Refactoring) bool {
	return r.Kind() == KindRenameVariable
}

//constable:nonmutating
func isStructural(r Refactoring) bool {
	return r.Kind() != KindRenameVariable
}

//constable:nonmutating
//phase:nonmutating
func peelMatching(current, after []byte, found []Refactoring, want func(Refactoring) bool) (Step, bool) {
	for _, r := range found {
		if !want(r) {
			continue
		}
		mid, ok := peel(current, after, r)
		if !ok {
			continue
		}
		return Step{Refactoring: r, Source: mid}, true
	}
	var none Step
	return none, false
}

//constable:nonmutating
//phase:nonmutating
func peel(current, after []byte, r Refactoring) ([]byte, bool) {
	switch item := r.(type) {
	case RenameVariable:
		return peelRename(current, after, item)
	case ExtractVariable:
		return peelExact(current, after, item)
	case InlineVariable:
		return peelExact(current, after, item)
	case ExtractFunction:
		return peelExact(current, after, item)
	case InlineFunction:
		return peelExact(current, after, item)
	}
	return nil, false
}

//constable:nonmutating
//phase:nonmutating
func peelExact(current, after []byte, r Refactoring) ([]byte, bool) {
	mid, ok := replay(current, after, r)
	if !ok {
		return nil, false
	}
	if !bytes.Equal(mid, after) {
		return nil, false
	}
	return mid, true
}

//constable:nonmutating
//phase:nonmutating
func replay(current, after []byte, r Refactoring) ([]byte, bool) {
	switch item := r.(type) {
	case RenameVariable:
		return nil, false
	case ExtractVariable:
		return replayExtractVariable(current, after, item)
	case InlineVariable:
		return replayInlineVariable(current, item)
	case ExtractFunction:
		return replayExtractFunction(current, after, item)
	case InlineFunction:
		return replayInlineFunction(current, item)
	}
	return nil, false
}

//constable:nonmutating
//phase:nonmutating
func peelRename(current, after []byte, r RenameVariable) ([]byte, bool) {
	mid, ok := replayRename(current, r)
	if !ok {
		return nil, false
	}
	if bytes.Equal(mid, current) {
		return nil, false
	}
	if !renameSeen(current, mid, r) {
		return nil, false
	}
	if renameSeen(mid, after, r) {
		return nil, false
	}
	return mid, true
}

//constable:nonmutating
func renameSeen(before, after []byte, r RenameVariable) bool {
	found, err := Detect(r.File, before, after)
	if err != nil {
		return false
	}
	for _, item := range found {
		got, ok := item.(RenameVariable)
		if !ok {
			continue
		}
		if got.Function != r.Function {
			continue
		}
		if got.BeforeName != r.BeforeName {
			continue
		}
		if got.AfterName != r.AfterName {
			continue
		}
		return true
	}
	return false
}

//constable:nonmutating
//phase:nonmutating
func replayRename(src []byte, r RenameVariable) ([]byte, bool) {
	parsed, ok := parseFuncs(r.File, src)
	if !ok {
		return nil, false
	}
	fn := parsed.funcs[r.Function]
	def := singletons(collectVarGroups(fn))[r.BeforeName]
	if def == nil {
		return nil, false
	}
	if !def.pos.IsValid() {
		return nil, false
	}
	positions := append([]token.Pos{def.pos}, def.uses...)
	var edits []edit
	for _, pos := range positions {
		span, valid := identSpan(parsed.fset, src, pos, r.BeforeName)
		if !valid {
			return nil, false
		}
		edits = append(edits, edit{start: span.start, end: span.end, text: r.AfterName})
	}
	return applyEdits(src, edits)
}

type parsedFile struct {
	file  *ast.File
	fset  *token.FileSet
	funcs map[string]*ast.FuncDecl
}

func parseFuncs(path string, src []byte) (parsedFile, bool) {
	file, fset, err := parseGoFile(path, src)
	if err != nil {
		return parsedFile{}, false
	}
	return parsedFile{file: file, fset: fset, funcs: collectFuncs(file)}, true
}

type span struct {
	start int
	end   int
}

//constable:nonmutating
func identSpan(fset *token.FileSet, src []byte, pos token.Pos, name string) (span, bool) {
	if fset == nil {
		return span{}, false
	}
	if !pos.IsValid() {
		return span{}, false
	}
	if name == "" {
		return span{}, false
	}
	start := fset.Position(pos).Offset
	end := start + len(name)
	if start < 0 {
		return span{}, false
	}
	if end > len(src) {
		return span{}, false
	}
	if string(src[start:end]) != name {
		return span{}, false
	}
	return span{start: start, end: end}, true
}

type edit struct {
	start int
	end   int
	text  string
}

//constable:nonmutating
//phase:nonmutating
func applyEdits(src []byte, edits []edit) ([]byte, bool) {
	if overlaps(src, edits) {
		return nil, false
	}
	ordered := slices.Clone(edits)
	slices.SortFunc(ordered, func(a, b edit) int { return cmp.Compare(b.start, a.start) })
	out := bytes.Clone(src)
	for _, e := range ordered {
		out = slices.Concat(out[:e.start], []byte(e.text), out[e.end:])
	}
	return out, true
}

//constable:nonmutating
func overlaps(src []byte, edits []edit) bool {
	ordered := slices.Clone(edits)
	slices.SortFunc(ordered, func(a, b edit) int { return cmp.Compare(a.start, b.start) })
	prev := 0
	for i, e := range ordered {
		if e.start < 0 {
			return true
		}
		if e.end > len(src) {
			return true
		}
		if e.start > e.end {
			return true
		}
		if i > 0 {
			if e.start < prev {
				return true
			}
		}
		prev = e.end
	}
	return false
}

//constable:nonmutating
//phase:nonmutating
func replayExtractVariable(current, after []byte, r ExtractVariable) ([]byte, bool) {
	base, ok := parseFuncs(r.File, current)
	if !ok {
		return nil, false
	}
	next, ok := parseFuncs(r.File, after)
	if !ok {
		return nil, false
	}
	def := singletons(collectVarGroups(next.funcs[r.Function]))[r.VarName]
	if def == nil {
		return nil, false
	}
	if def.initExpr == nil {
		return nil, false
	}
	init, ok := nodeBytes(next.fset, after, def.initExpr)
	if blankInit(ok, init) {
		return nil, false
	}
	decl, ok := stmtBytes(next.fset, after, smallestStmt(next.funcs[r.Function], def.pos))
	if !ok {
		return nil, false
	}
	spans := exprSpans(base.funcs[r.Function], base.fset, current, init)
	if len(spans) == 0 {
		return nil, false
	}
	var edits []edit
	for _, sp := range spans {
		edits = append(edits, edit{start: sp.start, end: sp.end, text: r.VarName})
	}
	insertAt, ok := lineStart(base.fset, current, spans[0].start)
	if !ok {
		return nil, false
	}
	edits = append(edits, edit{start: insertAt, end: insertAt, text: decl})
	return applyEdits(current, edits)
}

//constable:nonmutating
//phase:nonmutating
func replayInlineVariable(current []byte, r InlineVariable) ([]byte, bool) {
	base, ok := parseFuncs(r.File, current)
	if !ok {
		return nil, false
	}
	def := singletons(collectVarGroups(base.funcs[r.Function]))[r.VarName]
	if def == nil {
		return nil, false
	}
	if def.initExpr == nil {
		return nil, false
	}
	if len(def.uses) == 0 {
		return nil, false
	}
	init, ok := nodeBytes(base.fset, current, def.initExpr)
	if !ok {
		return nil, false
	}
	declStmt := smallestStmt(base.funcs[r.Function], def.pos)
	decl, ok := lineSpanNode(base.fset, current, declStmt)
	if !ok {
		return nil, false
	}
	var edits []edit
	edits = append(edits, edit{start: decl.start, end: decl.end, text: ""})
	for _, pos := range def.uses {
		sp, valid := identSpan(base.fset, current, pos, r.VarName)
		if !valid {
			return nil, false
		}
		if insideSpan(sp, decl) {
			return nil, false
		}
		edits = append(edits, edit{start: sp.start, end: sp.end, text: init})
	}
	return applyEdits(current, edits)
}

//constable:nonmutating
//phase:nonmutating
func replayExtractFunction(current, after []byte, r ExtractFunction) ([]byte, bool) {
	base, ok := parseFuncs(r.File, current)
	if !ok {
		return nil, false
	}
	next, ok := parseFuncs(r.File, after)
	if !ok {
		return nil, false
	}
	newFn := next.funcs[r.NewFunction]
	srcFn := base.funcs[r.SourceFunction]
	afterSrc := next.funcs[r.SourceFunction]
	if newFn == nil {
		return nil, false
	}
	if srcFn == nil {
		return nil, false
	}
	if afterSrc == nil {
		return nil, false
	}
	if newFn.Body == nil {
		return nil, false
	}
	if srcFn.Body == nil {
		return nil, false
	}
	run := findStmtRun(srcFn.Body.List, hashStmtListOf(newFn.Body.List, nil), len(newFn.Body.List))
	return finishExtract(base, next, current, after, newFn, srcFn, afterSrc, run, r)
}

//constable:nonmutating
//phase:nonmutating
func finishExtract(base, next parsedFile, current, after []byte, newFn, srcFn, afterSrc *ast.FuncDecl, run []ast.Stmt, r ExtractFunction) ([]byte, bool) {
	if len(run) == 0 {
		return nil, false
	}
	callStmt := soleCallStmt(afterSrc, simpleFuncName(r.NewFunction))
	call, ok := stmtBytes(next.fset, after, callStmt)
	if !ok {
		return nil, false
	}
	block, ok := lineSpan(base.fset, current, run[0], run[len(run)-1])
	if !ok {
		return nil, false
	}
	fnText, ok := stmtBytes(next.fset, after, newFn)
	if !ok {
		return nil, false
	}
	insertAt, ok := functionInsert(base.fset, current, srcFn, newFn.Pos() < afterSrc.Pos())
	if !ok {
		return nil, false
	}
	edits := []edit{
		{start: block.start, end: block.end, text: call},
		{start: insertAt, end: insertAt, text: fnText},
	}
	return applyEdits(current, edits)
}

//constable:nonmutating
//phase:nonmutating
func replayInlineFunction(current []byte, r InlineFunction) ([]byte, bool) {
	base, ok := parseFuncs(r.File, current)
	if !ok {
		return nil, false
	}
	target := base.funcs[r.TargetFunction]
	deleted := base.funcs[r.InlinedFunction]
	if target == nil {
		return nil, false
	}
	if deleted == nil {
		return nil, false
	}
	if deleted.Body == nil {
		return nil, false
	}
	if len(deleted.Body.List) == 0 {
		return nil, false
	}
	callStmt := soleCallStmt(target, simpleFuncName(r.InlinedFunction))
	call, ok := lineSpanNode(base.fset, current, callStmt)
	if !ok {
		return nil, false
	}
	body, ok := lineSpan(base.fset, current, deleted.Body.List[0], deleted.Body.List[len(deleted.Body.List)-1])
	if !ok {
		return nil, false
	}
	fn, ok := lineSpanNode(base.fset, current, deleted)
	if !ok {
		return nil, false
	}
	bodyText := string(current[body.start:body.end])
	edits := []edit{
		{start: call.start, end: call.end, text: bodyText},
		{start: fn.start, end: fn.end, text: ""},
	}
	return applyEdits(current, edits)
}

//constable:nonmutating
func soleCallStmt(fn *ast.FuncDecl, name string) ast.Stmt {
	if fn == nil {
		return nil
	}
	if fn.Body == nil {
		return nil
	}
	if name == "" {
		return nil
	}
	var found ast.Stmt
	for _, st := range fn.Body.List {
		if !stmtCalls(st, name) {
			continue
		}
		if found != nil {
			return nil
		}
		found = st
	}
	return found
}

//constable:nonmutating
func stmtCalls(st ast.Stmt, name string) bool {
	found := false
	ast.Inspect(st, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callNamesFunction(call, name) {
			found = true
			return false
		}
		return true
	})
	return found
}

//constable:nonmutating
func functionInsert(fset *token.FileSet, src []byte, fn *ast.FuncDecl, before bool) (int, bool) {
	sp, ok := lineSpanNode(fset, src, fn)
	if !ok {
		return 0, false
	}
	if before {
		return sp.start, true
	}
	return sp.end, true
}

//constable:nonmutating
func exprSpans(fn *ast.FuncDecl, fset *token.FileSet, src []byte, text string) []span {
	if fn == nil {
		return nil
	}
	if text == "" {
		return nil
	}
	var found []span
	ast.Inspect(fn, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if e == nil {
			return true
		}
		sp, valid := nodeSpan(fset, src, e)
		if !valid {
			return true
		}
		if string(src[sp.start:sp.end]) != text {
			return true
		}
		found = append(found, sp)
		return true
	})
	return outermost(found)
}

//constable:nonmutating
func outermost(spans []span) []span {
	var out []span
	for _, sp := range spans {
		if containsSpan(spans, sp) {
			continue
		}
		out = append(out, sp)
	}
	slices.SortFunc(out, func(a, b span) int { return cmp.Compare(a.start, b.start) })
	return out
}

//constable:nonmutating
func containsSpan(spans []span, sp span) bool {
	for _, other := range spans {
		if other == sp {
			continue
		}
		if other.start > sp.start {
			continue
		}
		if sp.end > other.end {
			continue
		}
		return true
	}
	return false
}

//constable:nonmutating
func smallestStmt(fn *ast.FuncDecl, pos token.Pos) ast.Stmt {
	if fn == nil {
		return nil
	}
	if !pos.IsValid() {
		return nil
	}
	var best ast.Stmt
	ast.Inspect(fn, func(n ast.Node) bool {
		st, ok := n.(ast.Stmt)
		if !ok {
			return true
		}
		if !nodeContains(st, pos) {
			return true
		}
		if best == nil {
			best = st
			return true
		}
		if tighterStmt(st, best) {
			best = st
		}
		return true
	})
	return best
}

//constable:nonmutating
func nodeContains(n ast.Node, pos token.Pos) bool {
	if n == nil {
		return false
	}
	if !n.Pos().IsValid() {
		return false
	}
	if !n.End().IsValid() {
		return false
	}
	if n.Pos() > pos {
		return false
	}
	return pos < n.End()
}

//constable:nonmutating
func tighterStmt(st, best ast.Stmt) bool {
	if st.Pos() < best.Pos() {
		return false
	}
	return st.End() <= best.End()
}

//constable:nonmutating
func nodeBytes(fset *token.FileSet, src []byte, n ast.Node) (string, bool) {
	sp, ok := nodeSpan(fset, src, n)
	if !ok {
		return "", false
	}
	return string(src[sp.start:sp.end]), true
}

//constable:nonmutating
func stmtBytes(fset *token.FileSet, src []byte, n ast.Node) (string, bool) {
	sp, ok := lineSpanNode(fset, src, n)
	if !ok {
		return "", false
	}
	return string(src[sp.start:sp.end]), true
}

//constable:nonmutating
func nodeSpan(fset *token.FileSet, src []byte, n ast.Node) (span, bool) {
	if fset == nil {
		return span{}, false
	}
	if n == nil {
		return span{}, false
	}
	if !n.Pos().IsValid() {
		return span{}, false
	}
	if !n.End().IsValid() {
		return span{}, false
	}
	start := fset.Position(n.Pos()).Offset
	end := fset.Position(n.End()).Offset
	if start < 0 {
		return span{}, false
	}
	if end > len(src) {
		return span{}, false
	}
	if start > end {
		return span{}, false
	}
	return span{start: start, end: end}, true
}

//constable:nonmutating
func lineSpanNode(fset *token.FileSet, src []byte, n ast.Node) (span, bool) {
	if n == nil {
		return span{}, false
	}
	return offsetLineSpan(fset, src, n.Pos(), n.End())
}

//constable:nonmutating
func lineSpan(fset *token.FileSet, src []byte, first, last ast.Node) (span, bool) {
	if first == nil {
		return span{}, false
	}
	if last == nil {
		return span{}, false
	}
	return offsetLineSpan(fset, src, first.Pos(), last.End())
}

//constable:nonmutating
func offsetLineSpan(fset *token.FileSet, src []byte, start, end token.Pos) (span, bool) {
	if fset == nil {
		return span{}, false
	}
	if !start.IsValid() {
		return span{}, false
	}
	if !end.IsValid() {
		return span{}, false
	}
	file := fset.File(start)
	if file == nil {
		return span{}, false
	}
	if fset.File(end) != file {
		return span{}, false
	}
	from := file.Offset(file.LineStart(file.Line(start)))
	endLine := file.Line(end)
	var to int
	if endLine >= file.LineCount() {
		to = file.Size()
	} else {
		to = file.Offset(file.LineStart(endLine + 1))
	}
	if from < 0 {
		return span{}, false
	}
	if to > len(src) {
		return span{}, false
	}
	if from > to {
		return span{}, false
	}
	return span{start: from, end: to}, true
}

//constable:nonmutating
func lineStart(fset *token.FileSet, src []byte, offset int) (int, bool) {
	if fset == nil {
		return 0, false
	}
	if offset < 0 {
		return 0, false
	}
	if offset > len(src) {
		return 0, false
	}
	var file *token.File
	fset.Iterate(func(f *token.File) bool {
		file = f
		return false
	})
	if file == nil {
		return 0, false
	}
	if offset > file.Size() {
		return 0, false
	}
	return file.Offset(file.LineStart(file.Line(file.Pos(offset)))), true
}

//constable:nonmutating
func blankInit(ok bool, init string) bool {
	if !ok {
		return true
	}
	return init == ""
}

//constable:nonmutating
func insideSpan(inner, outer span) bool {
	if inner.start < outer.start {
		return false
	}
	return inner.end <= outer.end
}
