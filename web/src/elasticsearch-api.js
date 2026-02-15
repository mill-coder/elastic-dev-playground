const TIMEOUT_MS = 10000;
const BULK_TIMEOUT_MS = 60000;

function makeHeaders(esUrl, user, pass) {
  const headers = {
    'X-ES-Target': esUrl,
    'Content-Type': 'application/json',
  };
  if (user && pass) {
    headers['Authorization'] = 'Basic ' + btoa(user + ':' + pass);
  }
  return headers;
}

async function request(method, esUrl, path, user, pass, body, timeoutMs, options) {
  const timeout = timeoutMs || TIMEOUT_MS;
  const hdrs = makeHeaders(esUrl, user, pass);
  if (options && options.contentType) {
    hdrs['Content-Type'] = options.contentType;
  }

  let fetchBody;
  if (options && options.rawBody) {
    fetchBody = body;
  } else if (body) {
    fetchBody = JSON.stringify(body);
  }

  const fetchOptions = {
    method,
    headers: hdrs,
    body: fetchBody,
  };
  if (options && options.signal) {
    fetchOptions.signal = options.signal;
  } else {
    fetchOptions.signal = AbortSignal.timeout(timeout);
  }

  const res = await fetch('/es-api' + path, fetchOptions);

  if (!res.ok) {
    let msg;
    try {
      const data = await res.json();
      msg = data.error?.reason || data.error?.type || data.error || data.message || res.statusText;
    } catch {
      msg = res.statusText;
    }
    if (res.status === 401) throw new Error('Authentication failed. Check username/password.');
    if (res.status === 502) throw new Error(`Cannot connect to Elasticsearch at ${esUrl}`);
    throw new Error(`ES API error (${res.status}): ${msg}`);
  }

  if (res.status === 204 || res.headers.get('content-length') === '0') return null;
  return res.json();
}

export async function testConnection(esUrl, user, pass) {
  const data = await request('GET', esUrl, '/', user, pass);
  return {
    name: data.cluster_name || data.name || 'unknown',
    version: data.version?.number || 'unknown',
  };
}

export async function listIndices(esUrl, user, pass) {
  const data = await request('GET', esUrl, '/_cat/indices?format=json&h=index,docs.count,store.size,health,status', user, pass);
  return (data || []).filter(idx => !idx.index.startsWith('.'));
}

export async function listDataStreams(esUrl, user, pass) {
  const data = await request('GET', esUrl, '/_data_stream', user, pass);
  return (data.data_streams || []).filter(ds => !ds.name.startsWith('.'));
}

export async function getIndexMapping(esUrl, user, pass, index) {
  const data = await request('GET', esUrl, `/${encodeURIComponent(index)}/_mapping`, user, pass);
  const fields = [];
  function walk(properties, prefix) {
    for (const [name, def] of Object.entries(properties)) {
      const fullName = prefix ? `${prefix}.${name}` : name;
      if (def.type) {
        fields.push({ name: fullName, type: def.type });
      }
      if (def.properties) {
        walk(def.properties, fullName);
      }
    }
  }
  for (const indexData of Object.values(data)) {
    const props = indexData.mappings?.properties;
    if (props) walk(props, '');
  }
  return fields;
}

export async function getDocCount(esUrl, user, pass, index, query) {
  const body = query ? { query } : { query: { match_all: {} } };
  const data = await request('POST', esUrl, `/${encodeURIComponent(index)}/_count`, user, pass, body);
  return data.count;
}

export async function startScroll(esUrl, user, pass, index, query, size) {
  const body = {
    query: query || { match_all: {} },
    size: size || 1000,
  };
  return request('POST', esUrl, `/${encodeURIComponent(index)}/_search?scroll=2m`, user, pass, body, BULK_TIMEOUT_MS);
}

export async function continueScroll(esUrl, user, pass, scrollId, signal) {
  return request('POST', esUrl, '/_search/scroll', user, pass, {
    scroll: '2m',
    scroll_id: scrollId,
  }, BULK_TIMEOUT_MS, { signal });
}

export async function clearScroll(esUrl, user, pass, scrollId) {
  try {
    await request('DELETE', esUrl, '/_search/scroll', user, pass, { scroll_id: scrollId });
  } catch {
    // fire-and-forget
  }
}

export async function bulkIndex(esUrl, user, pass, index, docs, signal) {
  let ndjson = '';
  for (const doc of docs) {
    ndjson += JSON.stringify({ index: { _index: index } }) + '\n';
    ndjson += JSON.stringify(doc) + '\n';
  }
  return request('POST', esUrl, '/_bulk', user, pass, ndjson, BULK_TIMEOUT_MS, {
    contentType: 'application/x-ndjson',
    rawBody: true,
    signal,
  });
}

export function buildQuery(startDate, endDate, timestampField, filters) {
  const must = [];
  const mustNot = [];
  const tsField = timestampField || '@timestamp';

  if (startDate || endDate) {
    const range = {};
    if (startDate) range.gte = startDate;
    if (endDate) range.lte = endDate;
    must.push({ range: { [tsField]: range } });
  }

  if (filters && filters.length > 0) {
    for (const f of filters) {
      if (!f.field || !f.operator) continue;
      if (f.operator === '=') {
        must.push({ term: { [f.field]: f.value } });
      } else if (f.operator === 'IN') {
        must.push({ terms: { [f.field]: f.values || [] } });
      } else if (f.operator === 'NOT =') {
        mustNot.push({ term: { [f.field]: f.value } });
      } else if (f.operator === 'NOT IN') {
        mustNot.push({ terms: { [f.field]: f.values || [] } });
      }
    }
  }

  if (must.length === 0 && mustNot.length === 0) {
    return { match_all: {} };
  }

  const bool = {};
  if (must.length > 0) bool.must = must;
  if (mustNot.length > 0) bool.must_not = mustNot;
  return { bool };
}
