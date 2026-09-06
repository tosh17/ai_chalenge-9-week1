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

const DEBUG_STORAGE_KEY = "deepseek-chat-debug-day5";

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

function escapeHTML(text) {
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
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

function logExchange({ requestBody, status, responseBody, durationMs }) {
  if (!debugEnabled) return;

  if (debugEntryCount === 0) debugLog.innerHTML = "";
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
    ${appendDebugBlock("→ /api/compare", {
      method: "POST",
      url: "/api/compare",
      body: requestBody,
    })}
    ${appendDebugBlock("← результаты", responseBody, status >= 400 ? "debug-block__code--error" : "")}
  `;
  debugLog.prepend(entry);
}

function createUserMessage(text) {
  hideWelcome();
  const el = document.createElement("div");
  el.className = "message message--user";
  el.innerHTML = `
    <div class="message__avatar">Вы</div>
    <div class="message__bubble">${escapeHTML(text)}</div>
  `;
  chatEl.appendChild(el);
  scrollToBottom();
}

function createCompareBlock() {
  hideWelcome();
  const el = document.createElement("div");
  el.className = "message message--assistant message--compare";
  el.innerHTML = `
    <div class="message__avatar">AI</div>
    <div class="compare">
      <div class="compare__label">Сравнение моделей · ответы приходят по мере готовности</div>
      <div class="compare__grid"></div>
      <div class="stats-table-wrap" hidden>
        <h3 class="stats-table__title">Сводка метрик</h3>
        <table class="stats-table">
          <thead>
            <tr>
              <th>Модель</th>
              <th>Время</th>
              <th>In</th>
              <th>Out</th>
              <th>Think</th>
              <th>Всего</th>
              <th>токен/с</th>
              <th>Стоимость</th>
            </tr>
          </thead>
          <tbody></tbody>
        </table>
        <p class="stats-table__hint">Стоимость — оценка по off-peak тарифам DeepSeek (cache hit/miss + output).</p>
      </div>
    </div>
  `;
  chatEl.appendChild(el);
  scrollToBottom();
  return el;
}

function ensureCard(compareEl, meta) {
  const grid = compareEl.querySelector(".compare__grid");
  let card = grid.querySelector(`[data-model="${meta.id}"]`);
  if (card) return card;

  card = document.createElement("article");
  card.className = "strategy-card strategy-card--loading";
  card.dataset.model = meta.id;
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
  });

  grid.appendChild(card);
  scrollToBottom();
  return card;
}

function formatDuration(ms) {
  if (ms == null) return "—";
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

function formatCost(usd) {
  if (usd == null || Number.isNaN(usd)) return "—";
  if (usd < 0.0001) return `$${usd.toFixed(6)}`;
  return `$${usd.toFixed(5)}`;
}

function formatTps(v) {
  if (v == null || Number.isNaN(v)) return "—";
  return v.toFixed(1);
}

function fillCard(card, result, { autoOpen = false } = {}) {
  const badge = card.querySelector(".strategy-card__badge");
  const body = card.querySelector(".strategy-card__body");
  const hasError = Boolean(result.error);

  card.classList.remove("strategy-card--loading");
  card.classList.toggle("strategy-card--error", hasError);
  card.classList.toggle("strategy-card--ready", !hasError);

  const stats = result.stats || {};
  badge.innerHTML = hasError
    ? "ошибка"
    : formatDuration(stats.duration_ms);

  if (hasError) {
    body.innerHTML = `<pre class="strategy-card__reply">${escapeHTML(result.error)}</pre>`;
  } else {
    const reasoning = result.reasoning_content
      ? `
        <div class="strategy-card__section-title">Reasoning</div>
        <pre class="strategy-card__prompt">${escapeHTML(result.reasoning_content)}</pre>
      `
      : "";

    body.innerHTML = `
      ${reasoning}
      <div class="strategy-card__section-title">Ответ</div>
      <pre class="strategy-card__reply">${escapeHTML(result.reply || "(пустой ответ)")}</pre>
      <div class="strategy-card__metrics">
        <span>in ${stats.prompt_tokens ?? 0}</span>
        <span>out ${stats.completion_tokens ?? 0}</span>
        <span>think ${stats.reasoning_tokens ?? 0}</span>
        <span>${formatTps(stats.tokens_per_sec)} tok/s</span>
        <span>${formatCost(stats.cost_usd)}</span>
      </div>
    `;
  }

  if (autoOpen && !hasError) {
    card.classList.add("strategy-card--open");
    card.querySelector(".strategy-card__head")?.setAttribute("aria-expanded", "true");
  }
}

function renderStatsTable(compareEl, results) {
  const wrap = compareEl.querySelector(".stats-table-wrap");
  const tbody = wrap.querySelector("tbody");
  tbody.innerHTML = "";

  const ordered = ["weak", "medium", "strong"];
  const byId = Object.fromEntries(results.map((r) => [r.id, r]));

  for (const id of ordered) {
    const r = byId[id];
    if (!r) continue;
    const s = r.stats || {};
    const tr = document.createElement("tr");
    if (r.error) tr.className = "stats-table__row--error";
    tr.innerHTML = `
      <td>
        <div class="stats-table__model">${escapeHTML(r.title)}</div>
        <div class="stats-table__model-id">${escapeHTML(r.model)} · ${escapeHTML(r.thinking)}</div>
      </td>
      <td>${r.error ? "—" : formatDuration(s.duration_ms)}</td>
      <td>${r.error ? "—" : (s.prompt_tokens ?? 0)}</td>
      <td>${r.error ? "—" : (s.completion_tokens ?? 0)}</td>
      <td>${r.error ? "—" : (s.reasoning_tokens ?? 0)}</td>
      <td>${r.error ? "—" : (s.total_tokens ?? 0)}</td>
      <td>${r.error ? "—" : formatTps(s.tokens_per_sec)}</td>
      <td>${r.error ? "—" : formatCost(s.cost_usd)}</td>
    `;
    tbody.appendChild(tr);
  }

  wrap.hidden = false;
  scrollToBottom();
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

async function sendCompare(text) {
  createUserMessage(text);
  setLoading(true);
  activeAbort = new AbortController();

  const compareEl = createCompareBlock();
  const requestBody = { message: text };
  const started = performance.now();
  let finalResults = [];

  try {
    const res = await fetch("/api/compare", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
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
      if (event === "card_start") {
        ensureCard(compareEl, payload);
      } else if (event === "card_done") {
        const card = ensureCard(compareEl, payload);
        fillCard(card, payload, { autoOpen: payload.id === "strong" && !payload.error });
      } else if (event === "done" || event === "aborted") {
        finalResults = payload.results || [];
        renderStatsTable(compareEl, finalResults);
      }
    });

    logExchange({
      requestBody,
      status: 200,
      responseBody: { results: finalResults },
      durationMs: Math.round(performance.now() - started),
    });
  } catch (err) {
    const isAbort = err?.name === "AbortError";
    if (!isAbort) {
      const fail = document.createElement("div");
      fail.className = "message message--assistant message--error";
      fail.innerHTML = `
        <div class="message__avatar">AI</div>
        <div class="message__bubble">${escapeHTML(err.message || "Не удалось связаться с сервером.")}</div>
      `;
      chatEl.appendChild(fail);
    }
    logExchange({
      requestBody,
      status: isAbort ? 499 : 0,
      responseBody: { error: String(err), results: finalResults },
      durationMs: Math.round(performance.now() - started),
    });
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
  sendCompare(text);
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
