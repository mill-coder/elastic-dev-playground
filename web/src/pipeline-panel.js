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

export function createPipelinePanel(editorApi, parserStatus) {
  const panel = document.createElement('div');
  panel.className = 'pipeline-panel';

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
  let statusEl = null;

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

  function buildStatusEl() {
    statusEl = el('span', {
      textContent: parserStatus.text,
      className: 'parser-status' + (parserStatus.state ? ' ' + parserStatus.state : ''),
    });
    return statusEl;
  }

  function buildConnectForm() {
    const form = document.createElement('div');
    form.className = 'pipeline-form';

    const creds = loadCredentials();

    const group1 = el('div', { className: 'submenu-group' });
    group1.appendChild(buildStatusEl());
    const sep1 = el('span', { className: 'pipeline-separator' });

    const group2 = el('div', { className: 'submenu-group' });
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

    group2.append(urlInput, userInput, passInput, connectBtn);
    form.append(group1, sep1, group2);
    return form;
  }

  function buildManageView() {
    const view = document.createElement('div');
    view.className = 'pipeline-manage';

    // Group 1 — Connection: parser status + disconnect + connection info
    const group1 = el('div', { className: 'submenu-group' });
    group1.appendChild(buildStatusEl());

    const sep1 = el('span', { className: 'pipeline-separator' });

    const group2 = el('div', { className: 'submenu-group' });
    const disconnectEmoji = el('span', {
      textContent: '\u23CF',
      className: 'disconnect-btn',
      title: 'Disconnect from Kibana',
    });
    const info = el('span', {
      textContent: `Connected to ${kibanaUrl}`,
      className: 'pipeline-connected-info',
    });

    disconnectEmoji.addEventListener('click', () => {
      connected = false;
      pipelines = [];
      currentPipelineId = null;
      currentDescription = '';
      clearCredentials();
      toast('Disconnected');
      render();
    });

    group2.append(disconnectEmoji, info);

    // Group 2 — Pipelines: select, Load, Save, Delete, Refresh
    const sep2 = el('span', { className: 'pipeline-separator' });
    const group3 = el('div', { className: 'submenu-group' });

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

    group3.append(select, loadBtn, saveBtn, deleteBtn, refreshBtn);

    // Group 3 — New: new pipeline ID input, Save As
    const sep3 = el('span', { className: 'pipeline-separator' });
    const group4 = el('div', { className: 'submenu-group' });

    const newIdInput = el('input', {
      type: 'text', placeholder: 'new-pipeline-id',
      className: 'pipeline-input pipeline-input-id',
    });
    const saveAsBtn = el('button', { textContent: 'Save As', className: 'pipeline-btn pipeline-btn-primary' });

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

    group4.append(newIdInput, saveAsBtn);

    view.append(group1, sep1, group2, sep2, group3, sep3, group4);
    return view;
  }

  function el(tag, props) {
    const elem = document.createElement(tag);
    if (props) Object.assign(elem, props);
    return elem;
  }

  panel.updateParserStatus = function ({ text, state }) {
    parserStatus.text = text;
    parserStatus.state = state;
    if (statusEl) {
      statusEl.textContent = text;
      statusEl.className = 'parser-status' + (state ? ' ' + state : '');
    }
  };

  render();
  return panel;
}
