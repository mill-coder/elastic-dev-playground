import { initWasm } from './wasm-bridge.js';
import { createEditor } from './editor.js';
import { createPipelinePanel } from './pipeline-panel.js';

function navigate(hash) {
  const page = hash.replace('#', '') || 'editor';
  document.querySelectorAll('.page').forEach(el => {
    el.style.display = el.id === `page-${page}` ? '' : 'none';
  });
  document.querySelectorAll('.nav-link[data-page]').forEach(link => {
    link.classList.toggle('active', link.dataset.page === page);
  });
}

async function init() {
  const parserStatus = { text: 'Loading WASM parser...', state: '' };

  const editorApi = createEditor(document.getElementById('editor'));

  const pageEditor = document.getElementById('page-editor');
  const panel = createPipelinePanel(editorApi, parserStatus);
  pageEditor.insertBefore(panel, pageEditor.firstChild);

  try {
    await initWasm();
    parserStatus.text = 'Parser ready';
    parserStatus.state = 'ready';
  } catch (err) {
    parserStatus.text = `Failed to load parser: ${err.message}`;
    parserStatus.state = 'error';
    console.error('WASM init failed:', err);
  }
  panel.updateParserStatus(parserStatus);

  navigate(window.location.hash || '#editor');
  window.addEventListener('hashchange', () => navigate(window.location.hash));
}

init();
