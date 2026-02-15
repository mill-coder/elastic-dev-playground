# Feature 6: Import Data Between Elasticsearch Clusters

## Overview

Browser-based tool to copy data from one Elasticsearch cluster to another via the ES REST API, proxied through Vite's dev server to avoid CORS. Supports index/data stream selection, date range and field filters, progress tracking, and cancel support.

**Scope**: ES REST API integration via a Vite dev-server proxy. Same dev-only proxy pattern as Feature 5 (Kibana pipelines).

---

## Architecture

```
Browser UI (import data page)
  -> elasticsearch-api.js (fetch with Basic Auth)
  -> /es-api/* (Vite proxy plugin)
  -> Elasticsearch REST API (source + destination clusters)
```

### Why a proxy?

Elasticsearch does not set CORS headers by default, so browser `fetch()` to ES is blocked. The Vite dev server proxies requests, stripping the `/es-api` prefix and forwarding to the target URL specified in the `X-ES-Target` request header. Both source and destination clusters use the same proxy — the header distinguishes them per-request.

---

## ES API Reference

| Operation | Method | Path | Purpose |
|-----------|--------|------|---------|
| Cluster info | GET | `/` | Test connection, get cluster name/version |
| List indices | GET | `/_cat/indices?format=json` | Get all indices (filter out system `.` indices) |
| List data streams | GET | `/_data_stream` | Get data streams (filter out system `.` streams) |
| Get mapping | GET | `/{index}/_mapping` | Get field names for autocomplete |
| Count docs | POST | `/{index}/_count` | Estimate matching documents |
| Start scroll | POST | `/{index}/_search?scroll=2m` | Begin paginated read |
| Continue scroll | POST | `/_search/scroll` | Fetch next batch |
| Clear scroll | DELETE | `/_search/scroll` | Cleanup scroll context |
| Bulk index | POST | `/_bulk` | Write documents to destination |

All requests require Basic Authentication.

---

## UI Design

### Six card sections, vertically stacked

1. **Source Connection** — URL, username, password, connect/disconnect
2. **Select Index** — Dropdown of indices + data streams, refresh button
3. **Query Configuration** — Max rows, timestamp field, date range, field filters, live doc count
4. **Destination Connection** — Independent connection to target cluster, destination index name
5. **Import** — Start/cancel button with progress bar (count, percentage, elapsed time)
6. **Report** — Shown after completion: imported count, errors, duration, expandable error details

### Section enable/disable rules

- Section 2: enabled when source connected
- Section 3: enabled when index selected
- Section 4: always enabled (independent)
- Section 5 "Start Import": enabled when source + index + dest + dest index all set
- Section 6: shown only after import completes/cancels

### Live feedback

- Debounced count query (300ms) updates estimated matching docs when query parameters change
- Field name autocomplete from index mapping via `<datalist>`

### Credential persistence

`localStorage` with keys `es-import-source-credentials` and `es-import-dest-credentials`.

---

## Import Orchestration

1. Build query from UI state via `buildQuery()`
2. `getDocCount()` for progress total (capped at max rows)
3. `startScroll()` with batch size 1000
4. Loop: fetch batch -> `bulkIndex()` to destination -> update progress -> re-render
5. Stop when: max rows reached, no more hits, or user cancelled
6. `clearScroll()` cleanup
7. Set final state and render report

### Cancel support

`AbortController` signal checked between batches. On cancel: clear scroll, show partial report.

---

## Files

| File | Purpose |
|------|---------|
| `web/vite.config.js` | `esProxyPlugin()` for `/es-api/*` proxy |
| `web/src/elasticsearch-api.js` | ES API client (pure functions) |
| `web/src/import-data.js` | Import Data page component |
| `web/src/main.js` | Mounts component into `#page-import-data` |
| `web/index.html` | Container element + updated docs |
| `web/src/style.css` | Import data section/card styles |

---

## Error Handling

| Scenario | Handling |
|----------|----------|
| Connection refused | Proxy returns 502. Toast: "Cannot connect to Elasticsearch at ..." |
| Auth failure (401) | Toast: "Authentication failed. Check username/password." |
| Bulk indexing errors | Counted in progress, shown in expandable error details |
| Network timeout | Toast with error message |
| Empty query results | Shows "Estimated matching docs: 0" |
| Scroll error mid-import | Import stops, partial report shown |

---

## Test Cases

| Action | Expected |
|--------|----------|
| Connect to source with valid credentials | Status shows cluster name and version, indices appear |
| Connect with wrong password | Toast: auth failed |
| Select index | Dropdown populates, field names load for autocomplete |
| Change date range or add filter | Estimated count updates live (debounced) |
| Connect to destination | Independent from source, shows cluster info |
| Start import | Progress bar advances, documents appear in destination |
| Cancel mid-import | Partial report shown with what was imported |
| Import completes | Full report: imported count, errors, duration |
| Bulk errors | Error count shown, expandable details available |
