# Elastic Dev Playground

Elastic platform engineering toolkit — a suite of browser-based developer tools for the Elastic stack.

Everything runs client-side — the production Docker image includes a lightweight Node.js server for API proxying.

### special thanks

Special thanks for the work done by [breml](https://github.com/breml) for his logstash config parser: [breml/logstash-config](https://github.com/breml/logstash-config)


## Features

### logstash pipeline config writer

- **Syntax error highlighting** — red underlines and gutter icons on parse errors, powered by [breml/logstash-config](https://github.com/breml/logstash-config) PEG parser
- **Semantic validation** — yellow warnings for unknown plugin names, unknown options, and invalid codec names
- **Kibana pipeline management** — connect to Kibana to list, load, save, and delete Logstash pipelines via Centralized Pipeline Management
- **Dark theme editor** — CodeMirror 6 with monospace font, fills the viewport

### Import Data

- **Cross-cluster data copy** — copy documents from one Elasticsearch cluster to another using scroll/bulk API
- **Index filtering** — select source indices, apply query filters
- **Progress tracking** — real-time document count and transfer status


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

Outputs static files to `dist/`.

### Docker

```bash
docker build -t elastic-dev-playground .
docker run -p 3000:3000 elastic-dev-playground
```

The Docker image includes a Node.js production server that serves static files and proxies Kibana/ES API requests (same proxy logic as the Vite dev server).

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
├── Dockerfile             # Multi-stage build (Go -> Node -> Node.js server)
├── server.js              # Production server: static files + API proxy
├── go/
│   ├── main.go            # WASM entry point + error extraction
│   ├── registry.go        # Known plugins, codecs, and option schemas
│   └── validate.go        # AST walker for semantic validation
└── web/
    ├── index.html
    ├── vite.config.js
    └── src/
        ├── main.js              # App init: load WASM, create editor, wire panel
        ├── wasm-bridge.js       # WASM loading + JS wrapper
        ├── editor.js            # CodeMirror 6 setup + lint integration
        ├── kibana-api.js        # Kibana CPM API client
        ├── pipeline-panel.js    # Pipeline panel UI
        ├── elasticsearch-api.js # ES API client (scroll, bulk, count, mapping)
        ├── import-data.js       # Import Data page UI
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

## Import Data

Copy documents between Elasticsearch clusters directly from the browser.

### Prerequisites

- Two Elasticsearch clusters accessible from the server (or via the Vite dev proxy)

### Usage

1. Click **Import Data** in the navigation bar
2. Enter the source cluster URL and credentials, click **Connect**
3. Select the source index and optionally apply a query filter
4. Enter the destination cluster URL and credentials, click **Connect**
5. Click **Start Import** to begin copying documents via scroll/bulk API

## License

[MIT](LICENSE)
