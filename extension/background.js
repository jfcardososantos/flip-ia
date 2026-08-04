const QWEN_URL = "https://chat.qwen.ai/";
const QWEN_TAB_PATTERNS = ["https://chat.qwen.ai/*", "https://chat.qwenlm.ai/*"];

let relayGeneration = 0;

const delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));

function normalizeProxyUrl(value) {
  return String(value || "").trim().replace(/\/+$/, "");
}

function adminHeaders(apiKey, json = false) {
  const headers = {};
  if (json) headers["Content-Type"] = "application/json";
  if (apiKey) headers.Authorization = `Bearer ${apiKey}`;
  return headers;
}

async function relayConfig() {
  const stored = await chrome.storage.local.get(["proxyUrl", "apiKey"]);
  return {
    proxyUrl: normalizeProxyUrl(stored.proxyUrl),
    apiKey: String(stored.apiKey || "").trim()
  };
}

async function waitForTab(tabId, timeoutMs = 30000) {
  const current = await chrome.tabs.get(tabId);
  if (current.status === "complete") return;
  await new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(listener);
      reject(new Error("A aba do Qwen não terminou de carregar."));
    }, timeoutMs);
    const listener = (updatedTabId, changeInfo) => {
      if (updatedTabId !== tabId || changeInfo.status !== "complete") return;
      clearTimeout(timeout);
      chrome.tabs.onUpdated.removeListener(listener);
      resolve();
    };
    chrome.tabs.onUpdated.addListener(listener);
  });
}

async function qwenTab() {
  let tabs = [];
  for (const pattern of QWEN_TAB_PATTERNS) {
    tabs = tabs.concat(await chrome.tabs.query({ url: pattern }));
  }
  let tab = tabs.find((item) => item.id);
  if (!tab) {
    tab = await chrome.tabs.create({ url: QWEN_URL, active: false });
  }
  if (!tab || !tab.id) throw new Error("Não foi possível abrir a aba autenticada do Qwen.");
  await waitForTab(tab.id);
  return tab;
}

async function executeQwenJob(job) {
  const tab = await qwenTab();
  const [execution] = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    world: "MAIN",
    args: [job],
    func: async (relayJob) => {
      const sleep = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));
      for (let attempt = 0; attempt < 80; attempt += 1) {
        const baxia = window.__baxia__;
        const fy = baxia && baxia.getFYModule;
        if (baxia && baxia.baxiaPromptInit && fy && typeof fy.getUidToken === "function") break;
        await sleep(250);
      }
      const requestOptions = {
        method: relayJob.method || "POST",
        credentials: "include",
        headers: relayJob.headers || {},
        body: relayJob.body || undefined
      };
      const response = await window.fetch(relayJob.url, requestOptions);
      const responseHeaders = {};
      response.headers.forEach((value, key) => {
        responseHeaders[key] = value;
      });
      return {
        status: response.status,
        headers: responseHeaders,
        body: await response.text(),
        debug: {
          baxiaReady: Boolean(window.__baxia__ && window.__baxia__.baxiaPromptInit),
          bxUA: Boolean(requestOptions.headers && (requestOptions.headers["bx-ua"] || requestOptions.headers.get && requestOptions.headers.get("bx-ua"))),
          bxUmid: Boolean(requestOptions.headers && (requestOptions.headers["bx-umidtoken"] || requestOptions.headers.get && requestOptions.headers.get("bx-umidtoken"))),
          bxVersion: Boolean(requestOptions.headers && (requestOptions.headers["bx-v"] || requestOptions.headers.get && requestOptions.headers.get("bx-v")))
        }
      };
    }
  });
  if (!execution) throw new Error("A aba do Qwen não retornou o resultado da chamada.");
  if (execution.error) throw new Error(execution.error.message || String(execution.error));
  return execution.result;
}

async function submitRelayResult(config, payload) {
  const response = await fetch(`${config.proxyUrl}/auth/qwen/relay/result`, {
    method: "POST",
    headers: adminHeaders(config.apiKey, true),
    body: JSON.stringify(payload)
  });
  if (!response.ok && response.status !== 409) {
    throw new Error(`O proxy recusou o resultado Qwen: HTTP ${response.status}`);
  }
}

async function handleRelayJob(config, job) {
  let payload;
  try {
    const result = await executeQwenJob(job);
    if (result.status < 200 || result.status >= 300) {
      throw new Error(`Qwen HTTP ${result.status}; Baxia=${JSON.stringify(result.debug || {})}; body=${String(result.body || "").slice(0, 500)}`);
    }
    payload = {
      job_id: job.id,
      status: result.status,
      headers: result.headers || {},
      body: result.body || ""
    };
  } catch (error) {
    payload = {
      job_id: job.id,
      status: 0,
      headers: {},
      body: "",
      error: error && error.message ? error.message : String(error)
    };
  }
  await submitRelayResult(config, payload);
}

async function runRelayLoop(generation) {
  while (generation === relayGeneration) {
    const config = await relayConfig();
    if (!config.proxyUrl) {
      await delay(10000);
      continue;
    }
    try {
      const response = await fetch(`${config.proxyUrl}/auth/qwen/relay/next`, {
        method: "GET",
        headers: adminHeaders(config.apiKey),
        cache: "no-store"
      });
      if (response.status === 204) continue;
      if (!response.ok) {
        await chrome.storage.local.set({ qwenRelayError: `HTTP ${response.status}`, qwenRelaySeenAt: Date.now() });
        await delay(5000);
        continue;
      }
      const job = await response.json();
      await chrome.storage.local.set({ qwenRelayError: "", qwenRelaySeenAt: Date.now() });
      await handleRelayJob(config, job);
    } catch (error) {
      await chrome.storage.local.set({
        qwenRelayError: error && error.message ? error.message : String(error),
        qwenRelaySeenAt: Date.now()
      });
      await delay(3000);
    }
  }
}

function restartRelay() {
  relayGeneration += 1;
  void runRelayLoop(relayGeneration);
}

chrome.runtime.onInstalled.addListener(restartRelay);
chrome.runtime.onStartup.addListener(restartRelay);
chrome.storage.onChanged.addListener((changes, area) => {
  if (area === "local" && (changes.proxyUrl || changes.apiKey)) restartRelay();
});
chrome.runtime.onMessage.addListener((message) => {
  if (message && message.type === "restart-qwen-relay") restartRelay();
});
chrome.alarms.create("qwen-relay-keepalive", { periodInMinutes: 0.5 });
chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === "qwen-relay-keepalive" && relayGeneration === 0) restartRelay();
});

restartRelay();
