const chatEl = document.getElementById("chat");
const welcomeEl = document.getElementById("welcome");
const formEl = document.getElementById("chatForm");
const inputEl = document.getElementById("messageInput");
const sendBtn = document.getElementById("sendBtn");
const stopBtn = document.getElementById("stopBtn");
const clearBtn = document.getElementById("clearBtn");
const settingsBtn = document.getElementById("settingsBtn");
const settingsModal = document.getElementById("settingsModal");
const saveSettingsBtn = document.getElementById("saveSettingsBtn");
const temperatureInput = document.getElementById("temperatureInput");
const tempValue = document.getElementById("tempValue");
const tempBadge = document.getElementById("tempBadge");
const compareToggle = document.getElementById("compareToggle");
const composerHint = document.getElementById("composerHint");
const layoutEl = document.getElementById("layout");
const debugToggle = document.getElementById("debugToggle");
const debugPanel = document.getElementById("debugPanel");
const debugLog = document.getElementById("debugLog");
const clearDebugBtn = document.getElementById("clearDebugBtn");

const DEBUG_STORAGE_KEY = "deepseek-chat-debug";
const SETTINGS_STORAGE_KEY = "deepseek-chat-settings-day4";
const COMPARE_STORAGE_KEY = "deepseek-chat-compare";
const DEFAULT_TEMPERATURE = 1.0;
const COMPARE_TEMPS = [0.1, 1.0, 1.9];

const HINT_NORMAL = "Enter — отправить · Shift+Enter — новая строка · Стоп — прервать генерацию";
const HINT_COMPARE = "Режим сравнения: temp 0.1 → 1.0 → 1.9 → анализ · Стоп — прервать";

/** @type {{role: string, content: string}[]} */
let history = [];
let isLoading = false;
let debugEnabled = localStorage.getItem(DEBUG_STORAGE_KEY) === "true";
let compareEnabled = localStorage.getItem(COMPARE_STORAGE_KEY) === "true";
let debugEntryCount = 0;
/** @type {AbortController | null} */
let activeAbort = null;

let settings = loadSettings();

function clampTemperature(value) {
  const n = Number(value);
  if (Number.isNaN(n)) return DEFAULT_TEMPERATURE;
  return Math.min(2, Math.max(0, Math.round(n * 10) / 10));
}

function formatTemp(value) {
  return clampTemperature(value).toFixed(1);
}

function loadSettings() {
  try {
    const raw = localStorage.getItem(SETTINGS_STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      return { temperature: clampTemperature(parsed.temperature) };
    }
  } catch {
    /* ignore */
  }
  return { temperature: DEFAULT_TEMPERATURE };
}

function saveSettings(next) {
  settings = next;
  localStorage.setItem(SETTINGS_STORAGE_KEY, JSON.stringify(settings));
  updateTempBadge();
}

function updateTempBadge() {
  tempBadge.textContent = compareEnabled ? "сравнение" : formatTemp(settings.temperature);
}

function syncTempUI(value) {
  const t = clampTemperature(value);
  temperatureInput.value = String(t);
  temperatureInput.setAttribute("aria-valuenow", String(t));
  tempValue.textContent = formatTemp(t);
}

function setCompareMode(enabled) {
  compareEnabled = enabled;
  localStorage.setItem(COMPARE_STORAGE_KEY, enabled ? "true" : "false");
  compareToggle.checked = enabled;
  composerHint.textContent = enabled ? HINT_COMPARE : HINT_NORMAL;
  updateTempBadge();
}

function hideWelcome() {
  if (welcomeEl) welcomeEl.style.display = "none";
}

function scrollToBottom() {
  chatEl.scrollTop = chatEl.scrollHeight;
}

function createMessage(role, content, extraClass = "", label = null) {
  hideWelcome();

  const isUser = role === "user";
  const el = document.createElement("div");
  el.className = `message message--${role} ${extraClass}`.trim();

  const labelHTML = label
    ? `<span class="message__label${label.analysis ? " message__label--analysis" : ""}">${escapeHTML(label.text)}</span>`
    : "";

  el.innerHTML = `
    <div class="message__avatar">${isUser ? "Вы" : "AI"}</div>
    <div class="message__col">
      ${labelHTML}
      <div class="message__bubble"></div>
    </div>
  `;
  el.querySelector(".message__bubble").textContent = content;
  chatEl.appendChild(el);
  scrollToBottom();
  return el;
}

function setBubbleText(messageEl, text) {
  const bubble = messageEl.querySelector(".message__bubble");
  if (bubble) bubble.textContent = text;
  scrollToBottom();
}

function setLoading(loading) {
  isLoading = loading;
  sendBtn.disabled = loading;
  inputEl.disabled = loading;
  stopBtn.hidden = !loading;
  compareToggle.disabled = loading;
}

function autoResizeTextarea() {
  inputEl.style.height = "auto";
  inputEl.style.height = Math.min(inputEl.scrollHeight, 160) + "px";
}

function formatJSON(value) {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function setDebugMode(enabled) {
  debugEnabled = enabled;
  localStorage.setItem(DEBUG_STORAGE_KEY, enabled ? "true" : "false");
  debugToggle.checked = enabled;
  layoutEl.classList.toggle("layout--debug", enabled);
  debugPanel.setAttribute("aria-hidden", enabled ? "false" : "true");
}

function clearDebugLog() {
  debugEntryCount = 0;
  debugLog.innerHTML = '<p class="debug-panel__empty">Запросы и ответы появятся здесь после отправки сообщения.</p>';
}

function appendDebugBlock(title, payload, extraClass = "") {
  const pre = document.createElement("pre");
  pre.className = `debug-block__code ${extraClass}`.trim();
  pre.textContent = formatJSON(payload);
  return `
    <div class="debug-block">
      <div class="debug-block__title">${escapeHTML(title)}</div>
      ${pre.outerHTML}
    </div>
  `;
}

function escapeHTML(text) {
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
}

function logExchange({ requestBody, status, responseBody, durationMs, upstream, url = "/api/chat/stream" }) {
  if (!debugEnabled) return;

  if (debugEntryCount === 0) {
    debugLog.innerHTML = "";
  }
  debugEntryCount += 1;

  const entry = document.createElement("article");
  entry.className = "debug-entry";
  entry.innerHTML = `
    <header class="debug-entry__header">
      <span class="debug-entry__id">#${debugEntryCount}</span>
      <time class="debug-entry__time">${new Date().toLocaleTimeString("ru-RU")}</time>
      <span class="debug-entry__status debug-entry__status--${status >= 200 && status < 300 ? "ok" : "err"}">${status}</span>
      <span class="debug-entry__duration">${durationMs} ms</span>
    </header>
    ${appendDebugBlock(`→ Запрос к ${url}`, {
      method: "POST",
      url,
      headers: { "Content-Type": "application/json", "X-Debug": debugEnabled ? "true" : undefined },
      body: requestBody,
    })}
    ${appendDebugBlock("← Ответ stream", responseBody, status >= 400 ? "debug-block__code--error" : "")}
    ${upstream ? appendDebugBlock("↔ DeepSeek API", upstream) : ""}
  `;

  debugLog.prepend(entry);
}

function buildRequestBody(text, temperature) {
  const body = {
    message: text,
    history: [],
  };
  if (temperature !== null && temperature !== undefined) {
    body.temperature = temperature;
  }
  return body;
}

function buildAnalysisPrompt(question, replies) {
  const blocks = replies
    .map((r) => `### Ответ при temperature=${formatTemp(r.temperature)}\n${r.reply || "(пустой или ошибка)"}`)
    .join("\n\n");

  return `Сравни три ответа на один и тот же вопрос. Ответы получены при разной температуре генерации одной и той же модели.

Вопрос пользователя:
${question}

${blocks}

Сделай сравнительный анализ:
1. Чем отличаются стиль, структура и тон.
2. Где больше точности и предсказуемости, а где креатива и разнообразия.
3. Есть ли фактические расхождения.
4. В каких задачах уместнее каждая температура.
5. Краткий итог — какая температура здесь сработала лучше и почему.

Не пересказывай ответы целиком — сравнивай по существу.`;
}

async function consumeSSE(response, onEvent) {
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    const parts = buffer.split("\n\n");
    buffer = parts.pop() || "";

    for (const part of parts) {
      const lines = part.split("\n");
      let event = "message";
      const dataLines = [];
      for (const line of lines) {
        if (line.startsWith("event:")) event = line.slice(6).trim();
        else if (line.startsWith("data:")) dataLines.push(line.slice(5).trim());
      }
      if (!dataLines.length) continue;
      let payload = dataLines.join("\n");
      try {
        payload = JSON.parse(payload);
      } catch {
        /* keep raw */
      }
      onEvent(event, payload);
    }
  }
}

/**
 * @param {{message: string, temperature?: number|null, label?: {text: string, analysis?: boolean}}} opts
 * @returns {Promise<{reply: string, aborted: boolean, error: string|null}>}
 */
async function streamOnce({ message, temperature = null, label = null }) {
  const assistantEl = createMessage("assistant", "", "message--streaming", label);
  let fullText = "";
  let finalDebug = null;
  let streamError = null;
  let aborted = false;

  const requestBody = buildRequestBody(message, temperature);
  const started = performance.now();

  try {
    const headers = { "Content-Type": "application/json" };
    if (debugEnabled) headers["X-Debug"] = "true";

    const res = await fetch("/api/chat/stream", {
      method: "POST",
      headers,
      body: JSON.stringify(requestBody),
      signal: activeAbort.signal,
    });

    if (!res.ok) {
      let data = {};
      try {
        data = await res.json();
      } catch {
        /* ignore */
      }
      throw new Error(data.error || `HTTP ${res.status}`);
    }

    await consumeSSE(res, (event, payload) => {
      if (event === "token" && payload?.delta) {
        fullText += payload.delta;
        setBubbleText(assistantEl, fullText.replaceAll("<<<END>>>", "").trimStart());
      } else if (event === "done") {
        fullText = payload.reply || fullText;
        finalDebug = payload.debug || null;
      } else if (event === "aborted") {
        aborted = true;
        fullText = payload.reply || fullText;
      } else if (event === "error") {
        streamError = payload.error || "Ошибка генерации";
        finalDebug = payload.debug || null;
      }
    });

    const durationMs = Math.round(performance.now() - started);
    assistantEl.classList.remove("message--streaming");
    const clean = fullText.replaceAll("<<<END>>>", "").trim();

    logExchange({
      requestBody,
      status: streamError ? 502 : 200,
      responseBody: streamError
        ? { error: streamError, partial: clean }
        : { reply: clean, aborted, streamed: true },
      durationMs,
      upstream: finalDebug,
    });

    if (streamError) {
      setBubbleText(assistantEl, streamError);
      assistantEl.classList.add("message--error");
      return { reply: "", aborted: false, error: streamError };
    }

    if (!clean) {
      const emptyText = aborted ? "Генерация остановлена." : "Пустой ответ.";
      setBubbleText(assistantEl, emptyText);
      return { reply: "", aborted, error: aborted ? "aborted" : "empty" };
    }

    setBubbleText(assistantEl, clean + (aborted ? "\n\n[остановлено]" : ""));
    return { reply: clean, aborted, error: null };
  } catch (err) {
    assistantEl.classList.remove("message--streaming");
    const durationMs = Math.round(performance.now() - started);
    const isAbort = err?.name === "AbortError";

    if (isAbort) {
      const clean = fullText.replaceAll("<<<END>>>", "").trim();
      setBubbleText(assistantEl, clean ? clean + "\n\n[остановлено]" : "Генерация остановлена.");
      logExchange({
        requestBody,
        status: 499,
        responseBody: { aborted: true, reply: clean },
        durationMs,
        upstream: null,
      });
      return { reply: clean, aborted: true, error: "aborted" };
    }

    setBubbleText(assistantEl, err.message || "Не удалось связаться с сервером.");
    assistantEl.classList.add("message--error");
    logExchange({
      requestBody,
      status: 0,
      responseBody: { error: String(err) },
      durationMs,
      upstream: null,
    });
    return { reply: "", aborted: false, error: err.message || String(err) };
  }
}

async function sendCompare(text) {
  createMessage("user", text);
  setLoading(true);
  activeAbort = new AbortController();

  const replies = [];

  try {
    for (const temp of COMPARE_TEMPS) {
      if (activeAbort.signal.aborted) break;

      const result = await streamOnce({
        message: text,
        temperature: temp,
        label: { text: `temperature ${formatTemp(temp)}` },
      });

      replies.push({ temperature: temp, reply: result.reply });

      if (result.aborted || result.error === "aborted") {
        return;
      }
    }

    if (activeAbort.signal.aborted) return;

    await streamOnce({
      message: buildAnalysisPrompt(text, replies),
      temperature: null,
      label: { text: "Сравнительный анализ", analysis: true },
    });
  } finally {
    activeAbort = null;
    setLoading(false);
    inputEl.focus();
  }
}

async function sendNormal(text) {
  createMessage("user", text);
  history.push({ role: "user", content: text });

  setLoading(true);
  const assistantEl = createMessage("assistant", "", "message--streaming");
  let fullText = "";
  let finalDebug = null;
  let streamError = null;
  let aborted = false;

  const requestBody = {
    message: text,
    history: history.slice(0, -1),
    temperature: settings.temperature,
  };
  const started = performance.now();
  activeAbort = new AbortController();

  try {
    const headers = { "Content-Type": "application/json" };
    if (debugEnabled) headers["X-Debug"] = "true";

    const res = await fetch("/api/chat/stream", {
      method: "POST",
      headers,
      body: JSON.stringify(requestBody),
      signal: activeAbort.signal,
    });

    if (!res.ok) {
      let data = {};
      try {
        data = await res.json();
      } catch {
        /* ignore */
      }
      throw new Error(data.error || `HTTP ${res.status}`);
    }

    await consumeSSE(res, (event, payload) => {
      if (event === "token" && payload?.delta) {
        fullText += payload.delta;
        setBubbleText(assistantEl, fullText.replaceAll("<<<END>>>", "").trimStart());
      } else if (event === "done") {
        fullText = payload.reply || fullText;
        finalDebug = payload.debug || null;
      } else if (event === "aborted") {
        aborted = true;
        fullText = payload.reply || fullText;
      } else if (event === "error") {
        streamError = payload.error || "Ошибка генерации";
        finalDebug = payload.debug || null;
      }
    });

    const durationMs = Math.round(performance.now() - started);
    assistantEl.classList.remove("message--streaming");
    const clean = fullText.replaceAll("<<<END>>>", "").trim();

    logExchange({
      requestBody,
      status: streamError ? 502 : 200,
      responseBody: streamError
        ? { error: streamError, partial: clean }
        : { reply: clean, aborted, streamed: true },
      durationMs,
      upstream: finalDebug,
    });

    if (streamError) {
      setBubbleText(assistantEl, streamError);
      assistantEl.classList.add("message--error");
      history.pop();
      return;
    }

    if (!clean) {
      setBubbleText(assistantEl, aborted ? "Генерация остановлена." : "Пустой ответ.");
      if (!aborted) history.pop();
      else history.push({ role: "assistant", content: "(остановлено)" });
      return;
    }

    setBubbleText(assistantEl, clean + (aborted ? "\n\n[остановлено]" : ""));
    history.push({ role: "assistant", content: clean });
  } catch (err) {
    assistantEl.classList.remove("message--streaming");
    const durationMs = Math.round(performance.now() - started);
    const isAbort = err?.name === "AbortError";

    if (isAbort) {
      const clean = fullText.replaceAll("<<<END>>>", "").trim();
      setBubbleText(assistantEl, clean ? clean + "\n\n[остановлено]" : "Генерация остановлена.");
      if (clean) history.push({ role: "assistant", content: clean });
      else history.pop();

      logExchange({
        requestBody,
        status: 499,
        responseBody: { aborted: true, reply: clean },
        durationMs,
        upstream: null,
      });
    } else {
      setBubbleText(assistantEl, err.message || "Не удалось связаться с сервером.");
      assistantEl.classList.add("message--error");
      history.pop();

      logExchange({
        requestBody,
        status: 0,
        responseBody: { error: String(err) },
        durationMs,
        upstream: null,
      });
    }
  } finally {
    activeAbort = null;
    setLoading(false);
    inputEl.focus();
  }
}

async function sendMessage(text) {
  if (compareEnabled) {
    await sendCompare(text);
    return;
  }
  await sendNormal(text);
}

function openSettings() {
  syncTempUI(settings.temperature);
  settingsModal.hidden = false;
}

function closeSettings() {
  settingsModal.hidden = true;
}

formEl.addEventListener("submit", (e) => {
  e.preventDefault();
  const text = inputEl.value.trim();
  if (!text || isLoading) return;

  inputEl.value = "";
  autoResizeTextarea();
  sendMessage(text);
});

stopBtn.addEventListener("click", () => {
  activeAbort?.abort();
});

inputEl.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    formEl.requestSubmit();
  }
});

inputEl.addEventListener("input", autoResizeTextarea);

clearBtn.addEventListener("click", () => {
  if (isLoading) activeAbort?.abort();
  history = [];
  chatEl.innerHTML = "";
  if (welcomeEl) {
    welcomeEl.style.display = "";
    chatEl.appendChild(welcomeEl);
  }
  inputEl.focus();
});

settingsBtn.addEventListener("click", openSettings);
saveSettingsBtn.addEventListener("click", () => {
  saveSettings({
    temperature: clampTemperature(temperatureInput.value),
  });
  closeSettings();
});

temperatureInput.addEventListener("input", () => {
  syncTempUI(temperatureInput.value);
});

settingsModal.querySelectorAll("[data-close-settings]").forEach((el) => {
  el.addEventListener("click", closeSettings);
});

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !settingsModal.hidden) closeSettings();
});

debugToggle.addEventListener("change", () => {
  setDebugMode(debugToggle.checked);
});

compareToggle.addEventListener("change", () => {
  setCompareMode(compareToggle.checked);
});

clearDebugBtn.addEventListener("click", clearDebugLog);

setDebugMode(debugEnabled);
setCompareMode(compareEnabled);
updateTempBadge();
inputEl.focus();
