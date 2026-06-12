package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "embed"

	"github.com/dreasy-dev/repomap/internal/golang"
	"github.com/dreasy-dev/repomap/internal/jsts"
	"github.com/dreasy-dev/repomap/internal/model"
	"github.com/dreasy-dev/repomap/internal/render"
	"github.com/dreasy-dev/repomap/internal/walk"
)

//go:embed assets/codebase-index.mdc
var ruleTemplate string

const outRel = ".cursor/index"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	root := "."
	if len(os.Args) > 2 {
		root = os.Args[2]
	}
	abs, err := filepath.Abs(root)
	must(err)

	switch cmd {
	case "build":
		idx, err := build(abs)
		must(err)
		must(render.Write(filepath.Join(abs, outRel), idx))
		fmt.Printf("indexed %d files, %d symbols, %d endpoints, %d call edges → %s\n",
			len(idx.Files), len(idx.Symbols), len(idx.Endpoints), len(idx.CallGraph), outRel)
	case "check":
		os.Exit(check(abs))
	case "init":
		must(initProject(abs))
		idx, err := build(abs)
		must(err)
		must(render.Write(filepath.Join(abs, outRel), idx))
		fmt.Println("installed rule + built index")
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `repomap — one-shot codebase index for AI agents

usage:
  repomap build [path]   scan and write .cursor/index/
  repomap check [path]   exit 1 if the index is stale
  repomap init  [path]   install the Cursor rule, then build`)
}

func build(root string) (*model.Index, error) {
	files, err := walk.Walk(root)
	if err != nil {
		return nil, err
	}

	idx := &model.Index{Root: root, GeneratedAt: time.Now()}
	walkFiles := make([]walk.File, 0, len(files))
	cache := map[string][]byte{}
	var rawCalls []model.RawCall
	aliasesByFile := map[string]map[string]string{}

	read := func(f walk.File) []byte {
		if b, ok := cache[f.Rel]; ok {
			return b
		}
		b, _ := os.ReadFile(f.Abs)
		cache[f.Rel] = b
		return b
	}

	for _, f := range files {
		lang := walk.Lang(f.Ext)
		src := read(f)
		idx.Files = append(idx.Files, model.FileInfo{
			Path: f.Rel, Lang: lang, Lines: countLines(src), Hash: hashBytes(src),
		})
		walkFiles = append(walkFiles, f)

		switch f.Ext {
		case ".go":
			r := golang.Parse(f.Rel, src)
			idx.Symbols = append(idx.Symbols, r.Symbols...)
			if r.Import != nil {
				idx.Imports = append(idx.Imports, *r.Import)
			}
			idx.Endpoints = append(idx.Endpoints, r.Endpoints...)
			rawCalls = append(rawCalls, r.Calls...)
			aliasesByFile[f.Rel] = r.Aliases
		case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
			syms, edge := jsts.Parse(f.Rel, lang, src)
			idx.Symbols = append(idx.Symbols, syms...)
			if edge != nil {
				idx.Imports = append(idx.Imports, *edge)
			}
		}
	}

	idx.Endpoints = append(idx.Endpoints, jsts.Endpoints(walkFiles, read)...)
	idx.Project = detectProject(root, read, walkFiles)
	idx.CallGraph = resolveCalls(idx, rawCalls, aliasesByFile)
	idx.GitCommit = gitCommit(root)
	idx.Hash = overallHash(idx.Files)
	return idx, nil
}

// resolveCalls turns raw Go call sites into edges that point at real symbols.
// It resolves bare calls within the same package and `pkg.Func` calls across
// internal packages. Method calls and external packages are intentionally
// skipped (would require full type resolution).
func resolveCalls(idx *model.Index, raw []model.RawCall, aliases map[string]map[string]string) []model.CallEdge {
	byDir := map[string]map[string]model.Symbol{}
	byImportPath := map[string]map[string]model.Symbol{}
	for _, s := range idx.Symbols {
		if s.Lang != "go" || s.Kind != "func" {
			continue
		}
		dir := path.Dir(s.File)
		if byDir[dir] == nil {
			byDir[dir] = map[string]model.Symbol{}
		}
		byDir[dir][s.Name] = s
		if ip := goImportPath(idx.Project.Modules, s.File); ip != "" {
			if byImportPath[ip] == nil {
				byImportPath[ip] = map[string]model.Symbol{}
			}
			byImportPath[ip][s.Name] = s
		}
	}

	seen := map[string]bool{}
	var edges []model.CallEdge
	for _, c := range raw {
		var target model.Symbol
		var ok bool
		if c.Qual == "" {
			target, ok = byDir[path.Dir(c.FromFile)][c.Name]
		} else if al := aliases[c.FromFile]; al != nil {
			if ip, has := al[c.Qual]; has {
				target, ok = byImportPath[ip][c.Name]
			}
		}
		if !ok || (target.File == c.FromFile && target.Line == c.FromLine) {
			continue
		}
		key := fmt.Sprintf("%s#%d->%s#%d", c.FromFile, c.FromLine, target.File, target.Line)
		if seen[key] {
			continue
		}
		seen[key] = true
		edges = append(edges, model.CallEdge{
			From: c.FromName, FromFile: c.FromFile, FromLine: c.FromLine,
			To: target.Name, ToFile: target.File, ToLine: target.Line,
		})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].FromFile != edges[j].FromFile {
			return edges[i].FromFile < edges[j].FromFile
		}
		if edges[i].FromLine != edges[j].FromLine {
			return edges[i].FromLine < edges[j].FromLine
		}
		return edges[i].To < edges[j].To
	})
	return edges
}

// goImportPath computes the Go import path of a file's package using the set of
// modules discovered in the tree (longest matching module dir wins).
func goImportPath(modules []model.GoModuleInfo, fileRel string) string {
	best := -1
	var bm model.GoModuleInfo
	for _, m := range modules {
		md := m.Dir
		if !(md == "." || fileRel == md || strings.HasPrefix(fileRel, md+"/")) {
			continue
		}
		l := len(md)
		if md == "." {
			l = 0
		}
		if l > best {
			best, bm = l, m
		}
	}
	if best < 0 {
		return ""
	}
	dir := path.Dir(fileRel)
	rel := dir
	if bm.Dir != "." {
		rel = strings.TrimPrefix(strings.TrimPrefix(dir, bm.Dir), "/")
	}
	if rel == "" || rel == "." {
		return bm.Path
	}
	return bm.Path + "/" + rel
}

func check(root string) int {
	metaPath := filepath.Join(root, outRel, ".meta.json")
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		fmt.Println("no index found — run `repomap build`")
		return 1
	}
	var meta struct {
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		fmt.Println("corrupt index metadata — run `repomap build`")
		return 1
	}
	files, err := walk.Walk(root)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	infos := make([]model.FileInfo, 0, len(files))
	for _, f := range files {
		b, _ := os.ReadFile(f.Abs)
		infos = append(infos, model.FileInfo{Path: f.Rel, Hash: hashBytes(b)})
	}
	if overallHash(infos) == meta.Hash {
		fmt.Println("index is up to date")
		return 0
	}
	fmt.Println("index is STALE — run `repomap build`")
	return 1
}

func initProject(root string) error {
	dir := filepath.Join(root, ".cursor", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "codebase-index.mdc"), []byte(ruleTemplate), 0o644)
}

// detectProject scans the whole tree (monorepo-aware) for go.mod, package.json,
// Makefile, Dockerfile and Go entry points.
func detectProject(root string, read func(walk.File) []byte, files []walk.File) model.Project {
	p := model.Project{}
	makeSet := map[string]bool{}
	for _, f := range files {
		dir := path.Dir(f.Rel)
		switch path.Base(f.Rel) {
		case "go.mod":
			mod := model.GoModuleInfo{Dir: dir}
			for _, line := range strings.Split(string(read(f)), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "module ") {
					mod.Path = strings.TrimSpace(strings.TrimPrefix(line, "module"))
				} else if strings.HasPrefix(line, "go ") {
					mod.GoVersion = strings.TrimSpace(strings.TrimPrefix(line, "go"))
				}
			}
			if mod.Path != "" {
				p.Modules = append(p.Modules, mod)
				p.ModulePaths = append(p.ModulePaths, mod.Path)
			}
		case "package.json":
			var pkg struct {
				Scripts map[string]string `json:"scripts"`
			}
			if json.Unmarshal(read(f), &pkg) == nil && len(pkg.Scripts) > 0 {
				p.NodeApps = append(p.NodeApps, model.NodeApp{Dir: dir, Scripts: pkg.Scripts})
			}
		case "Makefile":
			for _, t := range makeTargets(string(read(f))) {
				makeSet[t] = true
			}
		case "Dockerfile":
			p.HasDocker = true
		}
		if strings.HasSuffix(f.Rel, "main.go") && strings.Contains(string(read(f)), "package main") {
			p.EntryPoints = append(p.EntryPoints, f.Rel)
		}
	}
	for t := range makeSet {
		p.MakeTargets = append(p.MakeTargets, t)
	}
	sort.Strings(p.MakeTargets)
	sort.Strings(p.EntryPoints)
	sort.Slice(p.Modules, func(i, j int) bool { return p.Modules[i].Dir < p.Modules[j].Dir })
	sort.Slice(p.NodeApps, func(i, j int) bool { return p.NodeApps[i].Dir < p.NodeApps[j].Dir })
	return p
}

func makeTargets(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if line == "" || line[0] == '\t' || line[0] == '#' || line[0] == ' ' {
			continue
		}
		if i := strings.IndexByte(line, ':'); i > 0 {
			name := strings.TrimSpace(line[:i])
			if name != "" && !strings.ContainsAny(name, " =.") {
				out = append(out, name)
			}
		}
	}
	return out
}

func gitCommit(root string) string {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func overallHash(files []model.FileInfo) string {
	cp := append([]model.FileInfo(nil), files...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Path < cp[j].Path })
	h := sha256.New()
	for _, f := range cp {
		fmt.Fprintf(h, "%s:%s\n", f.Path, f.Hash)
	}
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum)[:12]
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := strings.Count(string(b), "\n")
	if !strings.HasSuffix(string(b), "\n") {
		n++
	}
	return n
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
