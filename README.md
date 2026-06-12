# repomap

**A one-shot codebase index for AI coding agents.**

Run it **once** in any project and it produces a structured, agent-readable
knowledge base under `.cursor/index/`. Your AI agents read that instead of
running `ls` / `grep` / `find` over and over to figure out *what is where, what
to call, and how to run things*.

- ⚡ **Fast** — pure Go, single binary, no CGO. ~750 files indexed in well under a second.
- 🎯 **Precise for Go** — native `go/ast` parsing: symbols, signatures, call graph, routes.
- 🧩 **Monorepo-aware** — detects every `go.mod` and `package.json` in the tree.
- 🤖 **Agent workflow** — installs a Cursor rule so agents *consult and maintain* the index.

---

## What it generates (`.cursor/index/`)

| File | Content |
|---|---|
| `MANIFEST.md` | Overview, entry points, **how to build/run/test** (Go modules, npm scripts, Make targets) |
| `TREE.md` | Annotated directory tree (code only — docs/media/secrets filtered out) |
| `SYMBOLS.md` | Every function / type / export → `file:line` (+ Go signatures & docs) |
| `IMPORTS.md` | Dependency graph (local vs external) |
| `CALLGRAPH.md` | Go call graph — caller ↔ callee (forward + reverse), with HTTP route-group reconstruction |
| `ENDPOINTS.md` | HTTP routes — Go routers (Fiber/gin/chi/net-http) + Next.js (`app/` & `pages/`) |
| `index.json` | Full machine-readable index |
| `.meta.json` | Content hashes + git commit for staleness checks |

---

## Requirements

- **Go >= 1.22** ([install Go](https://go.dev/dl/))
- Linux or macOS (Windows is not a target 🐧🍎)

Check your version:

```bash
go version
```

---

## Install

### Option A — `go install` (quickest)

```bash
go install github.com/dreasy-dev/repomap@latest
```

This drops the `repomap` binary into `$(go env GOPATH)/bin` (usually `~/go/bin`).

### Option B — build from source

```bash
git clone https://github.com/dreasy-dev/repomap.git
cd repomap
go build -o repomap .
# optional: move it onto your PATH
sudo mv repomap /usr/local/bin/      # or: mv repomap ~/.local/bin/
```

### Add Go's bin dir to your PATH

If you used `go install` and `repomap` isn't found, add `~/go/bin` to your PATH.

**macOS (zsh — the default):**

```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc
source ~/.zshrc
```

**Linux (bash):**

```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.bashrc
source ~/.bashrc
```

> Using zsh on Linux? Use `~/.zshrc` instead of `~/.bashrc`.

Verify:

```bash
repomap
which repomap
```

---

## Usage

Run these from the root of any project. An optional path argument lets you point
elsewhere (e.g. `repomap build ./my/project`).

| Command | What it does |
|---|---|
| `repomap init` | Install the Cursor rule **and** build the first index |
| `repomap build` | (Re)generate the index — run after big changes |
| `repomap check` | Exit code `1` if files changed since the last build (stale) |

Typical first run:

```bash
cd my-project
repomap init      # sets up .cursor/rules/codebase-index.mdc + builds .cursor/index/
```

Then later:

```bash
repomap build     # refresh after adding/renaming/moving things
repomap check     # cheap freshness check (great in a git hook or CI)
```

---

## How AI agents use it

`repomap init` installs `.cursor/rules/codebase-index.mdc` (an always-on Cursor
rule) that instructs every agent to:

1. **Read `.cursor/index/` before searching** the codebase.
2. Run `repomap check` if it suspects the index is stale.
3. Run `repomap build` after adding/renaming/moving symbols, files, or routes.

The result: agents stop burning turns on `ls`/`grep`/`find` and jump straight to
exact `file:line` locations.

---

## Supported languages

- **Go** — full AST parsing (funcs, methods, types, consts, vars, imports, call
  graph) with **router-group reconstruction**, e.g.
  `api := app.Group("/vms"); api.Get("/:id")` → `/vms/:id`. Zero dependencies.
- **TypeScript / JavaScript (incl. JSX/TSX)** — exported symbols + imports, and
  Next.js routes (App Router & Pages Router, incl. `[param]`, `[...slug]`, route
  groups). Lightweight/convention-based for portability.

---

## How it works (design notes)

- **Staleness is the real risk.** Every file is content-hashed; the overall hash
  plus git commit are stored, so `check` is reliable.
- **`.gitignore` anchoring is respected.** A leading `/` anchors a pattern to the
  repo root, so `/build` never accidentally hides `src/.../build/`.
- **Noise & secrets are skipped entirely** — `node_modules`, `vendor`, `.next`,
  build output, docs/office files, media, certs, lockfiles, and `.env` / `*.env`
  are never read or hashed.

---

## License

MIT — see [LICENSE](./LICENSE).
