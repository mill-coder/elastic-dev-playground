import { getContextInfo } from './wasm-bridge.js';

const STORAGE_KEY = 'context-sidebar-collapsed';

export function createContextSidebar() {
  const container = document.getElementById('context-sidebar');

  // Header
  const header = document.createElement('div');
  header.className = 'sidebar-header';

  const title = document.createElement('span');
  title.className = 'sidebar-title';
  title.textContent = 'Context Help';

  const toggleBtn = document.createElement('button');
  toggleBtn.className = 'sidebar-toggle';
  toggleBtn.title = 'Toggle sidebar';
  toggleBtn.textContent = '\u00BB'; // >>

  header.appendChild(title);
  header.appendChild(toggleBtn);

  // Content area
  const content = document.createElement('div');
  content.className = 'sidebar-content';

  container.appendChild(header);
  container.appendChild(content);

  // Collapsed toggle button (visible when sidebar is collapsed)
  const collapsedToggle = document.createElement('button');
  collapsedToggle.className = 'sidebar-toggle collapsed-toggle';
  collapsedToggle.title = 'Show context help';
  collapsedToggle.textContent = '\u00AB'; // <<
  collapsedToggle.style.display = 'none';
  container.parentElement.appendChild(collapsedToggle);

  // Collapse state
  let collapsed = localStorage.getItem(STORAGE_KEY) === 'true';
  applyCollapsed();

  function applyCollapsed() {
    container.classList.toggle('collapsed', collapsed);
    collapsedToggle.style.display = collapsed ? '' : 'none';
    toggleBtn.textContent = collapsed ? '\u00AB' : '\u00BB';
  }

  function toggle() {
    collapsed = !collapsed;
    localStorage.setItem(STORAGE_KEY, collapsed);
    applyCollapsed();
  }

  toggleBtn.addEventListener('click', toggle);
  collapsedToggle.addEventListener('click', toggle);

  // Rendering
  let lastJSON = '';

  function render(info) {
    const json = JSON.stringify(info);
    if (json === lastJSON) return;
    lastJSON = json;

    content.innerHTML = '';

    switch (info.kind) {
      case 'top-level':
        renderTopLevel(content);
        break;
      case 'section':
        renderSection(content, info);
        break;
      case 'plugin':
        renderPlugin(content, info);
        break;
      case 'codec':
        renderCodec(content, info);
        break;
      default:
        renderNone(content);
        break;
    }
  }

  async function update(source, pos) {
    try {
      const info = await getContextInfo(source, pos);
      render(info);
    } catch (err) {
      console.error('Context sidebar error:', err);
    }
  }

  // Initial state
  renderNone(content);

  return { element: container, update };
}

function renderTopLevel(parent) {
  const title = document.createElement('div');
  title.className = 'sidebar-section-title';
  title.textContent = 'Logstash Configuration';
  parent.appendChild(title);

  const desc = document.createElement('div');
  desc.className = 'sidebar-description';
  desc.textContent = 'A Logstash configuration has three sections:';
  parent.appendChild(desc);

  const sections = [
    { name: 'input', desc: 'Defines data sources (e.g., beats, file, stdin)' },
    { name: 'filter', desc: 'Transforms and enriches events (e.g., mutate, grok, date)' },
    { name: 'output', desc: 'Sends events to destinations (e.g., elasticsearch, stdout)' },
  ];

  const list = document.createElement('ul');
  list.className = 'sidebar-list';
  for (const s of sections) {
    const li = document.createElement('li');
    li.className = 'sidebar-list-item';
    const name = document.createElement('span');
    name.className = 'sidebar-item-name';
    name.textContent = s.name + ' { }';
    li.appendChild(name);
    const d = document.createElement('div');
    d.className = 'sidebar-item-desc';
    d.textContent = s.desc;
    li.appendChild(d);
    list.appendChild(li);
  }
  parent.appendChild(list);
}

function renderSection(parent, info) {
  const title = document.createElement('div');
  title.className = 'sidebar-section-title';
  title.textContent = info.sectionType + ' plugins';
  parent.appendChild(title);

  if (!info.plugins || info.plugins.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'sidebar-empty';
    empty.textContent = 'No plugins found for this section.';
    parent.appendChild(empty);
    return;
  }

  const count = document.createElement('div');
  count.className = 'sidebar-description';
  count.textContent = info.plugins.length + ' available plugin' + (info.plugins.length === 1 ? '' : 's');
  parent.appendChild(count);

  const list = document.createElement('ul');
  list.className = 'sidebar-list';
  for (const p of info.plugins) {
    const li = document.createElement('li');
    li.className = 'sidebar-list-item';
    const name = document.createElement('span');
    name.className = 'sidebar-item-name';
    name.textContent = p.name;
    li.appendChild(name);
    if (p.description) {
      const d = document.createElement('div');
      d.className = 'sidebar-item-desc';
      d.textContent = p.description;
      li.appendChild(d);
    }
    list.appendChild(li);
  }
  parent.appendChild(list);
}

function renderPlugin(parent, info) {
  const title = document.createElement('div');
  title.className = 'sidebar-section-title';
  title.textContent = info.pluginName;
  parent.appendChild(title);

  if (info.pluginDoc && info.pluginDoc.description) {
    const desc = document.createElement('div');
    desc.className = 'sidebar-description';
    desc.textContent = info.pluginDoc.description;
    parent.appendChild(desc);
  }

  const subtitle = document.createElement('div');
  subtitle.className = 'sidebar-section-title';
  subtitle.style.fontSize = '12px';
  subtitle.style.marginTop = '8px';
  subtitle.textContent = 'Options';
  parent.appendChild(subtitle);

  if (!info.options || info.options.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'sidebar-empty';
    empty.textContent = 'No option data available for this plugin.';
    parent.appendChild(empty);
    return;
  }

  const list = document.createElement('ul');
  list.className = 'sidebar-list';
  for (const opt of info.options) {
    const li = document.createElement('li');
    li.className = 'sidebar-list-item';
    if (info.optionName && opt.name === info.optionName) {
      li.classList.add('highlighted');
    }

    const nameRow = document.createElement('div');
    const name = document.createElement('span');
    name.className = 'sidebar-item-name';
    name.textContent = opt.name;
    nameRow.appendChild(name);

    if (opt.type) {
      const type = document.createElement('span');
      type.className = 'sidebar-item-type';
      type.textContent = opt.type;
      nameRow.appendChild(type);
    }

    if (opt.required) {
      const req = document.createElement('span');
      req.className = 'sidebar-item-required';
      req.textContent = 'required';
      nameRow.appendChild(req);
    }

    if (opt.default) {
      const def = document.createElement('span');
      def.className = 'sidebar-item-default';
      def.textContent = '= ' + opt.default;
      nameRow.appendChild(def);
    }

    li.appendChild(nameRow);

    if (opt.description) {
      const d = document.createElement('div');
      d.className = 'sidebar-item-desc';
      d.textContent = opt.description;
      li.appendChild(d);
    }

    list.appendChild(li);
  }
  parent.appendChild(list);
}

function renderCodec(parent, info) {
  const title = document.createElement('div');
  title.className = 'sidebar-section-title';
  title.textContent = 'Codecs';
  parent.appendChild(title);

  const desc = document.createElement('div');
  desc.className = 'sidebar-description';
  desc.textContent = 'Available codecs for encoding/decoding data:';
  parent.appendChild(desc);

  if (!info.plugins || info.plugins.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'sidebar-empty';
    empty.textContent = 'No codecs found.';
    parent.appendChild(empty);
    return;
  }

  const list = document.createElement('ul');
  list.className = 'sidebar-list';
  for (const c of info.plugins) {
    const li = document.createElement('li');
    li.className = 'sidebar-list-item';
    const name = document.createElement('span');
    name.className = 'sidebar-item-name';
    name.textContent = c.name;
    li.appendChild(name);
    if (c.description) {
      const d = document.createElement('div');
      d.className = 'sidebar-item-desc';
      d.textContent = c.description;
      li.appendChild(d);
    }
    list.appendChild(li);
  }
  parent.appendChild(list);
}

function renderNone(parent) {
  const empty = document.createElement('div');
  empty.className = 'sidebar-empty';
  empty.textContent = 'Place cursor inside a config block to see documentation.';
  parent.appendChild(empty);
}
