package refactor

import (
	"go/ast"
	"go/token"
)

// indexExprCounts counts non-trivial sub-expression shapes in one function.
//
//constable:nonmutating
func indexExprCounts(fn *ast.FuncDecl) map[uint64]int {
	counts := make(map[uint64]int)
	if fn == nil {
		return counts
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if e == nil {
			return true
		}
		if isTrivialExpr(e) {
			return true
		}
		counts[hashExprOf(e, nil)]++
		return true
	})
	return counts
}

// detectVariableRefactorings finds extract-variable and inline-variable in
// functions present on both sides.
//
//constable:nonmutating
func detectVariableRefactorings(
	path string,
	afterFset *token.FileSet,
	beforeFuncs, afterFuncs map[string]*ast.FuncDecl,
) []Refactoring {
	var out []Refactoring
	for name, beforeFn := range beforeFuncs {
		afterFn, ok := afterFuncs[name]
		if !ok {
			continue
		}
		out = append(out, detectVariableInFunc(path, afterFset, name, beforeFn, afterFn)...)
	}
	return out
}

//constable:nonmutating
func detectVariableInFunc(
	path string,
	afterFset *token.FileSet,
	funcName string,
	beforeFn, afterFn *ast.FuncDecl,
) []Refactoring {
	beforeSingles := singletons(collectVarGroups(beforeFn))
	afterSingles := singletons(collectVarGroups(afterFn))
	beforeCounts := indexExprCounts(beforeFn)
	afterCounts := indexExprCounts(afterFn)

	var out []Refactoring
	for name, a := range afterSingles {
		if _, ok := beforeSingles[name]; ok {
			continue
		}
		if !isExtractedVariable(a, beforeCounts, afterCounts) {
			continue
		}
		out = append(out, ExtractVariable{
			File:      path,
			Function:  funcName,
			VarName:   name,
			AfterLine: posLine(afterFset, a.pos),
		})
	}
	for name, b := range beforeSingles {
		if _, ok := afterSingles[name]; ok {
			continue
		}
		if !hasReusableInitializer(b) {
			continue
		}
		if b.useCount < 1 {
			continue
		}
		if afterCounts[b.initHash] < 1 {
			continue
		}
		out = append(out, InlineVariable{
			File:      path,
			Function:  funcName,
			VarName:   name,
			AfterLine: posLine(afterFset, firstExprPos(afterFn, b.initHash)),
		})
	}
	return out
}

//constable:nonmutating
func isNontrivialExpression(e ast.Expr) bool {
	if e == nil {
		return false
	}
	return !isTrivialExpr(e)
}

//constable:nonmutating
func hasReusableInitializer(d *varDef) bool {
	if !d.scalar {
		return false
	}
	return isNontrivialExpression(d.initExpr)
}

//constable:nonmutating
func isExtractedVariable(d *varDef, beforeCounts, afterCounts map[uint64]int) bool {
	if !hasReusableInitializer(d) {
		return false
	}
	if d.useCount < 1 {
		return false
	}
	beforeCount := beforeCounts[d.initHash]
	if beforeCount < 1 {
		return false
	}
	afterExcludingDef := max(afterCounts[d.initHash]-1, 0)
	replaced := beforeCount - afterExcludingDef
	if replaced < 1 {
		return false
	}
	return d.useCount >= replaced
}

// firstExprPos returns the position of the first non-trivial expression
// in fn whose hash matches target.
//
//constable:nonmutating
func firstExprPos(fn *ast.FuncDecl, target uint64) token.Pos {
	var found token.Pos
	if fn == nil {
		return token.NoPos
	}
	if target == 0 {
		return token.NoPos
	}
	ast.Inspect(fn, func(n ast.Node) bool {
		if found.IsValid() {
			return false
		}
		e, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if !isNontrivialExpression(e) {
			return true
		}
		if hashExprOf(e, nil) == target {
			found = e.Pos()
			return false
		}
		return true
	})
	return found
}
