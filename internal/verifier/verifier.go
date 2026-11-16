// Package verifier checks a deliberately narrow class of source-only renames.
package verifier

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"strings"
)

const languageVersion = "go1.27"

// Status describes whether the verifier established its proof condition.
type Status string

const (
	Verified    Status = "verified"
	Rejected    Status = "rejected"
	Unsupported Status = "unsupported"
)

// Evidence describes the scope and assumptions of a successful check.
type Evidence struct {
	AnalyzerVersion   string
	LanguageVersion   string
	File              string
	Function          string
	BeforeName        string
	AfterName         string
	CheckedConditions []string
	Assumptions       []string
}

// Result is not a general semantic equivalence judgment.
type Result struct {
	Status   Status
	Reason   string
	Evidence *Evidence
}

type (
	sourceToken struct {
		kind token.Token
		text string
		pos  token.Pos
	}
	source struct {
		info    *types.Info
		tokens  []sourceToken
		ident   map[token.Pos]*ast.Ident
		locals  map[types.Object]string
		indices map[token.Pos]int
	}
)

type rename struct {
	old, new types.Object
	function string
}

// Verify compares two complete, self-contained Go files under Go 1.27 rules.
// filename is a descriptive label, not a path that is read.
func Verify(filename string, before, after []byte) Result {
	a, err := analyze(filename, before)
	if err != nil {
		return Result{Status: Unsupported, Reason: "before: " + err.Error()}
	}
	b, err := analyze(filename, after)
	if err != nil {
		return Result{Status: Unsupported, Reason: "after: " + err.Error()}
	}
	change, reason := findRename(a, b)
	if reason != "" {
		return Result{Status: Rejected, Reason: reason}
	}
	if reason = checkBindings(a, b, change); reason != "" {
		return Result{Status: Rejected, Reason: reason}
	}
	return Result{Status: Verified, Evidence: &Evidence{
		AnalyzerVersion: "local-rename-v1", LanguageVersion: languageVersion, File: filename, Function: change.function,
		BeforeName: change.old.Name(), AfterName: change.new.Name(),
		CheckedConditions: []string{"supported syntax", "both files type-check without imports", "identical tokens except one local declaration and all its references", "corresponding identifier bindings preserved"},
		Assumptions:       []string{"local variable names have no runtime semantic significance", "debug metadata and source position observation are excluded"},
	}}
}

// findRename requires every changed token to name the same pair of objects.
func findRename(a, b *source) (rename, string) {
	var change rename
	if len(a.tokens) != len(b.tokens) {
		return change, "token count differs"
	}
	for i, x := range a.tokens {
		y := b.tokens[i]
		if x.kind != y.kind {
			return change, "token kinds differ"
		}
		if x.text == y.text {
			continue
		}
		var reason string
		change, reason = changedToken(a, b, x, y, change)
		if reason != "" {
			return change, reason
		}
	}
	return validateRename(a, b, change)
}

func changedToken(a, b *source, x, y sourceToken, change rename) (rename, string) {
	if x.kind != token.IDENT {
		return change, "non-identifier token changed"
	}
	old, new := a.objectAt(x.pos), b.objectAt(y.pos)
	if old == nil {
		return change, "changed identifier is not a local variable"
	}
	if new == nil {
		return change, "changed identifier is not a local variable"
	}
	if change.old == nil {
		change.old, change.new = old, new
	}
	if old != change.old {
		return change, "more than one binding changed"
	}
	if new != change.new {
		return change, "more than one binding changed"
	}
	return change, ""
}

func validateRename(a, b *source, change rename) (rename, string) {
	if change.old == nil {
		return change, "no local variable rename"
	}
	if change.new == nil {
		return change, "no local variable rename"
	}
	function, ok := a.locals[change.old]
	if !ok {
		return change, "change is not an explicit local variable rename"
	}
	if b.locals[change.new] != function {
		return change, "local declarations do not correspond"
	}
	change.function = function
	return change, ""
}

func (s *source) objectAt(pos token.Pos) types.Object {
	id := s.ident[pos]
	if id == nil {
		return nil
	}
	if obj := s.info.Defs[id]; obj != nil {
		return obj
	}
	return s.info.Uses[id]
}

// checkBindings compares declarations by token index, not byte position.
func checkBindings(a, b *source, change rename) string {
	uses := 0
	for i, x := range a.tokens {
		if x.kind != token.IDENT {
			continue
		}
		y := b.tokens[i]
		old, new := a.objectAt(x.pos), b.objectAt(y.pos)
		if isChangedBinding(old, new, change) {
			if old != change.old {
				return "renamed binding references differ"
			}
			if new != change.new {
				return "renamed binding references differ"
			}
			if x.text == y.text {
				return "renamed binding references differ"
			}
			if isUse(a, x.pos, change.old) {
				uses++
			}
			continue
		}
		if !sameBinding(a, b, old, new) {
			return "another identifier binding changed"
		}
	}
	if uses == 0 {
		return "local variable is not referenced"
	}
	return ""
}

func isChangedBinding(old, new types.Object, change rename) bool {
	if old == change.old {
		return true
	}
	return new == change.new
}

func isUse(s *source, pos token.Pos, obj types.Object) bool {
	id := s.ident[pos]
	if id == nil {
		return false
	}
	return s.info.Uses[id] == obj
}

func sameBinding(a, b *source, x, y types.Object) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return false
	}
	ix, okx := a.indices[x.Pos()]
	iy, oky := b.indices[y.Pos()]
	if okx {
		return oky && sameIndex(ix, iy)
	}
	if oky {
		return false
	}
	return x == y // predeclared universe objects
}
func sameIndex(x, y int) bool { return x == y }

func analyze(filename string, data []byte) (*source, error) {
	// The scanner normalizes carriage returns in some literals and comments.
	if strings.ContainsRune(string(data), '\r') {
		return nil, fmt.Errorf("carriage returns are unsupported")
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, data, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("invalid Go source: %w", err)
	}
	if err := supportedFile(file); err != nil {
		return nil, err
	}
	info := &types.Info{Defs: make(map[*ast.Ident]types.Object), Uses: make(map[*ast.Ident]types.Object)}
	conf := types.Config{GoVersion: languageVersion}
	if _, err := conf.Check("verifier/input", fset, []*ast.File{file}, info); err != nil {
		return nil, fmt.Errorf("type-check failed: %w", err)
	}
	s := &source{info: info, ident: make(map[token.Pos]*ast.Ident), locals: make(map[types.Object]string), indices: make(map[token.Pos]int)}
	collectIdentifiers(s, file)
	collectLocals(s, file)
	scanTokens(s, fset.File(file.Pos()), data)
	return s, nil
}

func supportedFile(file *ast.File) error {
	if hasDirectives(file) {
		return fmt.Errorf("directives are unsupported")
	}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			return fmt.Errorf("only plain function declarations are supported")
		}
		if err := supportedFunction(fn); err != nil {
			return err
		}
	}
	supported := true
	ast.Inspect(file, func(n ast.Node) bool {
		if !supportedNode(n) {
			supported = false
		}
		return supported
	})
	if !supported {
		return fmt.Errorf("syntax outside the supported subset")
	}
	return nil
}

func hasDirectives(file *ast.File) bool {
	prefixes := []string{"//go:", "//line ", "/*line ", "// +build", "//export ", "// #cgo"}
	for _, group := range file.Comments {
		for _, c := range group.List {
			for _, prefix := range prefixes {
				if strings.HasPrefix(c.Text, prefix) {
					return true
				}
			}
		}
	}
	return false
}

func supportedFunction(fn *ast.FuncDecl) error {
	if fn.Recv != nil {
		return fmt.Errorf("methods are unsupported")
	}
	if fn.Body == nil {
		return fmt.Errorf("functions without bodies are unsupported")
	}
	if fn.Type.TypeParams != nil {
		return fmt.Errorf("generic functions are unsupported")
	}
	if fn.Type.Results != nil {
		for _, field := range fn.Type.Results.List {
			if len(field.Names) != 0 {
				return fmt.Errorf("named results are unsupported")
			}
		}
	}
	return nil
}

func supportedNode(n ast.Node) bool {
	switch v := n.(type) {
	case nil, *ast.File, *ast.Comment, *ast.CommentGroup, *ast.Ident, *ast.FuncDecl, *ast.FuncType, *ast.FieldList, *ast.Field,
		*ast.BlockStmt, *ast.DeclStmt, *ast.GenDecl, *ast.ValueSpec, *ast.ReturnStmt,
		*ast.AssignStmt, *ast.IfStmt, *ast.ExprStmt, *ast.IncDecStmt, *ast.EmptyStmt,
		*ast.BasicLit, *ast.BinaryExpr, *ast.ParenExpr:
		return true
	case *ast.UnaryExpr:
		if v.Op == token.AND {
			return false
		}
		return v.Op != token.ARROW
	default:
		return false
	}
}

func collectIdentifiers(s *source, file *ast.File) {
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			s.ident[id.Pos()] = id
		}
		return true
	})
}

func collectLocals(s *source, file *ast.File) {
	for _, decl := range file.Decls {
		fn := decl.(*ast.FuncDecl)
		ast.Inspect(fn.Body, func(n ast.Node) bool { collectDeclaration(s, n, fn.Name.Name); return true })
	}
}

func collectDeclaration(s *source, n ast.Node, function string) {
	d, ok := n.(*ast.DeclStmt)
	if !ok {
		return
	}
	g, ok := d.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	if g.Tok != token.VAR {
		return
	}
	for _, spec := range g.Specs {
		for _, id := range spec.(*ast.ValueSpec).Names {
			if obj := s.info.Defs[id]; obj != nil {
				s.locals[obj] = function
			}
		}
	}
}

func scanTokens(s *source, file *token.File, data []byte) {
	var scan scanner.Scanner
	scan.Init(file, data, nil, scanner.ScanComments)
	for {
		pos, kind, lit := scan.Scan()
		if kind == token.EOF {
			break
		}
		if lit == "" {
			lit = kind.String()
		}
		s.indices[pos] = len(s.tokens)
		s.tokens = append(s.tokens, sourceToken{kind, lit, pos})
	}
}
