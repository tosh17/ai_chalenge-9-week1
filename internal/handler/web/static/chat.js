const formEl = document.getElementById("chatForm");
const inputEl = document.getElementById("messageInput");
const sendBtn = document.getElementById("sendBtn");
const clearBtn = document.getElementById("clearBtn");
const layoutEl = document.getElementById("layout");
const debugToggle = document.getElementById("debugToggle");
const debugPanel = document.getElementById("debugPanel");
const debugLog = document.getElementById("debugLog");
const clearDebugBtn = document.getElementById("clearDebugBtn");
const providerSelect = document.getElementById("providerSelect");
const providerLabel = document.getElementById("providerLabel");
const appSubtitle = document.getElementById("appSubtitle");
const compressHint = document.getElementById("compressHint");
const fullHint = document.getElementById("fullHint");

const DEBUG_STORAGE_KEY = "deepseek-chat-debug-day9";
const PROVIDER_STORAGE_KEY = "day9-chat-provider";

const panes = {
  compress: {
    chat: document.getElementById("chatCompress"),
    welcome: document.getElementById("welcomeCompress"),
    bar: document.getElementById("tokenBarCompress"),
    text: document.getElementById("tokenTextCompress"),
    hint: compressHint,
    typingId: "typingCompress",
  },
  full: {
    chat: document.getElementById("chatFull"),
    welcome: document.getElementById("welcomeFull"),
    bar: document.getElementById("tokenBarFull"),
    text: document.getElementById("tokenTextFull"),
    hint: fullHint,
    typingId: "typingFull",
  },
};

let isLoading = false;
let debugEnabled = localStorage.getItem(DEBUG_STORAGE_KEY) === "true";
let debugEntryCount = 0;
/** @type {{id: string, title: string, model: string, context_limit?: number}[]} */
let providers = [];

function hideWelcome(pane) {
  if (pane.welcome) pane.welcome.style.display = "none";
}

function scrollPane(pane) {
  pane.chat.scrollTop = pane.chat.scrollHeight;
}

function formatDuration(ms) {
  if (ms == null || Number.isNaN(ms)) return "";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

function escapeHTML(text) {
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
}

function createMessage(pane, role, content, extraClass = "", meta = {}) {
  hideWelcome(pane);

  const isUser = role === "user";
  const isSystem = role === "system";
  const el = document.createElement("div");
  el.className = `message message--${role} ${extraClass}`.trim();

  let avatar = "AI";
  if (isUser) avatar = "Вы";
  if (isSystem) avatar = "∑";

  const timeLabel = !isUser && !isSystem && meta.durationMs != null
    ? `<span class="message__time" title="время ответа">${escapeHTML(formatDuration(meta.durationMs))}</span>`
    : "";

  el.innerHTML = `
    <div class="message__avatar">${avatar}</div>
    <div class="message__body">
      <div class="message__bubble">${escapeHTML(content)}</div>
      ${timeLabel}
    </div>
  `;
  pane.chat.appendChild(el);
  scrollPane(pane);
  return el;
}

function createTypingIndicator(pane) {
  hideWelcome(pane);
  const el = document.createElement("div");
  el.className = "message message--assistant message--typing";
  el.id = pane.typingId;
  el.innerHTML = `
    <div class="message__avatar">AI</div>
    <div class="message__body">
      <div class="message__bubble">
        <span class="typing-dot"></span>
        <span class="typing-dot"></span>
        <span class="typing-dot"></span>
      </div>
    </div>
  `;
  pane.chat.appendChild(el);
  scrollPane(pane);
  return el;
}

function removeTypingIndicator(pane) {
  document.getElementById(pane.typingId)?.remove();
}

function setLoading(loading) {
  isLoading = loading;
  sendBtn.disabled = loading;
  inputEl.disabled = loading;
  providerSelect.disabled = loading;
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
  debugLog.innerHTML = '<p class="debug-panel__empty">Запросы и шаги агентов появятся здесь.</p>';
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

function logExchange({ requestBody, status, responseBody, durationMs, url = "/api/chat/dual" }) {
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
    ${appendDebugBlock(`→ ${url}`, { method: "POST", url, body: requestBody })}
    ${appendDebugBlock("← ответ", responseBody, status >= 400 ? "debug-block__code--error" : "")}
  `;
  debugLog.prepend(entry);
}

function currentProvider() {
  return providerSelect.value || "local";
}

function updateProviderLabel() {
  const p = providers.find((x) => x.id === currentProvider());
  if (p) {
    const limit = p.context_limit ? ` · ${(p.context_limit / 1000).toFixed(0)}K` : "";
    providerLabel.textContent = `${p.title} · ${p.model}${limit}`;
  } else {
    providerLabel.textContent = currentProvider();
  }
}

function renderProviders(list, defaultProvider) {
  providers = list;
  providerSelect.innerHTML = "";
  for (const p of list) {
    const opt = document.createElement("option");
    opt.value = p.id;
    const lim = p.context_limit ? ` · ${Math.round(p.context_limit / 1000)}K` : "";
    opt.textContent = `${p.title} (${p.model}${lim})`;
    providerSelect.appendChild(opt);
  }

  const saved = localStorage.getItem(PROVIDER_STORAGE_KEY);
  const preferred =
    (defaultProvider && list.some((p) => p.id === defaultProvider) && defaultProvider) ||
    (saved && list.some((p) => p.id === saved) && saved) ||
    (list.find((p) => p.default)?.id) ||
    (list.some((p) => p.id === "local") && "local") ||
    (list[0] && list[0].id);

  if (preferred) providerSelect.value = preferred;
  updateProviderLabel();
}

function formatCost(v) {
  if (v == null || Number.isNaN(v)) return "$0";
  if (v === 0) return "$0";
  if (v < 0.0001) return `$${v.toFixed(6)}`;
  return `$${v.toFixed(4)}`;
}

function updateTokenMeter(pane, tokens, session) {
  if (!pane.text || !pane.bar) return;
  if (!tokens && !session) {
    pane.text.textContent = "токены: —";
    pane.bar.style.width = "0%";
    pane.bar.className = "token-meter__fill";
    return;
  }

  const pct = Math.min(100, Math.max(0, (tokens && tokens.context_pct) || 0));
  pane.bar.style.width = `${pct}%`;
  pane.bar.className = "token-meter__fill";
  if (tokens && (pct >= 90 || tokens.over_limit)) pane.bar.classList.add("token-meter__fill--danger");
  else if (pct >= 60) pane.bar.classList.add("token-meter__fill--warn");

  const last = tokens
    ? `последний: ${tokens.total || tokens.prompt || 0} tok · prompt ${tokens.prompt || 0}/${tokens.context_limit || "?"} (${pct.toFixed(1)}%) · ${formatCost(tokens.cost_usd)}`
    : "последний: —";
  const sess = session
    ? `сессия: ${session.total_tokens || 0} tok / ${formatCost(session.estimated_cost_usd)}`
    : "сессия: —";
  pane.text.textContent = `${last} · ${sess}`;
}

function updatePaneHint(paneKey, compression) {
  const pane = panes[paneKey];
  if (!pane.hint || !compression) return;
  if (paneKey === "full" || !compression.enabled) {
    pane.hint.textContent = `вся история · ${compression.full_history_messages || 0} сообщ. · ≈${compression.full_history_tokens || 0} tok`;
    return;
  }
  const saved = compression.tokens_saved_estimate || 0;
  pane.hint.textContent =
    `summary ${compression.summarized_messages || 0} · сырых ${compression.raw_messages_in_prompt || 0}` +
    (saved > 0 ? ` · экономия ≈ ${saved} tok` : "");
}

function renderSummarizeEvents(pane, events) {
  if (!Array.isArray(events) || !events.length) return;
  for (const ev of events) {
    const tok = ev.total_tokens || (ev.prompt_tokens || 0) + (ev.completion_tokens || 0);
    const cost = formatCost(ev.cost_usd);
    const n = ev.messages_compressed || 0;
    let text =
      `Произошло сжатие: ${n} сообщений свёрнуты в summary. ` +
      `На операцию израсходовано ${tok} ток. (${cost})`;
    if (ev.summary_preview) {
      text += `\nПревью: ${ev.summary_preview}`;
    }
    createMessage(pane, "system", text, "message--system");
  }
}

function renderPaneResult(paneKey, side, clientDurationMs) {
  const pane = panes[paneKey];
  removeTypingIndicator(pane);

  if (side.error && !side.reply) {
    createMessage(pane, "assistant", side.error, "message--error", {
      durationMs: side.duration_ms ?? clientDurationMs,
    });
  } else if (side.reply) {
    createMessage(pane, "assistant", side.reply, side.error ? "message--error" : "", {
      durationMs: side.duration_ms ?? clientDurationMs,
    });
  }

  renderSummarizeEvents(pane, side.summarize_events);
  updateTokenMeter(pane, side.tokens, side.session);
  updatePaneHint(paneKey, side.compression);
}

function resetPaneUI(pane) {
  pane.chat.innerHTML = "";
  if (pane.welcome) {
    pane.welcome.style.display = "";
    pane.chat.appendChild(pane.welcome);
  }
  updateTokenMeter(pane, null, null);
}

async function loadHistory() {
  try {
    const res = await fetch("/api/history/dual");
    if (!res.ok) return;
    const data = await res.json();

    for (const key of ["compress", "full"]) {
      const side = data[key] || {};
      const pane = panes[key];
      const messages = Array.isArray(side.messages) ? side.messages : [];
      updateTokenMeter(pane, side.tokens, side.session);
      updatePaneHint(key, {
        enabled: key === "compress",
        ...(side.compression || {}),
        full_history_messages: messages.length,
      });
      if (!messages.length) continue;
      for (const m of messages) {
        if (!m || !m.content) continue;
        if (m.role === "user" || m.role === "assistant") {
          createMessage(pane, m.role, m.content);
        }
      }
      if (key === "compress" && side.summary) {
        createMessage(
          pane,
          "system",
          `Текущий summary (${side.compression?.summarize_every ? "политика сжатия активна" : "сохранён"}):\n${side.summary}`,
          "message--system",
        );
      }
    }
  } catch {
    /* keep empty */
  }
}

async function loadProviders() {
  try {
    const res = await fetch("/api/providers");
    if (!res.ok) return;
    const data = await res.json();
    if (Array.isArray(data.providers) && data.providers.length) {
      renderProviders(data.providers, data.default_provider);
    }
  } catch {
    /* keep defaults */
  }
}

async function sendMessage(text) {
  createMessage(panes.compress, "user", text);
  createMessage(panes.full, "user", text);

  setLoading(true);
  createTypingIndicator(panes.compress);
  createTypingIndicator(panes.full);

  const requestBody = {
    message: text,
    provider: currentProvider(),
  };
  const started = performance.now();

  try {
    const headers = { "Content-Type": "application/json" };
    if (debugEnabled) headers["X-Debug"] = "true";

    const res = await fetch("/api/chat/dual", {
      method: "POST",
      headers,
      body: JSON.stringify(requestBody),
    });

    const data = await res.json();
    const durationMs = Math.round(performance.now() - started);

    logExchange({ requestBody, status: res.status, responseBody: data, durationMs });

    renderPaneResult("compress", data.compress || { error: data.error || "нет ответа" }, durationMs);
    renderPaneResult("full", data.full || { error: data.error || "нет ответа" }, durationMs);

    if (appSubtitle) {
      const c = data.compress?.session?.total_tokens ?? "—";
      const f = data.full?.session?.total_tokens ?? "—";
      appSubtitle.textContent = `сессия: сжатие ${c} tok · без сжатия ${f} tok · Enter — отправить`;
    }
  } catch (err) {
    removeTypingIndicator(panes.compress);
    removeTypingIndicator(panes.full);
    const durationMs = Math.round(performance.now() - started);
    logExchange({
      requestBody,
      status: 0,
      responseBody: { error: "Не удалось связаться с сервером", details: String(err) },
      durationMs,
    });
    createMessage(panes.compress, "assistant", "Не удалось связаться с сервером.", "message--error");
    createMessage(panes.full, "assistant", "Не удалось связаться с сервером.", "message--error");
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

clearBtn.addEventListener("click", async () => {
  try {
    await fetch("/api/history", { method: "DELETE" });
  } catch {
    /* still clear UI */
  }
  resetPaneUI(panes.compress);
  resetPaneUI(panes.full);
  if (appSubtitle) appSubtitle.textContent = "Enter — отправить в оба чата";
  inputEl.focus();
});

providerSelect.addEventListener("change", () => {
  localStorage.setItem(PROVIDER_STORAGE_KEY, currentProvider());
  updateProviderLabel();
});

debugToggle.addEventListener("change", () => {
  setDebugMode(debugToggle.checked);
});

clearDebugBtn.addEventListener("click", clearDebugLog);

setDebugMode(debugEnabled);
Promise.all([loadProviders(), loadHistory()]).then(() => inputEl.focus());
