package jsts

import (
	"path"
	"regexp"
	"strings"

	"github.com/dreasy-dev/repomap/internal/model"
	"github.com/dreasy-dev/repomap/internal/walk"
)

var (
	reFunc   = regexp.MustCompile(`^\s*export\s+(?:default\s+)?(?:async\s+)?function\s+([A-Za-z0-9_$]+)`)
	reConst  = regexp.MustCompile(`^\s*export\s+(?:default\s+)?(?:const|let|var)\s+([A-Za-z0-9_$]+)`)
	reClass  = regexp.MustCompile(`^\s*export\s+(?:default\s+)?(?:abstract\s+)?class\s+([A-Za-z0-9_$]+)`)
	reType   = regexp.MustCompile(`^\s*export\s+(type|interface|enum)\s+([A-Za-z0-9_$]+)`)
	reImport = regexp.MustCompile(`^\s*import\s+.*?from\s+['"]([^'"]+)['"]`)
	reBare   = regexp.MustCompile(`^\s*import\s+['"]([^'"]+)['"]`)
	reVerb   = regexp.MustCompile(`^\s*export\s+(?:async\s+)?(?:function|const)\s+(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\b`)
)

// Parse extracts exported symbols and imports from a JS/TS file.
func Parse(rel, lang string, src []byte) ([]model.Symbol, *model.ImportEdge) {
	var symbols []model.Symbol
	var imports []string
	lines := strings.Split(string(src), "\n")
	for i, line := range lines {
		ln := i + 1
		if m := reFunc.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, sym(m[1], "function", lang, rel, ln))
		}
		if m := reClass.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, sym(m[1], "class", lang, rel, ln))
		}
		if m := reType.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, sym(m[2], m[1], lang, rel, ln))
		} else if m := reConst.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, sym(m[1], "const", lang, rel, ln))
		}
		if m := reImport.FindStringSubmatch(line); m != nil {
			imports = append(imports, m[1])
		} else if m := reBare.FindStringSubmatch(line); m != nil {
			imports = append(imports, m[1])
		}
	}
	var edge *model.ImportEdge
	if len(imports) > 0 {
		edge = &model.ImportEdge{File: rel, Lang: lang, Imports: imports}
	}
	return symbols, edge
}

func sym(name, kind, lang, rel string, line int) model.Symbol {
	return model.Symbol{Name: name, Kind: kind, Lang: lang, File: rel, Line: line, Exported: true}
}

// Endpoints derives Next.js routes by file convention across all JS/TS files.
func Endpoints(files []walk.File, read func(walk.File) []byte) []model.Endpoint {
	var eps []model.Endpoint
	for _, f := range files {
		switch f.Ext {
		case ".ts", ".tsx", ".js", ".jsx", ".mjs":
		default:
			continue
		}
		segs := strings.Split(f.Rel, "/")
		base := strings.TrimSuffix(path.Base(f.Rel), f.Ext)

		// App Router: <...>/app/<route>/{route|page}.ext
		if idx := lastIndex(segs, "app"); idx >= 0 {
			route := routePath(segs[idx+1 : len(segs)-1])
			if base == "route" {
				for _, m := range verbsIn(read(f)) {
					eps = append(eps, model.Endpoint{Method: m, Path: route, File: f.Rel, Framework: "next/app"})
				}
			} else if base == "page" {
				eps = append(eps, model.Endpoint{Method: "PAGE", Path: route, File: f.Rel, Framework: "next/app"})
			}
			continue
		}

		// Pages Router: <...>/pages/<route>.ext  (pages/api/* are endpoints)
		if idx := lastIndex(segs, "pages"); idx >= 0 {
			rest := segs[idx+1 : len(segs)-1]
			route := routePath(append(rest, strings.TrimSuffix(base, "")))
			if base == "index" {
				route = routePath(rest)
			}
			if len(rest) > 0 && rest[0] == "api" {
				eps = append(eps, model.Endpoint{Method: "ANY", Path: route, File: f.Rel, Framework: "next/pages-api"})
			} else if base != "_app" && base != "_document" {
				eps = append(eps, model.Endpoint{Method: "PAGE", Path: route, File: f.Rel, Framework: "next/pages"})
			}
		}
	}
	return eps
}

func verbsIn(src []byte) []string {
	var out []string
	for _, line := range strings.Split(string(src), "\n") {
		if m := reVerb.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	if len(out) == 0 {
		out = []string{"ANY"}
	}
	return out
}

// routePath turns directory segments into a URL path, handling Next conventions:
// route groups "(group)" are dropped, "[param]" -> ":param", "[...x]" -> "*".
func routePath(segs []string) string {
	var parts []string
	for _, s := range segs {
		if s == "" || (strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")")) {
			continue
		}
		if strings.HasPrefix(s, "[...") {
			parts = append(parts, "*")
		} else if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			parts = append(parts, ":"+strings.Trim(s, "[]"))
		} else {
			parts = append(parts, s)
		}
	}
	return "/" + strings.Join(parts, "/")
}

func lastIndex(segs []string, target string) int {
	for i := len(segs) - 1; i >= 0; i-- {
		if segs[i] == target {
			return i
		}
	}
	return -1
}
