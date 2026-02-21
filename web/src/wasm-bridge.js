let wasmReady = false;
let readyResolve;
const readyPromise = new Promise((resolve) => { readyResolve = resolve; });

export async function initWasm() {
  const go = new Go();
  const result = await WebAssembly.instantiateStreaming(
    fetch('/parser.wasm'),
    go.importObject
  );
  go.run(result.instance); // non-blocking (Go blocks on select{})
  wasmReady = true;
  readyResolve();
}

export async function parseLogstash(source) {
  if (!wasmReady) await readyPromise;
  const jsonStr = window.parseLogstashConfig(source);
  return JSON.parse(jsonStr);
}

export async function getVersions() {
  if (!wasmReady) await readyPromise;
  const jsonStr = window.getLogstashVersions();
  return JSON.parse(jsonStr);
}

export async function setVersion(version) {
  if (!wasmReady) await readyPromise;
  const jsonStr = window.setLogstashVersion(version);
  const result = JSON.parse(jsonStr);
  if (!result.ok) {
    throw new Error(result.error);
  }
  return result;
}
