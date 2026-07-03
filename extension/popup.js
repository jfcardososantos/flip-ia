const proxyUrlInput = document.getElementById("proxyUrl");
const apiKeyInput = document.getElementById("apiKey");
const statusOutput = document.getElementById("status");
const saveConfigButton = document.getElementById("saveConfig");
const importButton = document.getElementById("importSession");
const importDeepSeekButton = document.getElementById("importDeepSeekSession");
const openXiaomiButton = document.getElementById("openXiaomi");
const openDeepSeekButton = document.getElementById("openDeepSeek");

const XIAOMI_STUDIO_URL = "https://aistudio.xiaomimimo.com/";
const DEEPSEEK_CHAT_URL = "https://chat.deepseek.com/";
const COOKIE_NAMES = ["serviceToken", "userId", "xiaomichatbot_ph"];
const COOKIE_URLS = [
  "https://aistudio.xiaomimimo.com/",
  "https://xiaomimimo.com/",
  "https://account.xiaomi.com/"
];
const DEEPSEEK_COOKIE_URLS = [
  "https://chat.deepseek.com/",
  "https://deepseek.com/"
];

function setStatus(message) {
  statusOutput.value = message;
}

function normalizeProxyUrl(url) {
  return url.trim().replace(/\/+$/, "");
}

async function loadConfig() {
  const stored = await chrome.storage.local.get(["proxyUrl", "apiKey"]);
  proxyUrlInput.value = stored.proxyUrl || "";
  apiKeyInput.value = stored.apiKey || "";
  setStatus("Configure a URL do flip-mimo-api e clique em Import Xiaomi Session. A API principal não exige token.");
}

async function saveConfig() {
  const proxyUrl = normalizeProxyUrl(proxyUrlInput.value);
  const apiKey = apiKeyInput.value.trim();

  await chrome.storage.local.set({ proxyUrl, apiKey });
  setStatus("Configuração salva.");
}

async function getCookieByName(name) {
  for (const url of COOKIE_URLS) {
    const cookie = await chrome.cookies.get({ url, name });
    if (cookie && cookie.value) {
      return cookie;
    }
  }

  const fallbackCookies = await chrome.cookies.getAll({ name });
  return fallbackCookies.find((cookie) => cookie.domain.includes("xiaomi")) || null;
}

async function collectRawCookieJar() {
  const seen = new Map();

  for (const url of COOKIE_URLS) {
    const cookies = await chrome.cookies.getAll({ url });
    for (const cookie of cookies) {
      if (!cookie || !cookie.name) {
        continue;
      }
      seen.set(cookie.name, cookie.value);
    }
  }

  const fallbackCookies = await chrome.cookies.getAll({});
  for (const cookie of fallbackCookies) {
    if (!cookie || !cookie.name) {
      continue;
    }
    if (!cookie.domain.includes("xiaomi") && !cookie.domain.includes("xiaomimimo")) {
      continue;
    }
    if (!seen.has(cookie.name)) {
      seen.set(cookie.name, cookie.value);
    }
  }

  return Array.from(seen.entries())
    .map(([name, value]) => `${name}=${value}`)
    .join("; ");
}

async function collectDeepSeekRawCookieJar() {
  const seen = new Map();

  for (const url of DEEPSEEK_COOKIE_URLS) {
    const cookies = await chrome.cookies.getAll({ url });
    for (const cookie of cookies) {
      if (cookie && cookie.name) {
        seen.set(cookie.name, cookie.value);
      }
    }
  }

  const fallbackCookies = await chrome.cookies.getAll({});
  for (const cookie of fallbackCookies) {
    if (!cookie || !cookie.name || !cookie.domain.includes("deepseek")) {
      continue;
    }
    if (!seen.has(cookie.name)) {
      seen.set(cookie.name, cookie.value);
    }
  }

  return Array.from(seen.entries())
    .map(([name, value]) => `${name}=${value}`)
    .join("; ");
}

function normalizeDeepSeekToken(raw) {
  if (!raw) {
    return "";
  }
  try {
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed.value === "string") {
      return parsed.value;
    }
  } catch (error) {
    // Keep raw value when it is not JSON.
  }
  return String(raw).trim();
}

async function getDeepSeekUserToken() {
  const tabs = await chrome.tabs.query({ url: "https://chat.deepseek.com/*" });
  const tab = tabs.find((item) => item.id);
  if (!tab) {
    throw new Error("Abra https://chat.deepseek.com logado antes de importar.");
  }

  const [result] = await chrome.scripting.executeScript({
    target: { tabId: tab.id },
    func: () => localStorage.getItem("userToken") || ""
  });

  const token = normalizeDeepSeekToken(result && result.result);
  if (!token) {
    throw new Error("Não encontrei localStorage.userToken na aba do DeepSeek.");
  }
  return token;
}

async function collectSession() {
  const found = {};

  for (const name of COOKIE_NAMES) {
    const cookie = await getCookieByName(name);
    if (!cookie || !cookie.value) {
      throw new Error(`Missing Xiaomi cookie: ${name}`);
    }
    found[name] = cookie.value;
  }

  const rawCookie = await collectRawCookieJar();
  if (!rawCookie) {
    throw new Error("Could not build Xiaomi raw cookie jar");
  }

  return {
    serviceToken: found.serviceToken,
    userId: found.userId,
    xiaomichatbotPh: found.xiaomichatbot_ph,
    rawCookie
  };
}

async function importSession() {
  const proxyUrl = normalizeProxyUrl(proxyUrlInput.value);
  const apiKey = apiKeyInput.value.trim();

  if (!proxyUrl) {
    setStatus("Informe a URL do proxy antes de importar.");
    return;
  }

  setStatus("Lendo cookies da Xiaomi...");

  try {
    const session = await collectSession();
    setStatus("Cookies encontrados. Enviando sessão ao proxy...");

    const headers = {
      "Content-Type": "application/json"
    };
    if (apiKey) {
      headers.Authorization = `Bearer ${apiKey}`;
    }

    const response = await fetch(`${proxyUrl}/auth/extension/import`, {
      method: "POST",
      headers,
      body: JSON.stringify({
        ...session,
        source: "chrome-extension"
      })
    });

    const bodyText = await response.text();
    let prettyBody = bodyText;
    try {
      prettyBody = JSON.stringify(JSON.parse(bodyText), null, 2);
    } catch (error) {
      // Keep original text when response is not JSON.
    }

    setStatus(`HTTP ${response.status} ${response.statusText}\n\n${prettyBody}`);
  } catch (error) {
    setStatus(`Falha ao importar a sessão.\n\n${error.message || String(error)}`);
  }
}

async function importDeepSeekSession() {
  const proxyUrl = normalizeProxyUrl(proxyUrlInput.value);
  const apiKey = apiKeyInput.value.trim();

  if (!proxyUrl) {
    setStatus("Informe a URL do proxy antes de importar.");
    return;
  }

  setStatus("Lendo cookies e userToken do DeepSeek...");

  try {
    const [rawCookie, userToken] = await Promise.all([
      collectDeepSeekRawCookieJar(),
      getDeepSeekUserToken()
    ]);

    if (!rawCookie) {
      throw new Error("Could not build DeepSeek raw cookie jar");
    }

    setStatus("Credenciais DeepSeek encontradas. Enviando sessão ao proxy...");

    const headers = {
      "Content-Type": "application/json"
    };
    if (apiKey) {
      headers.Authorization = `Bearer ${apiKey}`;
    }

    const response = await fetch(`${proxyUrl}/auth/deepseek/extension/import`, {
      method: "POST",
      headers,
      body: JSON.stringify({
        userToken,
        rawCookie,
        source: "chrome-extension"
      })
    });

    const bodyText = await response.text();
    let prettyBody = bodyText;
    try {
      prettyBody = JSON.stringify(JSON.parse(bodyText), null, 2);
    } catch (error) {
      // Keep original text when response is not JSON.
    }

    setStatus(`HTTP ${response.status} ${response.statusText}\n\n${prettyBody}`);
  } catch (error) {
    setStatus(`Falha ao importar a sessão DeepSeek.\n\n${error.message || String(error)}`);
  }
}

saveConfigButton.addEventListener("click", saveConfig);
importButton.addEventListener("click", async () => {
  await saveConfig();
  await importSession();
});
importDeepSeekButton.addEventListener("click", async () => {
  await saveConfig();
  await importDeepSeekSession();
});
openXiaomiButton.addEventListener("click", () => {
  chrome.tabs.create({ url: XIAOMI_STUDIO_URL });
});
openDeepSeekButton.addEventListener("click", () => {
  chrome.tabs.create({ url: DEEPSEEK_CHAT_URL });
});

loadConfig();
