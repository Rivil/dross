package codex

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// GoIndexer extracts top-level symbols from Go source files via the
// stdlib parser. No external dependencies, no CGO.
//
// Captured kinds:
//   - "function" — top-level func declarations
//   - "method"   — methods (receiver-bearing functions)
//   - "type"     — top-level type declarations
//   - "var"      — top-level var declarations
//   - "const"    — top-level const declarations
//
// Test files (_test.go) are indexed too, but with the symbol kind
// suffix " (test)" so callers can filter them when surfacing
// production-side analysis.
type GoIndexer struct{}

func (g *GoIndexer) Name() string { return "go" }

func (g *GoIndexer) Supports(file string) bool {
	return strings.EqualFold(filepath.Ext(file), ".go")
}

func (g *GoIndexer) Symbols(file string) ([]Symbol, error) {
	fset := token.NewFileSet()
	// parser.SkipObjectResolution speeds up parsing; we only inspect
	// top-level decls so resolution isn't needed.
	f, err := parser.ParseFile(fset, file, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}

	isTest := strings.HasSuffix(file, "_test.go")
	tagKind := func(k string) string {
		if isTest {
			return k + " (test)"
		}
		return k
	}

	var out []Symbol
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			kind := "function"
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = "method"
				if recv := receiverTypeName(d.Recv.List[0].Type); recv != "" {
					name = recv + "." + name
				}
			}
			out = append(out, Symbol{
				Name: name,
				Kind: tagKind(kind),
				File: file,
				Line: fset.Position(d.Pos()).Line,
			})

		case *ast.GenDecl:
			// type / var / const blocks. A GenDecl can contain
			// multiple specs (e.g. `var ( a int; b int )`); each
			// spec gets its own Symbol so the LLM sees them all.
			var kind string
			switch d.Tok {
			case token.TYPE:
				kind = "type"
			case token.VAR:
				kind = "var"
			case token.CONST:
				kind = "const"
			default:
				continue // import etc. — skip
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					out = append(out, Symbol{
						Name: s.Name.Name,
						Kind: tagKind(kind),
						File: file,
						Line: fset.Position(s.Pos()).Line,
					})
				case *ast.ValueSpec:
					for _, n := range s.Names {
						out = append(out, Symbol{
							Name: n.Name,
							Kind: tagKind(kind),
							File: file,
							Line: fset.Position(n.Pos()).Line,
						})
					}
				}
			}
		}
	}
	return out, nil
}

// receiverTypeName extracts "Foo" from a method receiver expression
// like `*Foo` or `Foo[T]`. Returns "" for shapes we don't recognise so
// the symbol still records under just the method name.
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.IndexExpr:
		return receiverTypeName(t.X)
	case *ast.IndexListExpr:
		return receiverTypeName(t.X)
	}
	return ""
}
