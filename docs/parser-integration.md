# Parser Integration

Detailed data flow for the Logstash config parser, from editor keystroke to error highlighting.

## Data Flow

```
CodeMirror 6 editor (browser)
  → onChange (debounced 300ms via CM linter)
  → JS calls Go WASM: parseLogstashConfig(source) → JSON string
  → Go calls github.com/breml/logstash-config Parse()
  → Extracts error positions via regex on pigeon parser error strings
  → Returns {ok, diagnostics: [{from, to, severity, message}]}
  → JS feeds diagnostics to CodeMirror's linter/lintGutter
  → Red underlines + gutter icons on errors
```

## Key Dependency

- **[breml/logstash-config](https://github.com/breml/logstash-config)** (Apache 2.0) — Pure Go PEG parser for the Logstash config format. Provides `Parse()` function and `GetFarthestFailure()`. All parser error types are unexported (pigeon-generated), so we extract positions by regex-parsing error strings.

## Error Positions

Pigeon byte offsets are treated as character offsets. This is correct for ASCII and covers virtually all real Logstash configurations.

## Debouncing

Debouncing is handled by CodeMirror's built-in `linter({delay: 300})` — no custom debounce logic needed.
