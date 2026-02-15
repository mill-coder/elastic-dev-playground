import {
  testConnection, listIndices, listDataStreams, getIndexMapping,
  getDocCount, startScroll, continueScroll, clearScroll, bulkIndex, buildQuery,
} from './elasticsearch-api.js';

const SRC_STORAGE_KEY = 'es-import-source-credentials';
const DST_STORAGE_KEY = 'es-import-dest-credentials';

function loadCredentials(key) {
  try { return JSON.parse(localStorage.getItem(key)) || {}; } catch { return {}; }
}
function saveCredentials(key, url, user, pass) {
  localStorage.setItem(key, JSON.stringify({ url, user, pass }));
}

function el(tag, props) {
  const elem = document.createElement(tag);
  if (props) {
    for (const [key, val] of Object.entries(props)) {
      if (key === 'list') {
        elem.setAttribute('list', val);
      } else {
        elem[key] = val;
      }
    }
  }
  return elem;
}

function showToast(container, message, isError) {
  const toast = el('div', { className: 'toast ' + (isError ? 'toast-error' : 'toast-success'), textContent: message });
  container.appendChild(toast);
  setTimeout(() => toast.classList.add('toast-fade'), 2500);
  setTimeout(() => toast.remove(), 3000);
}

function debounce(fn, ms) {
  let timer;
  return (...args) => {
    clearTimeout(timer);
    timer = setTimeout(() => fn(...args), ms);
  };
}

function formatDuration(ms) {
  const s = Math.floor(ms / 1000);
  const m = Math.floor(s / 60);
  const sec = s % 60;
  return `${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`;
}

function formatNumber(n) {
  return n == null ? '—' : n.toLocaleString();
}

export function createImportDataPage() {
  const container = el('div', { className: 'import-container' });

  const toastContainer = el('div', { className: 'toast-container' });
  document.body.appendChild(toastContainer);
  const toast = (msg, isError) => showToast(toastContainer, msg, isError);

  // State
  let sourceConnected = false;
  let sourceUrl = '', sourceUser = '', sourcePass = '', sourceClusterInfo = null;
  let indices = [];
  let selectedIndex = null;
  let fieldNames = [];
  let maxRows = 10000;
  let timestampField = '@timestamp';
  let startDate = todayStart();
  let endDate = todayEnd();
  let filters = [];
  let estimatedCount = null;
  let countLoading = false;
  let destConnected = false;
  let destUrl = '', destUser = '', destPass = '', destClusterInfo = null;
  let destIndex = '';
  let importState = 'idle'; // idle | running | done | cancelled | error
  let progress = { imported: 0, total: 0, errors: 0, startTime: null, errorDetails: [] };
  let abortController = null;
  let progressTimer = null;

  function todayStart() {
    const d = new Date(); d.setHours(0, 0, 0, 0);
    return d.toISOString().slice(0, 16);
  }
  function todayEnd() {
    const d = new Date(); d.setHours(23, 59, 59, 999);
    return d.toISOString().slice(0, 16);
  }

  const debouncedCount = debounce(updateEstimatedCount, 300);

  async function updateEstimatedCount() {
    if (!sourceConnected || !selectedIndex) { estimatedCount = null; render(); return; }
    countLoading = true;
    render();
    try {
      const query = buildQuery(startDate, endDate, timestampField, filters);
      estimatedCount = await getDocCount(sourceUrl, sourceUser, sourcePass, selectedIndex, query);
    } catch {
      estimatedCount = null;
    }
    countLoading = false;
    render();
  }

  async function loadIndices() {
    try {
      const [idxList, dsList] = await Promise.all([
        listIndices(sourceUrl, sourceUser, sourcePass),
        listDataStreams(sourceUrl, sourceUser, sourcePass).catch(() => []),
      ]);
      const combined = [];
      for (const idx of idxList) combined.push({ name: idx.index, type: 'index', docs: idx['docs.count'], size: idx['store.size'] });
      for (const ds of dsList) combined.push({ name: ds.name, type: 'data_stream' });
      combined.sort((a, b) => a.name.localeCompare(b.name));
      indices = combined;
    } catch (err) {
      toast(err.message, true);
      indices = [];
    }
  }

  async function loadFieldNames() {
    if (!selectedIndex) { fieldNames = []; return; }
    try {
      fieldNames = await getIndexMapping(sourceUrl, sourceUser, sourcePass, selectedIndex);
    } catch {
      fieldNames = [];
    }
  }

  async function runImport() {
    importState = 'running';
    abortController = new AbortController();
    const query = buildQuery(startDate, endDate, timestampField, filters);

    let total;
    try {
      total = await getDocCount(sourceUrl, sourceUser, sourcePass, selectedIndex, query);
    } catch (err) {
      importState = 'error';
      progress.errorDetails.push(`Count failed: ${err.message}`);
      render();
      return;
    }
    const cap = Math.min(total, maxRows);
    progress = { imported: 0, total: cap, errors: 0, startTime: Date.now(), errorDetails: [] };
    render();

    // Start timer for elapsed time display
    progressTimer = setInterval(() => {
      if (importState === 'running') render();
    }, 1000);

    let scrollId = null;
    try {
      const scrollRes = await startScroll(sourceUrl, sourceUser, sourcePass, selectedIndex, query, 1000);
      scrollId = scrollRes._scroll_id;
      let hits = scrollRes.hits?.hits || [];

      while (hits.length > 0 && progress.imported < cap) {
        if (abortController.signal.aborted) break;

        const remaining = cap - progress.imported;
        const batch = hits.slice(0, remaining).map(h => h._source);

        try {
          const bulkRes = await bulkIndex(destUrl, destUser, destPass, destIndex, batch, abortController.signal);
          if (bulkRes && bulkRes.errors) {
            for (const item of bulkRes.items || []) {
              const op = item.index || item.create;
              if (op && op.error) {
                progress.errors++;
                if (progress.errorDetails.length < 100) {
                  progress.errorDetails.push(`Doc ${op._id}: ${op.error.reason || op.error.type}`);
                }
              }
            }
          }
          progress.imported += batch.length;
        } catch (err) {
          if (abortController.signal.aborted) break;
          progress.errors += batch.length;
          progress.errorDetails.push(`Bulk error: ${err.message}`);
        }

        render();

        if (progress.imported >= cap) break;
        if (abortController.signal.aborted) break;

        try {
          const nextRes = await continueScroll(sourceUrl, sourceUser, sourcePass, scrollId, abortController.signal);
          scrollId = nextRes._scroll_id;
          hits = nextRes.hits?.hits || [];
        } catch (err) {
          if (abortController.signal.aborted) break;
          progress.errorDetails.push(`Scroll error: ${err.message}`);
          break;
        }
      }
    } catch (err) {
      if (!abortController.signal.aborted) {
        progress.errorDetails.push(`Import error: ${err.message}`);
      }
    }

    if (scrollId) clearScroll(sourceUrl, sourceUser, sourcePass, scrollId);
    clearInterval(progressTimer);

    importState = abortController.signal.aborted ? 'cancelled' : 'done';
    render();
  }

  function cancelImport() {
    if (abortController) abortController.abort();
  }

  // Render
  function render() {
    container.innerHTML = '';
    container.appendChild(renderSourceSection());
    container.appendChild(renderIndexSection());
    container.appendChild(renderQuerySection());
    container.appendChild(renderDestSection());
    container.appendChild(renderImportSection());
    if (importState === 'done' || importState === 'cancelled' || importState === 'error') {
      container.appendChild(renderReportSection());
    }
  }

  function renderSourceSection() {
    const section = el('div', { className: 'import-section' });
    section.appendChild(el('div', { className: 'import-section-title', textContent: '1. Source Connection' }));
    const body = el('div', { className: 'import-section-body' });

    if (!sourceConnected) {
      const creds = loadCredentials(SRC_STORAGE_KEY);
      const row = el('div', { className: 'import-row' });
      const urlInput = el('input', { type: 'text', placeholder: 'Elasticsearch URL', value: creds.url || 'https://localhost:9200', className: 'pipeline-input import-input-wide' });
      const userInput = el('input', { type: 'text', placeholder: 'Username', value: creds.user || 'elastic', className: 'pipeline-input pipeline-input-user' });
      const passInput = el('input', { type: 'password', placeholder: 'Password', value: creds.pass || '', className: 'pipeline-input pipeline-input-pass' });
      const btn = el('button', { textContent: 'Connect', className: 'pipeline-btn pipeline-btn-primary' });

      btn.addEventListener('click', async () => {
        const url = urlInput.value.trim();
        if (!url) { toast('Enter a source URL', true); return; }
        btn.disabled = true; btn.textContent = 'Connecting...';
        try {
          sourceUrl = url; sourceUser = userInput.value.trim(); sourcePass = passInput.value;
          sourceClusterInfo = await testConnection(sourceUrl, sourceUser, sourcePass);
          sourceConnected = true;
          saveCredentials(SRC_STORAGE_KEY, sourceUrl, sourceUser, sourcePass);
          await loadIndices();
          toast(`Connected to "${sourceClusterInfo.name}"`);
          render();
        } catch (err) {
          toast(err.message, true);
          btn.disabled = false; btn.textContent = 'Connect';
        }
      });

      row.append(urlInput, userInput, passInput, btn);
      body.appendChild(row);
    } else {
      const row = el('div', { className: 'import-row' });
      const info = el('span', { className: 'import-connected-info', textContent: `Connected to "${sourceClusterInfo.name}" (v${sourceClusterInfo.version})` });
      const disconnectBtn = el('button', { textContent: 'Disconnect', className: 'pipeline-btn pipeline-btn-secondary' });
      disconnectBtn.addEventListener('click', () => {
        sourceConnected = false; sourceClusterInfo = null;
        indices = []; selectedIndex = null; fieldNames = [];
        estimatedCount = null; importState = 'idle';
        render();
      });
      row.append(info, disconnectBtn);
      body.appendChild(row);
    }

    section.appendChild(body);
    return section;
  }

  function renderIndexSection() {
    const section = el('div', { className: 'import-section' + (!sourceConnected ? ' import-section-disabled' : '') });
    section.appendChild(el('div', { className: 'import-section-title', textContent: '2. Select Index' }));
    const body = el('div', { className: 'import-section-body' });
    const row = el('div', { className: 'import-row' });

    const select = el('select', { className: 'pipeline-select import-input-wide' });
    if (indices.length === 0) {
      select.appendChild(el('option', { textContent: '(no indices)', value: '', disabled: true, selected: true }));
    } else {
      select.appendChild(el('option', { textContent: '-- select index --', value: '' }));
      for (const idx of indices) {
        const label = idx.type === 'data_stream' ? `${idx.name} (data stream)` : `${idx.name} (${idx.docs || '?'} docs)`;
        const opt = el('option', { textContent: label, value: idx.name });
        if (idx.name === selectedIndex) opt.selected = true;
        select.appendChild(opt);
      }
    }
    select.addEventListener('change', async () => {
      selectedIndex = select.value || null;
      if (!destIndex || destIndex === '') destIndex = selectedIndex || '';
      await loadFieldNames();
      debouncedCount();
      render();
    });

    const refreshBtn = el('button', { textContent: 'Refresh', className: 'pipeline-btn pipeline-btn-secondary' });
    refreshBtn.addEventListener('click', async () => {
      await loadIndices();
      toast('Index list refreshed');
      render();
    });

    row.append(select, refreshBtn);
    body.appendChild(row);
    section.appendChild(body);
    return section;
  }

  function renderQuerySection() {
    const section = el('div', { className: 'import-section' + (!selectedIndex ? ' import-section-disabled' : '') });
    section.appendChild(el('div', { className: 'import-section-title', textContent: '3. Query Configuration' }));
    const body = el('div', { className: 'import-section-body' });

    // Max rows
    const maxRow = el('div', { className: 'import-row' });
    maxRow.appendChild(el('span', { className: 'import-label', textContent: 'Max rows:' }));
    const maxInput = el('input', { type: 'number', value: String(maxRows), min: '1', max: '1000000', className: 'pipeline-input', style: 'width:120px' });
    maxInput.addEventListener('change', () => { maxRows = parseInt(maxInput.value) || 10000; });
    maxRow.appendChild(maxInput);
    body.appendChild(maxRow);

    // Timestamp field
    const tsRow = el('div', { className: 'import-row' });
    tsRow.appendChild(el('span', { className: 'import-label', textContent: 'Timestamp field:' }));
    const tsInput = el('input', { type: 'text', value: timestampField, className: 'pipeline-input', style: 'width:180px', list: 'field-list' });
    tsInput.addEventListener('change', () => { timestampField = tsInput.value; debouncedCount(); });
    tsRow.appendChild(tsInput);
    body.appendChild(tsRow);

    // Date range
    const dateRow = el('div', { className: 'import-row' });
    dateRow.appendChild(el('span', { className: 'import-label', textContent: 'Date range:' }));
    const startInput = el('input', { type: 'datetime-local', value: startDate, className: 'pipeline-input' });
    startInput.addEventListener('change', () => { startDate = startInput.value; debouncedCount(); });
    dateRow.appendChild(startInput);
    dateRow.appendChild(el('span', { textContent: 'to', style: 'color:#888;font-size:13px' }));
    const endInput = el('input', { type: 'datetime-local', value: endDate, className: 'pipeline-input' });
    endInput.addEventListener('change', () => { endDate = endInput.value; debouncedCount(); });
    dateRow.appendChild(endInput);
    body.appendChild(dateRow);

    // Filters
    const filtersLabel = el('div', { className: 'import-row' });
    filtersLabel.appendChild(el('span', { className: 'import-label', textContent: 'Filters:' }));
    body.appendChild(filtersLabel);

    // datalist for field autocomplete
    const datalist = el('datalist', { id: 'field-list' });
    for (const f of fieldNames) {
      datalist.appendChild(el('option', { value: f.name }));
    }
    body.appendChild(datalist);

    for (let i = 0; i < filters.length; i++) {
      const f = filters[i];
      const frow = el('div', { className: 'import-filter-row' });
      const fieldInput = el('input', { type: 'text', placeholder: 'field name', value: f.field || '', className: 'pipeline-input', style: 'width:180px', list: 'field-list' });
      const opSelect = el('select', { className: 'pipeline-select', style: 'min-width:80px' });
      for (const op of ['=', 'IN', 'NOT =', 'NOT IN']) {
        const opt = el('option', { textContent: op, value: op });
        if (op === f.operator) opt.selected = true;
        opSelect.appendChild(opt);
      }
      const valInput = el('input', { type: 'text', placeholder: 'value (comma-sep for IN)', value: f.value || '', className: 'pipeline-input', style: 'width:200px' });
      const removeBtn = el('button', { className: 'import-filter-remove', textContent: '\u2715' });

      const idx = i;
      fieldInput.addEventListener('change', () => { filters[idx].field = fieldInput.value; debouncedCount(); });
      opSelect.addEventListener('change', () => { filters[idx].operator = opSelect.value; debouncedCount(); });
      valInput.addEventListener('change', () => {
        const v = valInput.value;
        filters[idx].value = v;
        filters[idx].values = v.split(',').map(s => s.trim()).filter(Boolean);
        debouncedCount();
      });
      removeBtn.addEventListener('click', () => { filters.splice(idx, 1); debouncedCount(); render(); });

      frow.append(fieldInput, opSelect, valInput, removeBtn);
      body.appendChild(frow);
    }

    const addFilterBtn = el('button', { textContent: '+ Add filter', className: 'pipeline-btn pipeline-btn-secondary', style: 'align-self:flex-start' });
    addFilterBtn.addEventListener('click', () => {
      filters.push({ field: '', operator: '=', value: '', values: [] });
      render();
    });
    body.appendChild(addFilterBtn);

    // Estimated count
    const countRow = el('div', { className: 'import-row' });
    if (countLoading) {
      countRow.appendChild(el('span', { className: 'import-count-info loading', textContent: 'Counting...' }));
    } else if (estimatedCount != null) {
      countRow.appendChild(el('span', { className: 'import-count-info', textContent: `Estimated matching docs: ${formatNumber(estimatedCount)}` }));
    }
    body.appendChild(countRow);

    section.appendChild(body);
    return section;
  }

  function renderDestSection() {
    const section = el('div', { className: 'import-section' });
    section.appendChild(el('div', { className: 'import-section-title', textContent: '4. Destination Connection' }));
    const body = el('div', { className: 'import-section-body' });

    if (!destConnected) {
      const creds = loadCredentials(DST_STORAGE_KEY);
      const row = el('div', { className: 'import-row' });
      const urlInput = el('input', { type: 'text', placeholder: 'Elasticsearch URL', value: creds.url || 'https://localhost:9200', className: 'pipeline-input import-input-wide' });
      const userInput = el('input', { type: 'text', placeholder: 'Username', value: creds.user || 'elastic', className: 'pipeline-input pipeline-input-user' });
      const passInput = el('input', { type: 'password', placeholder: 'Password', value: creds.pass || '', className: 'pipeline-input pipeline-input-pass' });
      const btn = el('button', { textContent: 'Connect', className: 'pipeline-btn pipeline-btn-primary' });

      btn.addEventListener('click', async () => {
        const url = urlInput.value.trim();
        if (!url) { toast('Enter a destination URL', true); return; }
        btn.disabled = true; btn.textContent = 'Connecting...';
        try {
          destUrl = url; destUser = userInput.value.trim(); destPass = passInput.value;
          destClusterInfo = await testConnection(destUrl, destUser, destPass);
          destConnected = true;
          saveCredentials(DST_STORAGE_KEY, destUrl, destUser, destPass);
          toast(`Destination connected to "${destClusterInfo.name}"`);
          render();
        } catch (err) {
          toast(err.message, true);
          btn.disabled = false; btn.textContent = 'Connect';
        }
      });

      row.append(urlInput, userInput, passInput, btn);
      body.appendChild(row);
    } else {
      const row = el('div', { className: 'import-row' });
      const info = el('span', { className: 'import-connected-info', textContent: `Connected to "${destClusterInfo.name}" (v${destClusterInfo.version})` });
      const disconnectBtn = el('button', { textContent: 'Disconnect', className: 'pipeline-btn pipeline-btn-secondary' });
      disconnectBtn.addEventListener('click', () => {
        destConnected = false; destClusterInfo = null;
        if (importState === 'idle') render(); else render();
      });
      row.append(info, disconnectBtn);
      body.appendChild(row);
    }

    // Dest index name row
    const idxRow = el('div', { className: 'import-row' });
    idxRow.appendChild(el('span', { className: 'import-label', textContent: 'Destination index:' }));
    const destIdxInput = el('input', { type: 'text', value: destIndex, placeholder: 'index name', className: 'pipeline-input', style: 'width:280px' });
    destIdxInput.addEventListener('change', () => { destIndex = destIdxInput.value; });
    idxRow.appendChild(destIdxInput);
    body.appendChild(idxRow);

    section.appendChild(body);
    return section;
  }

  function renderImportSection() {
    const canStart = sourceConnected && selectedIndex && destConnected && destIndex;
    const section = el('div', { className: 'import-section' });
    section.appendChild(el('div', { className: 'import-section-title', textContent: '5. Import' }));
    const body = el('div', { className: 'import-section-body' });

    const row = el('div', { className: 'import-row' });
    if (importState !== 'running') {
      const startBtn = el('button', { textContent: 'Start Import', className: 'pipeline-btn pipeline-btn-primary' });
      if (!canStart) startBtn.disabled = true;
      startBtn.addEventListener('click', () => {
        importState = 'idle';
        progress = { imported: 0, total: 0, errors: 0, startTime: null, errorDetails: [] };
        runImport();
      });
      row.appendChild(startBtn);
    } else {
      const cancelBtn = el('button', { textContent: 'Cancel', className: 'pipeline-btn pipeline-btn-danger' });
      cancelBtn.addEventListener('click', cancelImport);
      row.appendChild(cancelBtn);
    }
    body.appendChild(row);

    // Progress bar
    if (importState === 'running' && progress.total > 0) {
      const pct = Math.round((progress.imported / progress.total) * 100);
      const bar = el('div', { className: 'import-progress-bar' });
      const fill = el('div', { className: 'import-progress-fill' });
      fill.style.width = pct + '%';
      bar.appendChild(fill);
      body.appendChild(bar);

      const elapsed = progress.startTime ? formatDuration(Date.now() - progress.startTime) : '00:00';
      const text = el('div', { className: 'import-progress-text', textContent: `${formatNumber(progress.imported)} / ${formatNumber(progress.total)} (${pct}%) — ${elapsed}` });
      body.appendChild(text);
    }

    section.appendChild(body);
    return section;
  }

  function renderReportSection() {
    const section = el('div', { className: 'import-section' });
    const title = importState === 'cancelled' ? '6. Report (Cancelled)' : importState === 'error' ? '6. Report (Error)' : '6. Report';
    section.appendChild(el('div', { className: 'import-section-title', textContent: title }));
    const body = el('div', { className: 'import-section-body' });

    const elapsed = progress.startTime ? formatDuration(Date.now() - progress.startTime) : '00:00';
    const report = el('div', { className: 'import-report' });
    report.appendChild(el('span', { className: 'import-report-stat', innerHTML: `Imported: <strong>${formatNumber(progress.imported)}</strong>` }));
    report.appendChild(el('span', { className: 'import-report-stat', innerHTML: `Errors: <strong>${formatNumber(progress.errors)}</strong>` }));
    report.appendChild(el('span', { className: 'import-report-stat', innerHTML: `Duration: <strong>${elapsed}</strong>` }));
    body.appendChild(report);

    if (progress.errorDetails.length > 0) {
      const toggle = el('button', { textContent: '\u25B8 Error details', className: 'pipeline-btn pipeline-btn-secondary', style: 'align-self:flex-start' });
      const details = el('div', { className: 'import-error-details', style: 'display:none', textContent: progress.errorDetails.join('\n') });
      toggle.addEventListener('click', () => {
        const visible = details.style.display !== 'none';
        details.style.display = visible ? 'none' : 'block';
        toggle.textContent = (visible ? '\u25B8' : '\u25BE') + ' Error details';
      });
      body.appendChild(toggle);
      body.appendChild(details);
    }

    section.appendChild(body);
    return section;
  }

  render();
  return container;
}
