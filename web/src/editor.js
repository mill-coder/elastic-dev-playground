import { EditorView, basicSetup } from 'codemirror';
import { EditorState, Compartment } from '@codemirror/state';
import { linter, lintGutter } from '@codemirror/lint';
import { parseLogstash } from './wasm-bridge.js';

const SAMPLE = `input {
  beats {
    port => 5044
  }
}

filter {
  mutate {
    add_tag => ["processed"]
  }
}

output {
  elasticsearch {
    hosts => ["http://localhost:9200"]
    index => "logstash-%{+YYYY.MM.dd}"
  }
}
`;

function createLogstashLinter() {
  return linter(async (view) => {
    const doc = view.state.doc.toString();
    if (!doc.trim()) return [];

    try {
      const result = await parseLogstash(doc);

      const diagnostics = (result.diagnostics || []).map(d => ({
        from: Math.max(0, d.from),
        to: Math.min(d.to, doc.length),
        severity: d.severity,
        message: d.message,
      }));

      if (!result.ok && result.farthest && !diagnostics.some(d => d.from === result.farthest.from)) {
        diagnostics.push({
          from: Math.max(0, result.farthest.from),
          to: Math.min(result.farthest.to, doc.length),
          severity: result.farthest.severity,
          message: result.farthest.message,
        });
      }

      return diagnostics;
    } catch (err) {
      console.error('Linter error:', err);
      return [];
    }
  }, { delay: 300 });
}

export function createEditor(parent) {
  const linterCompartment = new Compartment();

  const view = new EditorView({
    state: EditorState.create({
      doc: SAMPLE,
      extensions: [
        basicSetup,
        lintGutter(),
        linterCompartment.of(createLogstashLinter()),
        EditorView.theme({
          '&': { height: '100%' },
          '.cm-scroller': { overflow: 'auto' },
        }),
      ],
    }),
    parent,
  });

  return {
    view,
    getContent() {
      return view.state.doc.toString();
    },
    setContent(text) {
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: text },
      });
    },
    relint() {
      view.dispatch({
        effects: linterCompartment.reconfigure(createLogstashLinter()),
      });
    },
  };
}
