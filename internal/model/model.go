package model

import "time"

// Symbol is a named, addressable thing in the codebase: a function, type,
// const, export, etc. The whole point is that an agent can read Name+File+Line
// instead of grepping for it.
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // func, method, type, struct, interface, const, var, class, enum
	Lang      string `json:"lang"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Signature string `json:"signature,omitempty"`
	Doc       string `json:"doc,omitempty"`
	Receiver  string `json:"receiver,omitempty"` // Go methods
	Exported  bool   `json:"exported"`
}

// ImportEdge records that File depends on a set of modules/packages.
type ImportEdge struct {
	File    string   `json:"file"`
	Lang    string   `json:"lang"`
	Imports []string `json:"imports"`
}

// RawCall is an unresolved call site found in a function body. It gets resolved
// to a real symbol later, once every file has been parsed.
type RawCall struct {
	FromName string `json:"-"` // caller display name, e.g. "(*T) Method" or "Func"
	FromFile string `json:"-"`
	FromLine int    `json:"-"`
	Qual     string `json:"-"` // selector qualifier ident ("pkg" in pkg.Foo), "" for bare calls
	Name     string `json:"-"` // callee identifier
}

// CallEdge is a resolved caller -> callee relationship (Go only).
type CallEdge struct {
	From     string `json:"from"`
	FromFile string `json:"from_file"`
	FromLine int    `json:"from_line"`
	To       string `json:"to"`
	ToFile   string `json:"to_file"`
	ToLine   int    `json:"to_line"`
}

// Endpoint is an HTTP route discovered in the code (Go routers or Next.js).
type Endpoint struct {
	Method    string `json:"method"`
	Path      string `json:"path"`
	Handler   string `json:"handler,omitempty"`
	File      string `json:"file"`
	Line      int    `json:"line,omitempty"`
	Framework string `json:"framework,omitempty"`
}

// FileInfo is one indexed file with a content hash for staleness checks.
type FileInfo struct {
	Path  string `json:"path"`
	Lang  string `json:"lang"`
	Lines int    `json:"lines"`
	Hash  string `json:"hash"`
}

// GoModuleInfo is one Go module found anywhere in the tree (monorepo-aware).
type GoModuleInfo struct {
	Dir       string `json:"dir"`  // dir containing go.mod, relative to root ("." for root)
	Path      string `json:"path"` // module path from `module` directive
	GoVersion string `json:"go_version,omitempty"`
}

// NodeApp is one package.json found anywhere in the tree (monorepo-aware).
type NodeApp struct {
	Dir     string            `json:"dir"`
	Scripts map[string]string `json:"scripts,omitempty"`
}

// Project holds detected "how do I run this" metadata across a monorepo.
type Project struct {
	Modules     []GoModuleInfo `json:"modules,omitempty"`
	NodeApps    []NodeApp      `json:"node_apps,omitempty"`
	MakeTargets []string       `json:"make_targets,omitempty"`
	EntryPoints []string       `json:"entry_points,omitempty"`
	HasDocker   bool           `json:"has_docker,omitempty"`
	ModulePaths []string       `json:"-"` // all module paths, for local-import detection
}

// Index is the full in-memory representation that gets rendered to disk.
type Index struct {
	Root        string       `json:"root"`
	GeneratedAt time.Time    `json:"generated_at"`
	GitCommit   string       `json:"git_commit,omitempty"`
	Hash        string       `json:"hash"`
	Project     Project      `json:"project"`
	Files       []FileInfo   `json:"files"`
	Symbols     []Symbol     `json:"symbols"`
	Imports     []ImportEdge `json:"imports"`
	Endpoints   []Endpoint   `json:"endpoints"`
	CallGraph   []CallEdge   `json:"call_graph"`
	Tree        string       `json:"-"`
}
