package refactor

import (
	"go/ast"
	"go/token"
)

//constable:nonmutating
func functionHasBody(fn *ast.FuncDecl) bool {
	if fn == nil {
		return false
	}
	return fn.Body != nil
}

//constable:nonmutating
func functionHasStatements(fn *ast.FuncDecl) bool {
	if fn == nil {
		return false
	}
	if !functionHasBody(fn) {
		return false
	}
	return len(fn.Body.List) > 0
}

//constable:nonmutating
func isExtractableFunction(fn *ast.FuncDecl) bool {
	if fn == nil {
		return false
	}
	if fn.Name == nil {
		return false
	}
	return functionHasStatements(fn)
}

//constable:nonmutating
func callNamesFunction(call *ast.CallExpr, name string) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name == name
	case *ast.SelectorExpr:
		if fun.Sel == nil {
			return false
		}
		return fun.Sel.Name == name
	}
	return false
}

//constable:nonmutating
func functionNamesByPresence(funcs, other map[string]*ast.FuncDecl, present bool) []string {
	var names []string
	for name := range funcs {
		if _, ok := other[name]; ok == present {
			names = append(names, name)
		}
	}
	return names
}

//constable:nonmutating
func replacesBlockWithCall(before, after *ast.FuncDecl, name string, hash uint64, length int) bool {
	if before == nil {
		return false
	}
	if after == nil {
		return false
	}
	if !containsCall(after, name) {
		return false
	}
	if containsCall(before, name) {
		return false
	}
	if !containsBlock(before.Body.List, hash, length) {
		return false
	}
	return !containsBlock(after.Body.List, hash, length)
}

//constable:nonmutating
func inlinedStatementRun(before, after *ast.FuncDecl, name string, hash uint64, length int) []ast.Stmt {
	if before == nil {
		return nil
	}
	if after == nil {
		return nil
	}
	if !functionHasBody(before) || !functionHasBody(after) {
		return nil
	}
	if !containsCall(before, name) {
		return nil
	}
	if containsCall(after, name) {
		return nil
	}
	run := findStmtRun(after.Body.List, hash, length)
	if run == nil {
		return nil
	}
	if containsBlock(before.Body.List, hash, length) {
		return nil
	}
	return run
}

//constable:nonmutating
func statementRunLine(fset *token.FileSet, run []ast.Stmt) int {
	if len(run) == 0 {
		return 0
	}
	if run[0] == nil {
		return 0
	}
	return posLine(fset, run[0].Pos())
}

// containsCall reports whether a function body calls simpleName, either as
// f() or x.f().
//
//constable:nonmutating
func containsCall(fn *ast.FuncDecl, simpleName string) bool {
	if !functionHasBody(fn) {
		return false
	}
	if simpleName == "" {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callNamesFunction(call, simpleName) {
			found = true
			return false
		}
		return true
	})
	return found
}

// containsBlock reports whether body holds a contiguous statement run
// hashing to targetHash.
//
//constable:nonmutating
func containsBlock(body []ast.Stmt, targetHash uint64, targetLen int) bool {
	return findStmtRun(body, targetHash, targetLen) != nil
}

// findStmtRun returns the first contiguous statement run hashing to
// targetHash, or nil if none match.
//
//constable:nonmutating
func findStmtRun(body []ast.Stmt, targetHash uint64, targetLen int) []ast.Stmt {
	if targetLen < 1 {
		return nil
	}
	if len(body) < targetLen {
		return nil
	}
	for i := 0; i+targetLen <= len(body); i++ {
		run := body[i : i+targetLen]
		if hashStmtListOf(run, nil) == targetHash {
			return run
		}
	}
	return nil
}

// detectFunctionRefactorings finds extract-function and inline-function
// between new or deleted functions and common callers in the same file.
//
//constable:nonmutating
func detectFunctionRefactorings(
	path string,
	afterFset *token.FileSet,
	beforeFuncs, afterFuncs map[string]*ast.FuncDecl,
) []Refactoring {
	var out []Refactoring
	out = append(out, detectExtractFunction(path, afterFset, beforeFuncs, afterFuncs)...)
	out = append(out, detectInlineFunction(path, afterFset, beforeFuncs, afterFuncs)...)
	return out
}

//constable:nonmutating
func detectExtractFunction(
	path string,
	afterFset *token.FileSet,
	beforeFuncs, afterFuncs map[string]*ast.FuncDecl,
) []Refactoring {
	newNames := functionNamesByPresence(afterFuncs, beforeFuncs, false)
	common := functionNamesByPresence(beforeFuncs, afterFuncs, true)
	var out []Refactoring
	for _, newQualified := range newNames {
		newFn := afterFuncs[newQualified]
		if newFn == nil {
			continue
		}
		if !isExtractableFunction(newFn) {
			continue
		}
		newBody := newFn.Body.List
		newHash := hashStmtListOf(newBody, nil)
		newSimple := simpleFuncName(newQualified)
		for _, src := range common {
			beforeSrc := beforeFuncs[src]
			afterSrc := afterFuncs[src]
			if !functionHasBody(beforeSrc) || !functionHasBody(afterSrc) {
				continue
			}
			if !replacesBlockWithCall(beforeSrc, afterSrc, newSimple, newHash, len(newBody)) {
				continue
			}
			out = append(out, ExtractFunction{
				File:           path,
				SourceFunction: src,
				NewFunction:    newQualified,
				AfterLine:      posLine(afterFset, newFn.Name.Pos()),
			})
		}
	}
	return out
}

//constable:nonmutating
func detectInlineFunction(
	path string,
	afterFset *token.FileSet,
	beforeFuncs, afterFuncs map[string]*ast.FuncDecl,
) []Refactoring {
	deleted := functionNamesByPresence(beforeFuncs, afterFuncs, false)
	common := functionNamesByPresence(beforeFuncs, afterFuncs, true)
	var out []Refactoring
	for _, delQualified := range deleted {
		delFn := beforeFuncs[delQualified]
		if delFn == nil {
			continue
		}
		if !functionHasStatements(delFn) {
			continue
		}
		delBody := delFn.Body.List
		delHash := hashStmtListOf(delBody, nil)
		delSimple := simpleFuncName(delQualified)
		for _, target := range common {
			beforeT := beforeFuncs[target]
			afterT := afterFuncs[target]
			run := inlinedStatementRun(beforeT, afterT, delSimple, delHash, len(delBody))
			if run == nil {
				continue
			}
			line := statementRunLine(afterFset, run)
			out = append(out, InlineFunction{
				File:            path,
				TargetFunction:  target,
				InlinedFunction: delQualified,
				AfterLine:       line,
			})
		}
	}
	return out
}
