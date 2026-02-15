const TIMEOUT_MS = 5000;

function makeHeaders(kibanaUrl, user, pass) {
  const headers = {
    'X-Kibana-Target': kibanaUrl,
    'kbn-xsrf': 'true',
    'Content-Type': 'application/json',
  };
  if (user && pass) {
    headers['Authorization'] = 'Basic ' + btoa(user + ':' + pass);
  }
  return headers;
}

async function request(method, kibanaUrl, path, user, pass, body) {
  const res = await fetch('/kibana-api' + path, {
    method,
    headers: makeHeaders(kibanaUrl, user, pass),
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(TIMEOUT_MS),
  });

  if (!res.ok) {
    let msg;
    try {
      const data = await res.json();
      msg = data.error || data.message || res.statusText;
    } catch {
      msg = res.statusText;
    }
    if (res.status === 401) throw new Error('Authentication failed. Check username/password.');
    if (res.status === 404) throw new Error(`Not found: ${path}`);
    if (res.status === 502) throw new Error(`Cannot connect to Kibana at ${kibanaUrl}`);
    throw new Error(`Kibana API error (${res.status}): ${msg}`);
  }

  if (res.status === 204 || res.headers.get('content-length') === '0') return null;
  return res.json();
}

export async function testConnection(kibanaUrl, user, pass) {
  try {
    await request('GET', kibanaUrl, '/api/logstash/pipelines', user, pass);
    return true;
  } catch {
    return false;
  }
}

export async function listPipelines(kibanaUrl, user, pass) {
  const data = await request('GET', kibanaUrl, '/api/logstash/pipelines', user, pass);
  return data.pipelines || [];
}

export async function getPipeline(kibanaUrl, user, pass, id) {
  return request('GET', kibanaUrl, `/api/logstash/pipeline/${encodeURIComponent(id)}`, user, pass);
}

export async function savePipeline(kibanaUrl, user, pass, id, pipeline, description) {
  await request('PUT', kibanaUrl, `/api/logstash/pipeline/${encodeURIComponent(id)}`, user, pass, {
    pipeline,
    description: description || '',
    settings: {},
  });
}

export async function deletePipeline(kibanaUrl, user, pass, id) {
  await request('DELETE', kibanaUrl, `/api/logstash/pipeline/${encodeURIComponent(id)}`, user, pass);
}
