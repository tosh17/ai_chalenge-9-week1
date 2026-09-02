const chatEl = document.getElementById("chat");
const welcomeEl = document.getElementById("welcome");
const formEl = document.getElementById("chatForm");
const inputEl = document.getElementById("messageInput");
const sendBtn = document.getElementById("sendBtn");
const stopBtn = document.getElementById("stopBtn");
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
/** @type {AbortController | null} */
let activeAbort = null;

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

function createCompareMessage() {
  hideWelcome();

  const el = document.createElement("div");
  el.className = "message message--assistant message--compare";
  el.innerHTML = `
    <div class="message__avatar">AI</div>
    <div class="compare">
      <div class="compare__label">Сравнение техник · ответы появятся по мере готовности</div>
      <div class="compare__grid"></div>
    </div>
  `;
  chatEl.appendChild(el);
  scrollToBottom();
  return el;
}

function ensureStrategyCard(compareEl, meta) {
  const grid = compareEl.querySelector(".compare__grid");
  let card = grid.querySelector(`[data-strategy="${meta.id}"]`);
  if (card) return card;

  card = document.createElement("article");
  card.className = "strategy-card strategy-card--loading";
  card.dataset.strategy = meta.id;
  card.innerHTML = `
    <button type="button" class="strategy-card__head" aria-expanded="false">
      <span class="strategy-card__head-main">
        <span class="strategy-card__title">${escapeHTML(meta.title)}</span>
        <span class="strategy-card__desc">${escapeHTML(meta.description)}</span>
      </span>
      <span class="strategy-card__meta">
        <span class="strategy-card__badge"><span class="strategy-card__spinner"></span>ждём</span>
        <span class="strategy-card__chevron">▼</span>
      </span>
    </button>
    <div class="strategy-card__body">
      <p class="strategy-card__placeholder">Запрос выполняется…</p>
    </div>
  `;

  card.querySelector(".strategy-card__head").addEventListener("click", () => {
    const willOpen = !card.classList.contains("strategy-card--open");
    grid.querySelectorAll(".strategy-card--open").forEach((other) => {
      if (other !== card) {
        other.classList.remove("strategy-card--open");
        other.querySelector(".strategy-card__head")?.setAttribute("aria-expanded", "false");
      }
    });
    card.classList.toggle("strategy-card--open", willOpen);
    card.querySelector(".strategy-card__head").setAttribute("aria-expanded", willOpen ? "true" : "false");
    scrollToBottom();
  });

  grid.appendChild(card);
  scrollToBottom();
  return card;
}

function fillStrategyCard(card, result, { autoOpen = false } = {}) {
  const badge = card.querySelector(".strategy-card__badge");
  const body = card.querySelector(".strategy-card__body");
  const hasError = Boolean(result.error);

  card.classList.remove("strategy-card--loading");
  card.classList.toggle("strategy-card--error", hasError);
  card.classList.toggle("strategy-card--ready", !hasError);

  if (hasError) {
    badge.textContent = "ошибка";
    body.innerHTML = `<pre class="strategy-card__reply">${escapeHTML(result.error)}</pre>`;
  } else {
    const ms = result.duration_ms != null ? `${result.duration_ms} ms` : "";
    badge.textContent = "готово";
    let html = "";
    if (result.generated_prompt) {
      html += `
        <div class="strategy-card__section-title">Сгенерированный промпт</div>
        <pre class="strategy-card__prompt">${escapeHTML(result.generated_prompt)}</pre>
        <div class="strategy-card__section-title">Ответ модели</div>
      `;
    }
    html += `<pre class="strategy-card__reply">${escapeHTML(result.reply || "Пустой ответ.")}</pre>`;
    if (ms) html += `<div class="strategy-card__duration">${escapeHTML(ms)}</div>`;
    body.innerHTML = html;
  }

  if (autoOpen && !card.classList.contains("strategy-card--open")) {
    card.querySelector(".strategy-card__head").click();
  }
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
    ${appendDebugBlock("→ Запрос к /api/chat/compare", {
      method: "POST",
      url: "/api/chat/compare",
      headers: { "Content-Type": "application/json", "X-Debug": debugEnabled ? "true" : undefined },
      body: requestBody,
    })}
    ${appendDebugBlock("← Ответ compare", responseBody, status >= 400 ? "debug-block__code--error" : "")}
    ${upstream ? appendDebugBlock("↔ DeepSeek API", upstream) : ""}
  `;

  debugLog.prepend(entry);
}

function buildCompareBody(text) {
  return { message: text };
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
  const compareEl = createCompareMessage();
  const cards = new Map();
  let finalResults = [];
  let openedFirst = false;
  let aborted = false;

  const requestBody = buildCompareBody(text);
  const started = performance.now();
  activeAbort = new AbortController();

  try {
    const headers = { "Content-Type": "application/json" };
    if (debugEnabled) headers["X-Debug"] = "true";

    const res = await fetch("/api/chat/compare", {
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
      if (event === "card_start" && payload?.id) {
        const card = ensureStrategyCard(compareEl, payload);
        cards.set(payload.id, card);
      } else if (event === "card_done" && payload?.id) {
        let card = cards.get(payload.id);
        if (!card) {
          card = ensureStrategyCard(compareEl, payload);
          cards.set(payload.id, card);
        }
        const autoOpen = !openedFirst && !payload.error;
        if (autoOpen) openedFirst = true;
        fillStrategyCard(card, payload, { autoOpen });
      } else if (event === "done") {
        finalResults = payload.results || [];
      } else if (event === "aborted") {
        aborted = true;
        finalResults = payload.results || [];
      }
    });

    const durationMs = Math.round(performance.now() - started);
    const label = compareEl.querySelector(".compare__label");
    if (label) {
      label.textContent = aborted
        ? "Сравнение прервано"
        : `Сравнение техник · ${finalResults.filter((r) => !r.error).length}/${finalResults.length || cards.size} готово`;
    }

    logExchange({
      requestBody,
      status: 200,
      responseBody: { results: finalResults, aborted },
      durationMs,
      upstream: null,
    });

    const summary = finalResults
      .map((r) => `${r.title}: ${r.error ? "ошибка" : "ok"}`)
      .join("; ");
    history.push({ role: "assistant", content: summary || "(сравнение стратегий)" });
  } catch (err) {
    const durationMs = Math.round(performance.now() - started);
    const isAbort = err?.name === "AbortError";
    const label = compareEl.querySelector(".compare__label");

    if (isAbort) {
      if (label) label.textContent = "Сравнение прервано";
      compareEl.querySelectorAll(".strategy-card--loading").forEach((card) => {
        fillStrategyCard(card, { error: "Остановлено пользователем" });
      });
      history.push({ role: "assistant", content: "(сравнение остановлено)" });
      logExchange({
        requestBody,
        status: 499,
        responseBody: { aborted: true },
        durationMs,
        upstream: null,
      });
    } else {
      if (label) label.textContent = "Ошибка сравнения";
      const grid = compareEl.querySelector(".compare__grid");
      grid.innerHTML = `<article class="strategy-card strategy-card--error strategy-card--open">
        <div class="strategy-card__body" style="display:block;border:none;padding:14px">
          <pre class="strategy-card__reply">${escapeHTML(err.message || "Не удалось связаться с сервером.")}</pre>
        </div>
      </article>`;
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

debugToggle.addEventListener("change", () => {
  setDebugMode(debugToggle.checked);
});

clearDebugBtn.addEventListener("click", clearDebugLog);

setDebugMode(debugEnabled);
inputEl.focus();
