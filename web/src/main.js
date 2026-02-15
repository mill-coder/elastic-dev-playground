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
  const status = document.getElementById('status');
  try {
    await initWasm();
    status.textContent = 'Parser ready';
    status.classList.add('ready');
  } catch (err) {
    status.textContent = `Failed to load parser: ${err.message}`;
    status.classList.add('error');
    console.error('WASM init failed:', err);
  }

  const editorApi = createEditor(document.getElementById('editor'));

  const pageEditor = document.getElementById('page-editor');
  const panel = createPipelinePanel(editorApi);
  pageEditor.insertBefore(panel, pageEditor.firstChild);

  document.getElementById('pipelines-toggle').addEventListener('click', () => {
    panel.toggle();
  });

  navigate(window.location.hash || '#editor');
  window.addEventListener('hashchange', () => navigate(window.location.hash));
}

init();
