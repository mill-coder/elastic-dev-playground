import { EditorView, basicSetup } from 'codemirror';
import { EditorState, Compartment } from '@codemirror/state';
import { linter, lintGutter } from '@codemirror/lint';
import { autocompletion } from '@codemirror/autocomplete';
import { parseLogstash, getCompletions } from './wasm-bridge.js';

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

async function logstashCompletionSource(context) {
  const word = context.matchBefore(/[a-zA-Z_][a-zA-Z0-9_]*/);
  if (!word && !context.explicit) return null;

  const source = context.state.doc.toString();
  const result = await getCompletions(source, context.pos);
  if (!result.options || result.options.length === 0) return null;

  return {
    from: result.from,
    options: result.options,
    validFor: /^[a-zA-Z_][a-zA-Z0-9_]*$/,
  };
}

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
  let cursorCallback = null;

  const view = new EditorView({
    state: EditorState.create({
      doc: SAMPLE,
      extensions: [
        basicSetup,
        autocompletion({ override: [logstashCompletionSource] }),
        lintGutter(),
        linterCompartment.of(createLogstashLinter()),
        EditorView.theme({
          '&': { height: '100%' },
          '.cm-scroller': { overflow: 'auto' },
        }),
        EditorView.updateListener.of((update) => {
          if (update.selectionSet || update.docChanged) {
            if (cursorCallback) cursorCallback(update.state.selection.main.head);
          }
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
    onCursorActivity(callback) {
      cursorCallback = callback;
    },
  };
}
