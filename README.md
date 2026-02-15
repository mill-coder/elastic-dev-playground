# Elastic Dev Playground

Elastic platform engineering toolkit — a suite of browser-based developer tools for the Elastic stack.

Everything runs client-side, no server needed.

### special thanks

Special thanks for the work done by [breml](https://github.com/breml) for his logstash config parser: [breml/logstash-config](https://github.com/breml/logstash-config)


## Features

### logstash pipeline config writer

- **Syntax error highlighting** — red underlines and gutter icons on parse errors, powered by [breml/logstash-config](https://github.com/breml/logstash-config) PEG parser
- **Semantic validation** — yellow warnings for unknown plugin names, unknown options, and invalid codec names
- **Kibana pipeline management** — connect to Kibana to list, load, save, and delete Logstash pipelines via Centralized Pipeline Management
- **Dark theme editor** — CodeMirror 6 with monospace font, fills the viewport

### elastic data import in sandbox instance

(coming soon)


## Quick start

### Prerequisites

- Go 1.22+
- Node.js 18+
- Make

### Development

```bash
make dev
```

This builds the WASM parser, installs npm dependencies, and starts Vite at `http://localhost:5173`.

### Production build

```bash
make build
```

Outputs static files to `dist/` — deploy to any HTTP server or GitHub Pages.

### Docker

```bash
docker build -t elastic-dev-playground .
docker run -p 8080:80 elastic-dev-playground
```

## How it works

```
CodeMirror 6 editor (browser)
  -> onChange (debounced 300ms)
  -> JS calls Go WASM: parseLogstashConfig(source) -> JSON
  -> Go parses config, extracts error positions
  -> Returns {ok, diagnostics: [{from, to, severity, message}]}
  -> On success, walks AST to validate plugin/option/codec names
  -> JS feeds diagnostics to CodeMirror linter
  -> Red underlines for errors, yellow for warnings
```

The Go parser ([breml/logstash-config](https://github.com/breml/logstash-config)) is compiled to WebAssembly. All parsing and validation happens in the browser — no data leaves the client.

## Project structure

```
elastic-dev-playground/
├── Makefile               # Build targets: wasm, dev, build, clean
├── Dockerfile             # Multi-stage build (Go -> Node -> nginx)
├── go/
│   ├── main.go            # WASM entry point + error extraction
│   ├── registry.go        # Known plugins, codecs, and option schemas
│   └── validate.go        # AST walker for semantic validation
└── web/
    ├── index.html
    ├── vite.config.js
    └── src/
        ├── main.js         # App init: load WASM, create editor
        ├── wasm-bridge.js  # WASM loading + JS wrapper
        ├── editor.js       # CodeMirror 6 setup + lint integration
        └── style.css
```

## Kibana Integration

The editor can connect to a Kibana instance with Centralized Pipeline Management (CPM) enabled to load and save Logstash pipelines.

### Prerequisites

- Kibana with Logstash CPM enabled (e.g., [elastic-sandbox](https://github.com/mill-coder/elastic-sandbox) or any Kibana 7.x/8.x with `xpack.management.enabled: true` in Logstash)
- The Vite dev server (`make dev`) provides a proxy to bypass CORS restrictions

### Usage

1. Click **Pipelines** in the header bar
2. Enter your Kibana URL (default: `http://localhost:5601`), username, and password
3. Click **Connect** to list available pipelines
4. **Load** a pipeline into the editor, edit it, and **Save** back to Kibana
5. Use **Save As** to create new pipelines with a custom ID

## License

[MIT](LICENSE)
