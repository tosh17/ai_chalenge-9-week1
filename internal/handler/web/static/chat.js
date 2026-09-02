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
const personaGrid = document.getElementById("personaGrid");
const customPersonaField = document.getElementById("customPersonaField");
const customPersonaInput = document.getElementById("customPersonaInput");
const maxTokensInput = document.getElementById("maxTokensInput");
const maxWordsInput = document.getElementById("maxWordsInput");
const personaBadge = document.getElementById("personaBadge");
const layoutEl = document.getElementById("layout");
const debugToggle = document.getElementById("debugToggle");
const debugPanel = document.getElementById("debugPanel");
const debugLog = document.getElementById("debugLog");
const clearDebugBtn = document.getElementById("clearDebugBtn");

const DEBUG_STORAGE_KEY = "deepseek-chat-debug";
const SETTINGS_STORAGE_KEY = "deepseek-chat-settings";

const DEFAULT_PERSONAS = [
  { id: "default", title: "Ассистент", description: "Краткий и деловой стиль" },
  { id: "bender", title: "Бендер", description: "Робот из «Футурамы»" },
  { id: "yoda", title: "Мастер Йода", description: "Мудрец из «Звёздных войн»" },
  { id: "peasant", title: "Средневековый крестьянин", description: "Просторечье и суеверия" },
  { id: "ravshan", title: "Равшан", description: "Персонаж «Наша Russia»" },
  { id: "custom", title: "Свой персонаж", description: "Опишите роль сами" },
];

/** @type {{role: string, content: string}[]} */
let history = [];
let isLoading = false;
let debugEnabled = localStorage.getItem(DEBUG_STORAGE_KEY) === "true";
let debugEntryCount = 0;
/** @type {AbortController | null} */
let activeAbort = null;
/** @type {{id: string, title: string, description: string}[]} */
let personas = DEFAULT_PERSONAS;
let draftPersona = "default";

let settings = loadSettings();

function loadSettings() {
  try {
    const raw = localStorage.getItem(SETTINGS_STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      return {
        persona: parsed.persona || "default",
        customPersona: parsed.customPersona || "",
        maxTokens: Number(parsed.maxTokens) || 300,
        maxWords: Number(parsed.maxWords) || 120,
      };
    }
  } catch {
    /* ignore */
  }
  return { persona: "default", customPersona: "", maxTokens: 300, maxWords: 120 };
}

function saveSettings(next) {
  settings = next;
  localStorage.setItem(SETTINGS_STORAGE_KEY, JSON.stringify(settings));
  updatePersonaBadge();
}

function personaTitle(id) {
  return personas.find((p) => p.id === id)?.title || "Ассистент";
}

function updatePersonaBadge() {
  personaBadge.textContent = personaTitle(settings.persona);
}

function hideWelcome() {
  if (welcomeEl) welcomeEl.style.display = "none";
}

function scrollToBottom() {
  chatEl.scrollTop = chatEl.scrollHeight;
}

function createMessage(role, content, extraClass = "") {
  hideWelcome();

  const isUser = role === "user";
  const el = document.createElement("div");
  el.className = `message message--${role} ${extraClass}`.trim();
  el.innerHTML = `
    <div class="message__avatar">${isUser ? "Вы" : "AI"}</div>
    <div class="message__bubble"></div>
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

function logExchange({ requestBody, status, responseBody, durationMs, upstream }) {
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
    ${appendDebugBlock("→ Запрос к /api/chat/stream", {
      method: "POST",
      url: "/api/chat/stream",
      headers: { "Content-Type": "application/json", "X-Debug": debugEnabled ? "true" : undefined },
      body: requestBody,
    })}
    ${appendDebugBlock("← Ответ stream", responseBody, status >= 400 ? "debug-block__code--error" : "")}
    ${upstream ? appendDebugBlock("↔ DeepSeek API", upstream) : ""}
  `;

  debugLog.prepend(entry);
}

function buildRequestBody(text) {
  return {
    message: text,
    history: history.slice(0, -1),
    persona: settings.persona,
    custom_persona: settings.customPersona,
    max_tokens: settings.maxTokens,
    max_words: settings.maxWords,
  };
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

async function sendMessage(text) {
  createMessage("user", text);
  history.push({ role: "user", content: text });

  setLoading(true);
  const assistantEl = createMessage("assistant", "", "message--streaming");
  let fullText = "";
  let finalDebug = null;
  let streamError = null;
  let aborted = false;

  const requestBody = buildRequestBody(text);
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
        // Не показываем stop-маркер в UI по мере стрима
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

function openSettings() {
  draftPersona = settings.persona;
  maxTokensInput.value = String(settings.maxTokens);
  maxWordsInput.value = String(settings.maxWords);
  customPersonaInput.value = settings.customPersona;
  renderPersonaCards();
  settingsModal.hidden = false;
}

function closeSettings() {
  settingsModal.hidden = true;
}

function renderPersonaCards() {
  personaGrid.innerHTML = "";
  for (const p of personas) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = `persona-card${draftPersona === p.id ? " persona-card--active" : ""}`;
    btn.innerHTML = `
      <span class="persona-card__title">${escapeHTML(p.title)}</span>
      <span class="persona-card__desc">${escapeHTML(p.description)}</span>
    `;
    btn.addEventListener("click", () => {
      draftPersona = p.id;
      customPersonaField.hidden = draftPersona !== "custom";
      renderPersonaCards();
    });
    personaGrid.appendChild(btn);
  }
  customPersonaField.hidden = draftPersona !== "custom";
}

async function loadPersonas() {
  try {
    const res = await fetch("/api/personas");
    if (!res.ok) return;
    const data = await res.json();
    if (Array.isArray(data.personas) && data.personas.length) {
      personas = data.personas;
    }
  } catch {
    /* fallback to defaults */
  }
  updatePersonaBadge();
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
    persona: draftPersona,
    customPersona: customPersonaInput.value.trim(),
    maxTokens: Math.max(32, Number(maxTokensInput.value) || 300),
    maxWords: Math.max(20, Number(maxWordsInput.value) || 120),
  });
  closeSettings();
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

clearDebugBtn.addEventListener("click", clearDebugLog);

setDebugMode(debugEnabled);
updatePersonaBadge();
loadPersonas();
inputEl.focus();
