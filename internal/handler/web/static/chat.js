const chatEl = document.getElementById("chat");
const welcomeEl = document.getElementById("welcome");
const formEl = document.getElementById("chatForm");
const inputEl = document.getElementById("messageInput");
const sendBtn = document.getElementById("sendBtn");
const clearBtn = document.getElementById("clearBtn");
const layoutEl = document.getElementById("layout");
const debugToggle = document.getElementById("debugToggle");
const debugPanel = document.getElementById("debugPanel");
const debugLog = document.getElementById("debugLog");
const clearDebugBtn = document.getElementById("clearDebugBtn");

const DEBUG_STORAGE_KEY = "deepseek-chat-debug";

/** @type {{role: string, content: string}[]} */
let history = [];
let isLoading = false;
let debugEnabled = localStorage.getItem(DEBUG_STORAGE_KEY) === "true";
let debugEntryCount = 0;

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
    <div class="message__bubble">${escapeHTML(content)}</div>
  `;
  chatEl.appendChild(el);
  scrollToBottom();
  return el;
}

function createTypingIndicator() {
  hideWelcome();

  const el = document.createElement("div");
  el.className = "message message--assistant message--typing";
  el.id = "typingIndicator";
  el.innerHTML = `
    <div class="message__avatar">AI</div>
    <div class="message__bubble">
      <span class="typing-dot"></span>
      <span class="typing-dot"></span>
      <span class="typing-dot"></span>
    </div>
  `;
  chatEl.appendChild(el);
  scrollToBottom();
  return el;
}

function removeTypingIndicator() {
  document.getElementById("typingIndicator")?.remove();
}

function escapeHTML(text) {
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
}

function setLoading(loading) {
  isLoading = loading;
  sendBtn.disabled = loading;
  inputEl.disabled = loading;
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
    ${appendDebugBlock("→ Запрос к /api/chat", {
      method: "POST",
      url: "/api/chat",
      headers: { "Content-Type": "application/json", "X-Debug": "true" },
      body: requestBody,
    })}
    ${appendDebugBlock("← Ответ /api/chat", responseBody, status >= 400 ? "debug-block__code--error" : "")}
    ${upstream ? appendDebugBlock("↔ DeepSeek API", upstream) : ""}
  `;

  debugLog.prepend(entry);
}

async function sendMessage(text) {
  createMessage("user", text);
  history.push({ role: "user", content: text });

  setLoading(true);
  createTypingIndicator();

  const requestBody = { message: text, history: history.slice(0, -1) };
  const started = performance.now();

  try {
    const headers = { "Content-Type": "application/json" };
    if (debugEnabled) {
      headers["X-Debug"] = "true";
    }

    const res = await fetch("/api/chat", {
      method: "POST",
      headers,
      body: JSON.stringify(requestBody),
    });

    const data = await res.json();
    const durationMs = Math.round(performance.now() - started);

    removeTypingIndicator();

    logExchange({
      requestBody,
      status: res.status,
      responseBody: data,
      durationMs,
      upstream: data.debug || null,
    });

    if (!res.ok) {
      createMessage("assistant", data.error || "Неизвестная ошибка", "message--error");
      history.pop();
      return;
    }

    createMessage("assistant", data.reply);
    history.push({ role: "assistant", content: data.reply });
  } catch (err) {
    removeTypingIndicator();
    const durationMs = Math.round(performance.now() - started);

    logExchange({
      requestBody,
      status: 0,
      responseBody: { error: "Не удалось связаться с сервером", details: String(err) },
      durationMs,
      upstream: null,
    });

    createMessage("assistant", "Не удалось связаться с сервером.", "message--error");
    history.pop();
  } finally {
    setLoading(false);
    inputEl.focus();
  }
}

formEl.addEventListener("submit", (e) => {
  e.preventDefault();
  const text = inputEl.value.trim();
  if (!text || isLoading) return;

  inputEl.value = "";
  autoResizeTextarea();
  sendMessage(text);
});

inputEl.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    formEl.requestSubmit();
  }
});

inputEl.addEventListener("input", autoResizeTextarea);

clearBtn.addEventListener("click", () => {
  history = [];
  chatEl.innerHTML = "";
  if (welcomeEl) {
    welcomeEl.style.display = "";
    chatEl.appendChild(welcomeEl);
  }
  inputEl.focus();
});

debugToggle.addEventListener("change", () => {
  setDebugMode(debugToggle.checked);
});

clearDebugBtn.addEventListener("click", clearDebugLog);

setDebugMode(debugEnabled);
inputEl.focus();
