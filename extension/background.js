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
  await installQwenRequestCapture(tab.id);
  return tab;
}

async function installQwenRequestCapture(tabId) {
  await chrome.scripting.executeScript({
    target: { tabId },
    world: "MAIN",
    func: () => {
      if (window.__flipAiQwenCaptureInstalled) return;
      window.__flipAiQwenCaptureInstalled = true;
      const storageKey = "__flip_ai_qwen_request_template_v1";
      const isCompletion = (value) => String(value || "").includes("/chat/completions");
      const saveTemplate = (rawBody) => {
        if (typeof rawBody !== "string" || !rawBody.trim()) return;
        try {
          const template = JSON.parse(rawBody);
          template.chatId = "";
          template.chat_id = "";
          template.parentId = "";
          template.parent_id = null;
          template.timestamp = 0;
          if (Array.isArray(template.messages)) {
            template.messages = template.messages.slice(0, 1).map((message) => ({
              ...message,
              id: null,
              fid: "",
              parentId: null,
              parent_id: null,
              content: "",
              files: [],
              timestamp: 0
            }));
          }
          localStorage.setItem(storageKey, JSON.stringify(template));
        } catch (_error) {
          // Ignore non-JSON requests.
        }
      };

      const pageFetch = window.fetch;
      window.fetch = async function(input, init) {
        const url = typeof input === "string" ? input : input && input.url;
        const rawBody = init && init.body;
        const response = await pageFetch.apply(this, arguments);
        if (isCompletion(url) && response && response.ok) saveTemplate(rawBody);
        return response;
      };

      const xhr = window.XMLHttpRequest && window.XMLHttpRequest.prototype;
      if (xhr) {
        const pageOpen = xhr.open;
        const pageSend = xhr.send;
        xhr.open = function(method, url) {
          this.__flipAiQwenURL = url;
          return pageOpen.apply(this, arguments);
        };
        xhr.send = function(body) {
          if (isCompletion(this.__flipAiQwenURL)) {
            this.addEventListener("loadend", () => {
              if (this.status >= 200 && this.status < 300) saveTemplate(body);
            }, { once: true });
          }
          return pageSend.apply(this, arguments);
        };
      }
    }
  });
}

async function executeQwenJob(job) {
  const tab = await qwenTab();
  const [execution] = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    world: "MAIN",
    args: [job],
    func: async (relayJob) => {
      const sleep = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));
      const hasTerminalSignal = (text) => {
        if (text.includes("data: [DONE]")) return true;
        const inspect = (value) => {
          if (!value || typeof value !== "object") return false;
          if (value.done === true || value.finished === true || value.is_finished === true) return true;
          if (typeof value.finish_reason === "string" && value.finish_reason) return true;
          if (value["response.completed"] || value["response.finished"] || value["message.finished"]) return true;
          return Object.values(value).some(inspect);
        };
        const frames = text.split(/\r?\n\r?\n/);
        for (const frame of frames) {
          const data = frame.split(/\r?\n/).filter((line) => line.startsWith("data:"))
            .map((line) => line.slice(5).trim()).join("\n");
          if (!data || data === "[DONE]") continue;
          try {
            if (inspect(JSON.parse(data))) return true;
          } catch (_error) {
            // Wait for a complete SSE frame.
          }
        }
        return false;
      };
      const protectedQwenPaths = [
        "/api/chat/completions", "/api/chats/new", "/api/chat/completed",
        "/api/v1/chats", "/api/task/suggestions/completions",
        "/api/v2/chats", "/api/v2/chat/completions",
        "/api/v2/task/suggestions/completions", "/api/v2/files"
      ];
      const hasQwenUidToken = () => {
        try {
          const fy = window.__baxia__ && window.__baxia__.getFYModule;
          return Boolean(fy && typeof fy.getUidToken === "function" && fy.getUidToken());
        } catch (_error) {
          return false;
        }
      };
      for (let attempt = 0; attempt < 120; attempt += 1) {
        const baxia = window.__baxia__;
        const fy = baxia && baxia.getFYModule;
        if (window.baxiaCommon && baxia && baxia.baxiaPromptInit && !window.baxiaInitialized && !window.baxiaNeedDelay) {
          try {
            window.baxiaCommon.init({
              appendTo: "header",
              uabOptions: { location: "sea" },
              checkApiPath: (url) => protectedQwenPaths.some((path) => String(url || "").includes(path)),
              showCallback: () => {},
              hideCallback: () => {},
              paramstype: ["uab", "umid"],
              autoSize: true
            });
            window.baxiaInitialized = true;
          } catch (_error) {
            // The official page may be initializing the same module concurrently.
          }
        }
        let uidToken = "";
        try {
          if (fy && typeof fy.getUidToken === "function") uidToken = fy.getUidToken() || "";
        } catch (_error) {}
        if (window.baxiaInitialized && uidToken) break;
        await sleep(250);
      }
      let requestBody = relayJob.body || undefined;
      let templateApplied = false;
      if (String(relayJob.url || "").includes("/chat/completions") && requestBody) {
        try {
          const incoming = JSON.parse(requestBody);
          const template = JSON.parse(localStorage.getItem("__flip_ai_qwen_request_template_v1") || "null");
          if (template && typeof template === "object" && Array.isArray(template.messages) && template.messages[0]) {
            const merged = structuredClone(template);
            for (const key of ["stream", "version", "incremental_output", "chatId", "parentId", "chat_id", "chat_mode", "model", "parent_id", "timestamp"]) {
              if (Object.prototype.hasOwnProperty.call(merged, key) && Object.prototype.hasOwnProperty.call(incoming, key)) {
                merged[key] = incoming[key];
              }
            }
            const sourceMessage = Array.isArray(incoming.messages) ? incoming.messages[0] : null;
            const targetMessage = merged.messages[0];
            if (sourceMessage) {
              for (const key of ["id", "fid", "parentId", "parent_id", "childrenIds", "role", "content", "user_action", "files", "timestamp", "models", "model", "chat_type", "sub_chat_type"]) {
                if (Object.prototype.hasOwnProperty.call(targetMessage, key) && Object.prototype.hasOwnProperty.call(sourceMessage, key)) {
                  targetMessage[key] = sourceMessage[key];
                }
              }
            }
            requestBody = JSON.stringify(merged);
            templateApplied = true;
          }
        } catch (_error) {
          // Fall back to the adapter payload until a valid official template exists.
        }
      }
      const requestOptions = {
        method: relayJob.method || "POST",
        credentials: "include",
        headers: relayJob.headers || {},
        body: requestBody
      };
      const observedHeaders = {};
      const headerValue = (name) => {
        const match = Object.keys(observedHeaders).find((key) => key.toLowerCase() === name);
        return match ? observedHeaders[match] : "";
      };
      const requestDebug = (stage, error = "") => ({
        stage,
        error,
        transport: "xhr",
        baxiaReady: Boolean(window.__baxia__ && window.__baxia__.baxiaPromptInit),
        baxiaInitialized: Boolean(window.baxiaInitialized),
        uidTokenReady: hasQwenUidToken(),
        bxUA: Boolean(headerValue("bx-ua")),
        bxUmid: Boolean(headerValue("bx-umidtoken")),
        bxVersion: Boolean(headerValue("bx-v")),
        templateApplied,
        templateTopLevelKeys: templateApplied ? Object.keys(JSON.parse(requestBody)).sort() : [],
        templateMessageKeys: templateApplied ? Object.keys(JSON.parse(requestBody).messages[0] || {}).sort() : []
      });
      const isCompletionRequest = String(relayJob.url || "").includes("/chat/completions");
      // Baxia caches the selected transport per exact URL. A previous fetch for
      // the same endpoint makes its XHR hook intentionally skip signing it.
      // Relay jobs use XHR, so discard only this endpoint's transport decision.
      if (window.__baxia__ && window.__baxia__.handleEffectUrl) {
        delete window.__baxia__.handleEffectUrl[String(relayJob.url || "") + "fetch"];
        delete window.__baxia__.handleEffectUrl[String(relayJob.url || "") + "xhr"];
      }
      const xhrResult = await new Promise((resolve) => {
        const xhr = new XMLHttpRequest();
        let settled = false;
        let lastProgressAt = Date.now();
        const startedAt = Date.now();
        let interval;
        let deadline;
        const parseHeaders = () => {
          const headers = {};
          try {
            for (const line of String(xhr.getAllResponseHeaders() || "").trim().split(/\r?\n/)) {
              const separator = line.indexOf(":");
              if (separator > 0) headers[line.slice(0, separator).trim()] = line.slice(separator + 1).trim();
            }
          } catch (_error) {
            // Headers may be unavailable before the response starts.
          }
          return headers;
        };
        const finish = (forcedStatus = 0, error = "") => {
          if (settled) return;
          settled = true;
          clearInterval(interval);
          clearTimeout(deadline);
          let body = "";
          try { body = String(xhr.responseText || ""); } catch (_error) {}
          const result = {
            status: forcedStatus || xhr.status || 598,
            headers: parseHeaders(),
            body,
            error
          };
          resolve(result);
          if (xhr.readyState !== XMLHttpRequest.DONE) xhr.abort();
        };
        try {
          xhr.open(requestOptions.method, relayJob.url, true);
          xhr.withCredentials = true;
          const pageSetRequestHeader = xhr.setRequestHeader.bind(xhr);
          xhr.setRequestHeader = (key, value) => {
            observedHeaders[key] = value;
            return pageSetRequestHeader(key, value);
          };
          for (const [key, value] of Object.entries(requestOptions.headers || {})) {
            xhr.setRequestHeader(key, value);
          }
          xhr.onprogress = () => {
            lastProgressAt = Date.now();
            if (isCompletionRequest) {
              let text = "";
              try { text = String(xhr.responseText || ""); } catch (_error) {}
              if (hasTerminalSignal(text)) finish();
            }
          };
          xhr.onload = () => finish();
          xhr.onerror = () => finish(598, "Qwen XHR network error");
          xhr.onabort = () => finish(598, "Qwen XHR aborted");
          interval = setInterval(() => {
            if (!isCompletionRequest || settled) return;
            let text = "";
            try { text = String(xhr.responseText || ""); } catch (_error) {}
            const hasAnswer = /["']phase["']\s*:\s*["'](?:answer|slides)["']/.test(text);
            const idleLimit = hasAnswer ? 4000 : text ? 45000 : 55000;
            if (text && Date.now() - lastProgressAt >= idleLimit) finish();
            if (Date.now() - startedAt >= 65000) finish(text ? 0 : 598, text ? "" : "Qwen did not start an XHR response within 65s");
          }, 500);
          deadline = setTimeout(() => {
            finish(598, `Qwen did not complete the XHR request within ${isCompletionRequest ? 65 : 30}s`);
          }, isCompletionRequest ? 65000 : 30000);
          xhr.send(requestBody);
        } catch (error) {
          finish(598, error && error.message ? error.message : String(error));
        }
      });
      return {
        status: xhrResult.status,
        headers: xhrResult.headers,
        body: xhrResult.body,
        debug: requestDebug(xhrResult.status === 598 ? "xhr" : "response", xhrResult.error)
      };
    }
  });
  if (!execution) throw new Error("A aba do Qwen não retornou o resultado da chamada.");
  if (execution.error) throw new Error(execution.error.message || String(execution.error));
  if (execution.result && String(execution.result.body || "").includes("aliyun_waf_")) {
    await chrome.tabs.update(tab.id, { active: true }).catch(() => {});
    if (tab.windowId) await chrome.windows.update(tab.windowId, { focused: true }).catch(() => {});
  }
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
  let resetSent = false;
  while (generation === relayGeneration) {
    const config = await relayConfig();
    if (!config.proxyUrl) {
      await delay(10000);
      continue;
    }
    try {
      if (!resetSent) {
        const resetResponse = await fetch(`${config.proxyUrl}/auth/qwen/relay/reset`, {
          method: "POST",
          headers: adminHeaders(config.apiKey, true),
          body: "{}"
        });
        if (!resetResponse.ok) throw new Error(`Falha ao reiniciar a ponte Qwen: HTTP ${resetResponse.status}`);
        resetSent = true;
      }
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
  void qwenTab().catch(() => {});
  void runRelayLoop(relayGeneration);
}

chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (changeInfo.status === "complete" && tab.url && (tab.url.startsWith("https://chat.qwen.ai/") || tab.url.startsWith("https://chat.qwenlm.ai/"))) {
    void installQwenRequestCapture(tabId).catch(() => {});
  }
});

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
