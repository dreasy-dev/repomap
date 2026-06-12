package walk

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ignoreDirs are never indexed. They are noise (deps, build output, vcs).
var ignoreDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true,
	"build": true, ".next": true, "out": true, ".cache": true,
	".cursor": true, "coverage": true, ".idea": true, ".vscode": true,
	"__pycache__": true, ".venv": true, "venv": true, "target": true,
	".turbo": true, ".parcel-cache": true, "bin": true,
}

// ignoreFiles are junk/secret/lock files that should never be indexed.
var ignoreFiles = map[string]bool{
	".DS_Store": true, "Thumbs.db": true, ".gitkeep": true,
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"go.sum": true, "Cargo.lock": true, "composer.lock": true,
}

// ignoreExt are non-code extensions: docs, media, binaries, certs.
var ignoreExt = map[string]bool{
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true, ".odt": true, ".ini": true, ".log": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true,
	".ico": true, ".webp": true, ".bmp": true,
	".mp4": true, ".mov": true, ".avi": true, ".mp3": true, ".wav": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true, ".otf": true,
	".zip": true, ".tar": true, ".gz": true, ".tgz": true, ".rar": true, ".7z": true,
	".bin": true, ".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".o": true, ".a": true, ".class": true, ".jar": true,
	".ovpn": true, ".crt": true, ".csr": true, ".key": true, ".pem": true,
	".p12": true, ".pfx": true, ".env": true,
}

// ignoreFile reports whether a filename should be skipped (junk, secret, binary).
func ignoreFile(name, ext string) bool {
	if ignoreFiles[name] || ignoreExt[ext] {
		return true
	}
	if strings.HasPrefix(name, ".env") { // .env, .env.local, .env.production ...
		return true
	}
	return false
}

// File is one discovered source file.
type File struct {
	Abs string
	Rel string
	Ext string
}

// GitIgnore holds a deliberately lightweight subset of .gitignore semantics.
// It does NOT try to be a full gitignore engine, but it DOES respect anchoring:
// a leading "/" (or an embedded "/") anchors a pattern to the repo root, so
// "/vaultaire_serveur" must never match "src/vaultaire_serveur".
type GitIgnore struct {
	names  map[string]bool // bare names: match a file/dir with this basename anywhere
	rooted map[string]bool // root-relative paths: match only at that exact path
}

func (g GitIgnore) Match(name, relPath string) bool {
	return g.names[name] || g.rooted[relPath]
}

// LoadGitignore reads the top-level .gitignore. Wildcard lines are skipped
// (handled instead by the extension/name blocklists), keeping this safe.
func LoadGitignore(root string) GitIgnore {
	gi := GitIgnore{names: map[string]bool{}, rooted: map[string]bool{}}
	f, err := os.Open(filepath.Join(root, ".gitignore"))
	if err != nil {
		return gi
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.ContainsAny(line, "*?!") {
			continue
		}
		anchored := strings.HasPrefix(line, "/")
		p := strings.Trim(line, "/")
		if p == "" {
			continue
		}
		if anchored || strings.Contains(p, "/") {
			gi.rooted[p] = true // anchored to root, exact relative path
		} else {
			gi.names[p] = true // floating, match basename anywhere
		}
	}
	return gi
}

// Walk returns every source file under root, skipping ignored directories.
func Walk(root string) ([]File, error) {
	gi := LoadGitignore(root)
	var files []File
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if path == root {
				return nil
			}
			if ignoreDirs[name] || gi.Match(name, rel) || (strings.HasPrefix(name, ".") && name != ".") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ignoreFile(name, ext) || gi.Match(name, rel) {
			return nil
		}
		files = append(files, File{Abs: path, Rel: rel, Ext: ext})
		return nil
	})
	return files, err
}

// Lang maps a file extension to a coarse language label.
func Lang(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".ts":
		return "ts"
	case ".tsx":
		return "tsx"
	case ".js", ".mjs", ".cjs":
		return "js"
	case ".jsx":
		return "jsx"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".json":
		return "json"
	case ".md":
		return "markdown"
	default:
		return strings.TrimPrefix(ext, ".")
	}
}
