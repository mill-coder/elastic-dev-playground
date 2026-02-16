# Feature 5: Kibana Pipeline Load/Save Integration

## Overview

Connect the elastic-dev-playground editor to a Kibana instance with Centralized Pipeline Management (CPM) enabled. Users can list, load, edit, save, and delete Logstash pipelines directly from the browser — closing the loop between editing and deployment.

**Scope**: Kibana CPM API integration via a Vite dev-server proxy.

> **Note (superseded)**: This plan originally stated that the proxy was dev-only. Since then, `server.js` was added to provide the same proxy functionality in production Docker deployments.

---

## Architecture

```
Browser UI (pipeline panel)
  → kibana-api.js (fetch with Basic Auth)
  → /kibana-api/* (Vite proxy plugin)
  → Kibana REST API (e.g., http://localhost:5601)
  → Elasticsearch pipeline storage
```

### Why a proxy?

Kibana does not set CORS headers, so browser `fetch()` to `localhost:5601` is blocked. The Vite dev server proxies requests, stripping the `/kibana-api` prefix and forwarding to the target URL specified in the `X-Kibana-Target` request header.

---

## Kibana API Reference

| Operation | Method | Path | Body | Response |
|-----------|--------|------|------|----------|
| List | GET | `/api/logstash/pipelines` | — | `{pipelines: [{id, description, last_modified, username}]}` |
| Get | GET | `/api/logstash/pipeline/{id}` | — | `{id, pipeline, description, settings}` |
| Save | PUT | `/api/logstash/pipeline/{id}` | `{pipeline, description, settings}` | 204 No Content |
| Delete | DELETE | `/api/logstash/pipeline/{id}` | — | 204 No Content |

All requests require:
- `kbn-xsrf: true` header (Kibana XSRF protection)
- Basic Authentication (`Authorization: Basic base64(user:pass)`)

---

## UI Design

### Panel location

Collapsible panel below the header bar, toggled by a "Pipelines" button in the nav. Zero footprint when collapsed; ~60px strip when open.

### Two states

**Disconnected** (default):
- Kibana URL input (default: `http://localhost:5601`)
- Username input (default: `elastic`)
- Password input
- Connect button

**Connected**:
- Connection info label (teal)
- Pipeline dropdown + Load button
- Save button (saves current pipeline)
- Pipeline ID text input + "Save As" button
- Delete button
- Disconnect button

### Persistence

Credentials (`kibanaUrl`, `username`, `password`) saved to `localStorage` as `kibana-credentials` on successful connect. Pre-filled on page load. No auto-connect.

### Notifications

Toast messages auto-dismiss after 3s. Success = teal, Error = red.

---

## Implementation Phases

### Phase 1: Vite proxy plugin

File: `web/vite.config.js`

Add `kibanaProxyPlugin()` using Vite's `configureServer` hook:
- Intercepts `POST/GET/PUT/DELETE /kibana-api/*`
- Reads `X-Kibana-Target` header for target Kibana URL
- Forwards request (stripping `/kibana-api` prefix)
- `rejectUnauthorized: false` for self-signed certs
- Returns 502 on connection error, 400 if header missing

### Phase 2: API client

File: `web/src/kibana-api.js`

Pure functions:
- `testConnection(kibanaUrl, user, pass)` → boolean
- `listPipelines(kibanaUrl, user, pass)` → Pipeline[]
- `getPipeline(kibanaUrl, user, pass, id)` → {id, pipeline, description}
- `savePipeline(kibanaUrl, user, pass, id, pipeline, description)` → void
- `deletePipeline(kibanaUrl, user, pass, id)` → void

5s timeout via `AbortSignal.timeout(5000)`.

### Phase 3: Editor API

File: `web/src/editor.js`

Change `createEditor()` to return `{ view, getContent(), setContent(text) }`.

### Phase 4: Pipeline panel

File: `web/src/pipeline-panel.js`

Programmatic DOM construction. Manages connect/disconnect state, pipeline CRUD, and toast notifications.

### Phase 5: Integration

- Add Pipelines toggle button to `index.html` header
- Wire panel in `main.js`
- Add CSS styles for panel, inputs, toasts

---

## Error Handling

| Scenario | Handling |
|----------|----------|
| Connection refused | Proxy returns 502. Toast: "Cannot connect to Kibana at ..." |
| Auth failure (401) | Toast: "Authentication failed. Check username/password." |
| Pipeline not found (404) | Toast: "Pipeline 'xxx' not found." |
| Save error | Toast with status and server message |
| Network timeout (5s) | Toast: "Request timed out." |
| Empty pipeline list | Dropdown shows "(no pipelines)" placeholder |

---

## Test Cases

| Action | Expected |
|--------|----------|
| Connect with valid credentials | Panel switches to connected view, pipelines listed |
| Connect with wrong password | Toast: auth failed |
| Connect with Kibana down | Toast: cannot connect |
| Load pipeline | Editor content replaced with pipeline source |
| Save pipeline | Toast: saved successfully, pipeline updated in Kibana |
| Save As with new ID | New pipeline created, appears in dropdown |
| Delete pipeline | Pipeline removed from list and Kibana |
| Disconnect | Panel returns to connect form |
| Page reload after connect | Fields pre-filled from localStorage, not auto-connected |

---

## Known Risks

| Risk | Mitigation |
|------|------------|
| Kibana API changes | Pin to Kibana 8.x; API has been stable since 7.x |
| No CORS in production | ~~Dev-only~~ — now handled by `server.js` in Docker production builds |
| Credentials in localStorage | Acceptable for local dev tool; document the trade-off |
| Large pipeline configs | CodeMirror handles large docs well; no special handling needed |
