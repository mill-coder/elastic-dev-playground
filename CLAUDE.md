# elastic-dev-playground

## Project identity

Elastic platform engineering toolkit — a suite of browser-based developer tools for the Elastic stack. The first feature is a **Logstash configuration editor** with live error highlighting, powered by a Go parser compiled to WebAssembly. No server needed — everything runs client-side.

- **License**: MIT
- **Status**: Beta
- **Detailed implementation plans**: see [`plans/`](plans/) — features are independent and can be implemented in any order

### Feature status

| # | Feature | Plan | Status |
|---|---------|------|--------|
| 1 | Syntax error highlighting | `plans/feature-1-syntax-errors.md` | Done |
| 2 | Semantic validation | `plans/feature-2-semantic-validation.md` | Done |
| 3 | Code completion | `plans/feature-3-code-completion.md` | Not started |
| 4 | Registry scraper | `plans/feature-4-registry-scraper.md` | Not started |
| 5 | Kibana pipeline management | `plans/feature-5-kibana-pipelines.md` | Done |
| 6 | Import data | `plans/feature-6-import-data.md` | Done |

## Architecture

```
Browser-based SPA (Vite + vanilla JS)
├── Logstash Editor — CodeMirror 6 + Go WASM parser
│   ├── Live syntax errors (pigeon parser → diagnostics)
│   ├── Semantic validation (AST walker + plugin registry)
│   └── Kibana CPM integration (list/load/save/delete pipelines)
├── Import Data — Copy data between ES clusters via scroll/bulk API
└── Documentation — in-app feature reference
```

For the detailed parser→CodeMirror data flow, see [`docs/parser-integration.md`](docs/parser-integration.md).

### Components

| Component | Tech | Location |
|-----------|------|----------|
| Parser WASM module | Go + `syscall/js` | `go/` |
| Web frontend | Vite + CodeMirror 6 | `web/` |
| Kibana integration | Vite proxy + fetch API | `web/src/kibana-api.js`, `web/src/pipeline-panel.js` |
| Import data | Vite proxy + ES scroll/bulk API | `web/src/elasticsearch-api.js`, `web/src/import-data.js` |
| Build system | Makefile | root |

## Tech stack

- **Go 1.22+** — compiled to WASM via `GOOS=js GOARCH=wasm`
- **Node.js 18+** — for Vite dev server and npm deps
- **Vite** — zero-config bundler for the frontend
- **CodeMirror 6** — modular editor with built-in `linter()` extension
- **No backend** — fully static, deployable to any HTTP server or GitHub Pages

## Project structure

```
elastic-dev-playground/
├── CLAUDE.md              # This file
├── plans/                 # Detailed implementation plans
│   ├── feature-1-syntax-errors.md
│   ├── feature-2-semantic-validation.md
│   ├── feature-3-code-completion.md
│   ├── feature-4-registry-scraper.md
│   ├── feature-5-kibana-pipelines.md
│   └── feature-6-import-data.md
├── docs/
│   └── parser-integration.md  # Detailed parser→editor data flow
├── Makefile               # Build targets: wasm, dev, build, clean
├── .gitignore
├── LICENSE
├── go/
│   ├── go.mod
│   ├── go.sum
│   ├── main.go            # WASM entry: parser bridge + error extraction
│   ├── registry.go        # Known plugins, codecs, and option schemas
│   └── validate.go        # AST walker for semantic validation
└── web/
    ├── package.json
    ├── vite.config.js
    ├── index.html
    ├── src/
    │   ├── main.js           # App init: load WASM, create editor, wire panel
    │   ├── wasm-bridge.js   # WASM loading + parseLogstash() wrapper
    │   ├── editor.js        # CodeMirror 6 setup + lint integration
    │   ├── kibana-api.js    # Kibana CPM API client (list/get/save/delete)
    │   ├── pipeline-panel.js # Pipeline panel UI (connect, load, save)
    │   ├── elasticsearch-api.js # ES API client (scroll, bulk, count, mapping)
    │   ├── import-data.js   # Import Data page (source→dest copy with filters)
    │   └── style.css
    └── public/             # Build artifacts (gitignored)
        ├── parser.wasm
        └── wasm_exec.js
```

## Conventions

- **Scope**: parse errors, semantic validation (unknown plugins/options/codecs), code completion, and Kibana pipeline management
- Build artifacts (`parser.wasm`, `wasm_exec.js`, `node_modules/`, `dist/`) are gitignored
- Go→JS data exchange uses JSON strings (most reliable with `syscall/js`)
- Error positions: pigeon byte offsets treated as char offsets (correct for ASCII, covers ~all real Logstash configs)
- Debouncing handled by CodeMirror's built-in `linter({delay: 300})`

## Build & run

```bash
make dev      # Build WASM + start Vite dev server
make build    # Production build into dist/
make clean    # Remove all build artifacts
```
