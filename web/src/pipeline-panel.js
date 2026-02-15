import { listPipelines, getPipeline, savePipeline, deletePipeline, testConnection } from './kibana-api.js';

const STORAGE_KEY = 'kibana-credentials';

function loadCredentials() {
  try {
    return JSON.parse(localStorage.getItem(STORAGE_KEY)) || {};
  } catch {
    return {};
  }
}

function saveCredentials(kibanaUrl, username, password) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify({ kibanaUrl, username, password }));
}

function clearCredentials() {
  localStorage.removeItem(STORAGE_KEY);
}

function showToast(container, message, isError) {
  const toast = document.createElement('div');
  toast.className = 'toast ' + (isError ? 'toast-error' : 'toast-success');
  toast.textContent = message;
  container.appendChild(toast);
  setTimeout(() => toast.classList.add('toast-fade'), 2500);
  setTimeout(() => toast.remove(), 3000);
}

export function createPipelinePanel(editorApi) {
  const panel = document.createElement('div');
  panel.className = 'pipeline-panel collapsed';

  const toastContainer = document.createElement('div');
  toastContainer.className = 'toast-container';
  document.body.appendChild(toastContainer);

  let connected = false;
  let kibanaUrl = '';
  let username = '';
  let password = '';
  let pipelines = [];
  let currentPipelineId = null;
  let currentDescription = '';

  function toast(msg, isError) {
    showToast(toastContainer, msg, isError);
  }

  function render() {
    panel.innerHTML = '';
    const content = document.createElement('div');
    content.className = 'pipeline-panel-content';

    if (!connected) {
      content.appendChild(buildConnectForm());
    } else {
      content.appendChild(buildManageView());
    }

    panel.appendChild(content);
  }

  function buildConnectForm() {
    const form = document.createElement('div');
    form.className = 'pipeline-form';

    const creds = loadCredentials();

    const urlInput = el('input', {
      type: 'text', placeholder: 'Kibana URL', value: creds.kibanaUrl || 'http://localhost:5601',
      className: 'pipeline-input pipeline-input-url',
    });
    const userInput = el('input', {
      type: 'text', placeholder: 'Username', value: creds.username || 'elastic',
      className: 'pipeline-input pipeline-input-user',
    });
    const passInput = el('input', {
      type: 'password', placeholder: 'Password', value: creds.password || '',
      className: 'pipeline-input pipeline-input-pass',
    });
    const connectBtn = el('button', {
      textContent: 'Connect', className: 'pipeline-btn pipeline-btn-primary',
    });

    connectBtn.addEventListener('click', async () => {
      const url = urlInput.value.trim();
      const user = userInput.value.trim();
      const pass = passInput.value;

      if (!url) { toast('Please enter a Kibana URL', true); return; }

      connectBtn.disabled = true;
      connectBtn.textContent = 'Connecting...';
      try {
        kibanaUrl = url;
        username = user;
        password = pass;

        const ok = await testConnection(kibanaUrl, username, password);
        if (!ok) {
          // testConnection swallows errors, try listing to get the real error
          await listPipelines(kibanaUrl, username, password);
        }

        pipelines = await listPipelines(kibanaUrl, username, password);
        connected = true;
        saveCredentials(kibanaUrl, username, password);
        currentPipelineId = null;
        currentDescription = '';
        toast('Connected to Kibana');
        render();
      } catch (err) {
        toast(err.message, true);
        connectBtn.disabled = false;
        connectBtn.textContent = 'Connect';
      }
    });

    form.append(urlInput, userInput, passInput, connectBtn);
    return form;
  }

  function buildManageView() {
    const view = document.createElement('div');
    view.className = 'pipeline-manage';

    // Connection info
    const info = el('span', {
      textContent: `Connected to ${kibanaUrl}`,
      className: 'pipeline-connected-info',
    });

    // Pipeline dropdown
    const select = el('select', { className: 'pipeline-select' });
    if (pipelines.length === 0) {
      select.appendChild(el('option', { textContent: '(no pipelines)', value: '', disabled: true, selected: true }));
    } else {
      select.appendChild(el('option', { textContent: '-- select pipeline --', value: '' }));
      for (const p of pipelines) {
        const opt = el('option', { textContent: p.id, value: p.id });
        if (p.id === currentPipelineId) opt.selected = true;
        select.appendChild(opt);
      }
    }

    const loadBtn = el('button', { textContent: 'Load', className: 'pipeline-btn pipeline-btn-secondary' });
    const saveBtn = el('button', { textContent: 'Save', className: 'pipeline-btn pipeline-btn-primary' });
    const deleteBtn = el('button', { textContent: 'Delete', className: 'pipeline-btn pipeline-btn-danger' });
    const refreshBtn = el('button', { textContent: 'Refresh', className: 'pipeline-btn pipeline-btn-secondary' });

    const sep = el('span', { className: 'pipeline-separator' });

    const newIdInput = el('input', {
      type: 'text', placeholder: 'new-pipeline-id',
      className: 'pipeline-input pipeline-input-id',
    });
    const saveAsBtn = el('button', { textContent: 'Save As', className: 'pipeline-btn pipeline-btn-primary' });

    const disconnectBtn = el('button', { textContent: 'Disconnect', className: 'pipeline-btn pipeline-btn-secondary' });

    // Event handlers
    loadBtn.addEventListener('click', async () => {
      const id = select.value;
      if (!id) { toast('Select a pipeline first', true); return; }
      try {
        const data = await getPipeline(kibanaUrl, username, password, id);
        editorApi.setContent(data.pipeline || '');
        currentPipelineId = data.id;
        currentDescription = data.description || '';
        toast(`Loaded pipeline "${id}"`);
        render();
      } catch (err) {
        toast(err.message, true);
      }
    });

    saveBtn.addEventListener('click', async () => {
      if (!currentPipelineId) { toast('No pipeline loaded. Use "Save As" to create a new one.', true); return; }
      try {
        await savePipeline(kibanaUrl, username, password, currentPipelineId, editorApi.getContent(), currentDescription);
        toast(`Saved pipeline "${currentPipelineId}"`);
      } catch (err) {
        toast(err.message, true);
      }
    });

    deleteBtn.addEventListener('click', async () => {
      const id = select.value;
      if (!id) { toast('Select a pipeline first', true); return; }
      try {
        await deletePipeline(kibanaUrl, username, password, id);
        if (currentPipelineId === id) {
          currentPipelineId = null;
          currentDescription = '';
        }
        pipelines = await listPipelines(kibanaUrl, username, password);
        toast(`Deleted pipeline "${id}"`);
        render();
      } catch (err) {
        toast(err.message, true);
      }
    });

    refreshBtn.addEventListener('click', async () => {
      try {
        pipelines = await listPipelines(kibanaUrl, username, password);
        toast('Pipeline list refreshed');
        render();
      } catch (err) {
        toast(err.message, true);
      }
    });

    saveAsBtn.addEventListener('click', async () => {
      const id = newIdInput.value.trim();
      if (!id) { toast('Enter a pipeline ID', true); return; }
      try {
        await savePipeline(kibanaUrl, username, password, id, editorApi.getContent(), '');
        currentPipelineId = id;
        currentDescription = '';
        pipelines = await listPipelines(kibanaUrl, username, password);
        toast(`Saved new pipeline "${id}"`);
        render();
      } catch (err) {
        toast(err.message, true);
      }
    });

    disconnectBtn.addEventListener('click', () => {
      connected = false;
      pipelines = [];
      currentPipelineId = null;
      currentDescription = '';
      clearCredentials();
      toast('Disconnected');
      render();
    });

    view.append(
      info, select, loadBtn, saveBtn, deleteBtn, refreshBtn,
      sep,
      newIdInput, saveAsBtn,
      disconnectBtn,
    );
    return view;
  }

  function el(tag, props) {
    const elem = document.createElement(tag);
    if (props) Object.assign(elem, props);
    return elem;
  }

  // Public toggle method
  panel.toggle = function () {
    panel.classList.toggle('collapsed');
  };

  render();
  return panel;
}
