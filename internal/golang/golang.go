package golang

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"regexp"
	"strings"

	"github.com/dreasy-dev/repomap/internal/model"
)

var routeRe = regexp.MustCompile(`(?i)\.(Get|Post|Put|Delete|Patch|Head|Options|Handle|HandleFunc)\s*\(\s*"([^"]+)"`)
var httpFuncRe = regexp.MustCompile(`http\.HandleFunc\s*\(\s*"([^"]+)"`)

// FileResult is everything extracted from one Go file.
type FileResult struct {
	Symbols   []model.Symbol
	Import    *model.ImportEdge
	Endpoints []model.Endpoint
	Calls     []model.RawCall
	Aliases   map[string]string // import alias -> import path
}

// Parse extracts symbols, imports, endpoints and call sites from a Go file.
// src is the raw file content; rel is the repo-relative path used in output.
func Parse(rel string, src []byte) *FileResult {
	res := &FileResult{Aliases: map[string]string{}}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments)
	if err != nil {
		res.Endpoints = scanEndpoints(rel, src) // best-effort regex fallback
		return res
	}
	res.Endpoints = astEndpoints(fset, f, rel)

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			res.Symbols = append(res.Symbols, funcSymbol(fset, rel, d))
			res.Calls = append(res.Calls, calls(fset, rel, d)...)
		case *ast.GenDecl:
			res.Symbols = append(res.Symbols, genSymbols(fset, rel, d)...)
		}
	}

	if len(f.Imports) > 0 {
		imps := make([]string, 0, len(f.Imports))
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			imps = append(imps, p)
			alias := lastPathElem(p)
			if imp.Name != nil {
				if imp.Name.Name == "_" || imp.Name.Name == "." {
					continue
				}
				alias = imp.Name.Name
			}
			res.Aliases[alias] = p
		}
		res.Import = &model.ImportEdge{File: rel, Lang: "go", Imports: imps}
	}
	return res
}

// routeVerbs maps router method names to HTTP methods (Fiber/gin/chi/net-http).
var routeVerbs = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE",
	"Patch": "PATCH", "Head": "HEAD", "Options": "OPTIONS",
	"Handle": "ANY", "HandleFunc": "ANY", "All": "ANY", "Any": "ANY",
}

// astEndpoints extracts HTTP routes and reconstructs full paths through router
// groups, e.g. `api := app.Group("/vms"); api.Get("/:id")` -> `/vms/:id`.
func astEndpoints(fset *token.FileSet, f *ast.File, rel string) []model.Endpoint {
	type grp struct{ parent, prefix string }
	groups := map[string]grp{}
	ast.Inspect(f, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
			return true
		}
		lhs, ok := as.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		ce, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Group" || len(ce.Args) == 0 {
			return true
		}
		prefix, ok := stringLit(ce.Args[0])
		if !ok {
			return true
		}
		parent := ""
		if x, ok := sel.X.(*ast.Ident); ok {
			parent = x.Name
		}
		groups[lhs.Name] = grp{parent: parent, prefix: prefix}
		return true
	})

	resolve := func(v string) string {
		var parts []string
		seen := map[string]bool{}
		for v != "" && !seen[v] && groups[v].prefix != "" {
			seen[v] = true
			g := groups[v]
			parts = append([]string{g.prefix}, parts...)
			v = g.parent
		}
		return joinRoute(parts...)
	}

	var eps []model.Endpoint
	ast.Inspect(f, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok || len(ce.Args) == 0 {
			return true
		}
		sel, ok := ce.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		method, ok := routeVerbs[sel.Sel.Name]
		if !ok {
			return true
		}
		sub, ok := stringLit(ce.Args[0])
		if !ok || !strings.HasPrefix(sub, "/") { // skip header getters: c.Get("Authorization")
			return true
		}
		recv := ""
		if x, ok := sel.X.(*ast.Ident); ok {
			recv = x.Name
		}
		eps = append(eps, model.Endpoint{
			Method: method, Path: joinRoute(resolve(recv), sub), File: rel,
			Line: fset.Position(ce.Pos()).Line, Framework: "go",
		})
		return true
	})
	return eps
}

func stringLit(e ast.Expr) (string, bool) {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	return strings.Trim(bl.Value, "`\""), true
}

func joinRoute(parts ...string) string {
	var segs []string
	for _, p := range parts {
		for _, s := range strings.Split(p, "/") {
			if s != "" {
				segs = append(segs, s)
			}
		}
	}
	return "/" + strings.Join(segs, "/")
}

// calls returns every call site inside a function body as unresolved RawCalls.
func calls(fset *token.FileSet, rel string, fd *ast.FuncDecl) []model.RawCall {
	if fd.Body == nil {
		return nil
	}
	from := fd.Name.Name
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		from = "(" + typeString(fset, fd.Recv.List[0].Type) + ") " + fd.Name.Name
	}
	line := fset.Position(fd.Pos()).Line
	var out []model.RawCall
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := ce.Fun.(type) {
		case *ast.Ident:
			out = append(out, model.RawCall{FromName: from, FromFile: rel, FromLine: line, Name: fun.Name})
		case *ast.SelectorExpr:
			if x, ok := fun.X.(*ast.Ident); ok {
				out = append(out, model.RawCall{FromName: from, FromFile: rel, FromLine: line, Qual: x.Name, Name: fun.Sel.Name})
			}
		}
		return true
	})
	return out
}

func lastPathElem(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func funcSymbol(fset *token.FileSet, rel string, fd *ast.FuncDecl) model.Symbol {
	kind := "func"
	recv := ""
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		kind = "method"
		recv = typeString(fset, fd.Recv.List[0].Type)
	}
	cp := *fd
	cp.Body = nil
	cp.Doc = nil
	sig := printNode(fset, &cp)
	return model.Symbol{
		Name:      fd.Name.Name,
		Kind:      kind,
		Lang:      "go",
		File:      rel,
		Line:      fset.Position(fd.Pos()).Line,
		Signature: sig,
		Doc:       firstDocLine(fd.Doc),
		Receiver:  recv,
		Exported:  fd.Name.IsExported(),
	}
}

func genSymbols(fset *token.FileSet, rel string, gd *ast.GenDecl) []model.Symbol {
	var out []model.Symbol
	for _, spec := range gd.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			kind := "type"
			switch s.Type.(type) {
			case *ast.StructType:
				kind = "struct"
			case *ast.InterfaceType:
				kind = "interface"
			}
			out = append(out, model.Symbol{
				Name: s.Name.Name, Kind: kind, Lang: "go", File: rel,
				Line: fset.Position(s.Pos()).Line, Exported: s.Name.IsExported(),
				Doc: firstDocLine(gd.Doc),
			})
		case *ast.ValueSpec:
			kind := "var"
			if gd.Tok == token.CONST {
				kind = "const"
			}
			for _, n := range s.Names {
				if n.Name == "_" {
					continue
				}
				out = append(out, model.Symbol{
					Name: n.Name, Kind: kind, Lang: "go", File: rel,
					Line: fset.Position(n.Pos()).Line, Exported: n.IsExported(),
				})
			}
		}
	}
	return out
}

func scanEndpoints(rel string, src []byte) []model.Endpoint {
	var eps []model.Endpoint
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		for _, m := range routeRe.FindAllStringSubmatch(line, -1) {
			if !strings.HasPrefix(m[2], "/") { // skip header getters like c.Get("Authorization")
				continue
			}
			method := strings.ToUpper(m[1])
			if method == "HANDLE" || method == "HANDLEFUNC" {
				method = "ANY"
			}
			eps = append(eps, model.Endpoint{
				Method: method, Path: m[2], File: rel, Line: i + 1, Framework: "go",
			})
		}
		for _, m := range httpFuncRe.FindAllStringSubmatch(line, -1) {
			if !strings.HasPrefix(m[1], "/") {
				continue
			}
			eps = append(eps, model.Endpoint{
				Method: "ANY", Path: m[1], File: rel, Line: i + 1, Framework: "net/http",
			})
		}
	}
	return eps
}

func printNode(fset *token.FileSet, node ast.Node) string {
	var b bytes.Buffer
	if err := printer.Fprint(&b, fset, node); err != nil {
		return ""
	}
	return strings.TrimSpace(b.String())
}

func typeString(fset *token.FileSet, e ast.Expr) string {
	return printNode(fset, e)
}

func firstDocLine(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	t := strings.TrimSpace(cg.Text())
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[:i]
	}
	return t
}
