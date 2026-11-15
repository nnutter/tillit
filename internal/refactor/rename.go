package refactor

import (
	"cmp"
	"go/ast"
	"go/token"
	"slices"
)

// detectRenames finds local variables renamed within the same function.
//
// A rename is a before-name absent after paired with an after-name absent
// before where both definitions share kind, scope depth, use count, and
// initializer/type shape. Matching is intentionally local so renames are
// still found when mixed with other edits.
//
//constable:nonmutating
func detectRenames(
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
		out = append(out, detectRenamesInFunc(path, afterFset, name, beforeFn, afterFn)...)
	}
	slices.SortFunc(out, compareRenames)
	return out
}

//constable:nonmutating
func detectRenamesInFunc(
	path string,
	afterFset *token.FileSet,
	funcName string,
	beforeFn, afterFn *ast.FuncDecl,
) []Refactoring {
	beforeSingles := singletons(collectVarGroups(beforeFn))
	afterSingles := singletons(collectVarGroups(afterFn))
	if len(beforeSingles) == 0 {
		return nil
	}
	if len(afterSingles) == 0 {
		return nil
	}

	removed := absentVariableDefinitions(beforeSingles, afterSingles)
	added := absentVariableDefinitions(afterSingles, beforeSingles)
	used := make(map[*varDef]bool, len(added))
	var out []Refactoring
	for _, r := range removed {
		for _, a := range added {
			if used[a] {
				continue
			}
			if !renameCompatible(r, a) {
				continue
			}
			used[a] = true
			out = append(out, RenameVariable{
				File:       path,
				Function:   funcName,
				BeforeName: r.name,
				AfterName:  a.name,
				AfterLine:  posLine(afterFset, a.pos),
			})
			break
		}
	}
	return out
}

//constable:nonmutating
func compareRenames(a, b Refactoring) int {
	ra, oka := a.(RenameVariable)
	if !oka {
		return 0
	}
	rb, okb := b.(RenameVariable)
	if !okb {
		return 0
	}
	if ra.Function != rb.Function {
		return cmp.Compare(ra.Function, rb.Function)
	}
	if ra.AfterLine != rb.AfterLine {
		return ra.AfterLine - rb.AfterLine
	}
	if ra.BeforeName != rb.BeforeName {
		return cmp.Compare(ra.BeforeName, rb.BeforeName)
	}
	return cmp.Compare(ra.AfterName, rb.AfterName)
}

//constable:nonmutating
func absentVariableDefinitions(defs, other map[string]*varDef) []*varDef {
	var absent []*varDef
	for name, d := range defs {
		if _, ok := other[name]; !ok {
			absent = append(absent, d)
		}
	}
	slices.SortFunc(absent, func(a, b *varDef) int { return a.order - b.order })
	return absent
}

// renameCompatible checks local signatures without requiring the rest of
// the function to be identical, so renames mixed with other edits match.
//
//constable:nonmutating
func renameCompatible(before, after *varDef) bool {
	if before.kind != after.kind {
		return false
	}
	if before.scopeDepth != after.scopeDepth {
		return false
	}
	if before.useCount != after.useCount {
		return false
	}
	if before.initHash != after.initHash {
		return false
	}
	if before.typeHash != after.typeHash {
		return false
	}
	return true
}
