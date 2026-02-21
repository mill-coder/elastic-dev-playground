import { initWasm, getVersions, setVersion } from './wasm-bridge.js';
import { createEditor } from './editor.js';
import { createPipelinePanel } from './pipeline-panel.js';
import { createImportDataPage } from './import-data.js';
import { createContextSidebar } from './context-sidebar.js';

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

  // Context sidebar
  const sidebar = createContextSidebar();

  // Debounced sidebar updates on cursor activity
  let sidebarTimer = null;
  editorApi.onCursorActivity((pos) => {
    clearTimeout(sidebarTimer);
    sidebarTimer = setTimeout(() => {
      const source = editorApi.getContent();
      sidebar.update(source, pos);
    }, 150);
  });

  const importPage = document.getElementById('page-import-data');
  importPage.appendChild(createImportDataPage());

  try {
    await initWasm();
    parserStatus.text = 'Parser ready';
    parserStatus.state = 'ready';

    // Populate version dropdown
    const versionInfo = await getVersions();
    if (versionInfo.versions && versionInfo.versions.length > 0) {
      const select = document.getElementById('version-select');
      for (const v of versionInfo.versions) {
        const opt = document.createElement('option');
        opt.value = v;
        opt.textContent = v;
        if (v === versionInfo.current) opt.selected = true;
        select.appendChild(opt);
      }
      select.addEventListener('change', async () => {
        try {
          await setVersion(select.value);
          editorApi.relint();
          // Refresh sidebar with new version's registry data
          const source = editorApi.getContent();
          const pos = editorApi.view.state.selection.main.head;
          sidebar.update(source, pos);
        } catch (err) {
          console.error('Failed to switch version:', err);
        }
      });
      document.getElementById('version-selector').style.display = '';
    }
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
