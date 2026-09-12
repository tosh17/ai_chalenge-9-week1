const chatEl = document.getElementById("chat");
const welcomeEl = document.getElementById("welcome");
const formEl = document.getElementById("chatForm");
const inputEl = document.getElementById("messageInput");
const sendBtn = document.getElementById("sendBtn");
const clearBtn = document.getElementById("clearBtn");
const designBtn = document.getElementById("designBtn");
const tokenDemoBtn = document.getElementById("tokenDemoBtn");
const layoutEl = document.getElementById("layout");
const debugToggle = document.getElementById("debugToggle");
const debugPanel = document.getElementById("debugPanel");
const debugLog = document.getElementById("debugLog");
const clearDebugBtn = document.getElementById("clearDebugBtn");
const providerSelect = document.getElementById("providerSelect");
const providerLabel = document.getElementById("providerLabel");
const appTitle = document.getElementById("appTitle");
const welcomeTitle = document.getElementById("welcomeTitle");
const welcomeText = document.getElementById("welcomeText");
const appSubtitle = document.getElementById("appSubtitle");

const DEBUG_STORAGE_KEY = "deepseek-chat-debug-day8";
const PROVIDER_STORAGE_KEY = "day8-chat-provider";
const tokenMeterText = document.getElementById("tokenMeterText");
const tokenBarFill = document.getElementById("tokenBarFill");

/** @type {{role: string, content: string}[]} */
let history = [];
let isLoading = false;
let debugEnabled = localStorage.getItem(DEBUG_STORAGE_KEY) === "true";
let debugEntryCount = 0;
/** @type {{id: string, title: string, model: string}[]} */
let providers = [];

const DEFAULT_DESIGN_WISH =
  "Сделай веб-чат в ярком мультяшном стиле американского ситкома 90-х: жёлтый, голубое небо, толстые чёрные контуры, комикс, весело и дерзко, без копирования чужих персонажей.";

function hideWelcome() {
  if (welcomeEl) welcomeEl.style.display = "none";
}

function scrollToBottom() {
  chatEl.scrollTop = chatEl.scrollHeight;
}

function formatDuration(ms) {
  if (ms == null || Number.isNaN(ms)) return "";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

function createMessage(role, content, extraClass = "", meta = {}) {
  hideWelcome();

  const isUser = role === "user";
  const el = document.createElement("div");
  el.className = `message message--${role} ${extraClass}`.trim();

  const timeLabel = !isUser && meta.durationMs != null
    ? `<span class="message__time" title="время ответа">${escapeHTML(formatDuration(meta.durationMs))}</span>`
    : "";

  el.innerHTML = `
    <div class="message__avatar">${isUser ? "Вы" : "AI"}</div>
    <div class="message__body">
      <div class="message__bubble">${escapeHTML(content)}</div>
      ${timeLabel}
    </div>
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
    <div class="message__body">
      <div class="message__bubble">
        <span class="typing-dot"></span>
        <span class="typing-dot"></span>
        <span class="typing-dot"></span>
      </div>
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
  providerSelect.disabled = loading;
  designBtn.disabled = loading;
  if (tokenDemoBtn) tokenDemoBtn.disabled = loading;
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

function logExchange({ requestBody, status, responseBody, durationMs, upstream, url = "/api/chat" }) {
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
    ${appendDebugBlock(`→ ${url}`, {
      method: "POST",
      url,
      body: requestBody,
    })}
    ${appendDebugBlock("← ответ", responseBody, status >= 400 ? "debug-block__code--error" : "")}
    ${upstream ? appendDebugBlock("↔ upstream", upstream) : ""}
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

function updateTokenMeter(tokens, session) {
  if (!tokenMeterText || !tokenBarFill) return;
  if (!tokens) {
    tokenMeterText.textContent = "токены: —";
    tokenBarFill.style.width = "0%";
    tokenBarFill.className = "token-meter__fill";
    return;
  }

  const pct = Math.min(100, Math.max(0, tokens.context_pct || 0));
  tokenBarFill.style.width = `${pct}%`;
  tokenBarFill.className = "token-meter__fill";
  if (pct >= 90 || tokens.over_limit) tokenBarFill.classList.add("token-meter__fill--danger");
  else if (pct >= 60) tokenBarFill.classList.add("token-meter__fill--warn");

  const sess = session
    ? ` · сессия: ${session.total_tokens || 0} tok / ${formatCost(session.estimated_cost_usd)}`
    : "";
  tokenMeterText.textContent =
    `запрос ${tokens.request || 0} · история ${tokens.history || 0} · ответ ${tokens.completion || 0}` +
    ` · prompt ${tokens.prompt || 0}/${tokens.context_limit || "?"} (${pct.toFixed(1)}%)` +
    ` · ${formatCost(tokens.cost_usd)}${sess}`;
}

async function loadHistory() {
  try {
    const res = await fetch("/api/history");
    if (!res.ok) return;
    const data = await res.json();
    const messages = Array.isArray(data.messages) ? data.messages : [];
    history = messages
      .filter((m) => m && (m.role === "user" || m.role === "assistant") && m.content)
      .map((m) => ({ role: m.role, content: m.content }));

    updateTokenMeter(data.tokens, data.session);

    if (!history.length) return;

    for (const m of history) {
      createMessage(m.role, m.content);
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

function applyTheme(cssVars, design) {
  const root = document.documentElement;
  if (cssVars && typeof cssVars === "object") {
    for (const [key, value] of Object.entries(cssVars)) {
      if (value) root.style.setProperty(key, value);
    }
  }
  if (design?.title) appTitle.textContent = design.title;
  if (design?.welcome_title) welcomeTitle.textContent = design.welcome_title;
  if (design?.welcome_text) welcomeText.textContent = design.welcome_text;
  if (design?.subtitle) appSubtitle.textContent = design.subtitle;
}

async function runTokenDemo() {
  setLoading(true);
  createMessage("user", "Покажи на трёх примерах, как растут токены и что бывает при переполнении");
  createTypingIndicator();
  const requestBody = { provider: currentProvider(), limit: 4096 };
  const started = performance.now();

  try {
    const res = await fetch("/api/token-demo", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
    });
    const data = await res.json();
    const durationMs = Math.round(performance.now() - started);
    removeTypingIndicator();

    logExchange({
      url: "/api/token-demo",
      requestBody,
      status: res.status,
      responseBody: data,
      durationMs,
    });

    if (!res.ok) {
      createMessage("assistant", data.error || "Демо токенов упало", "message--error", { durationMs });
      return;
    }

    const table = data.table || JSON.stringify(data.rows || [], null, 2);
    createMessage("assistant", table, "message--mono", { durationMs });

    // покажем последнюю успешную/overflow строку на meter
    const rows = Array.isArray(data.rows) ? data.rows : [];
    const last = [...rows].reverse().find((r) => r) || null;
    if (last) {
      updateTokenMeter(
        {
          request: last.request_tokens,
          history: last.history_tokens,
          completion: last.completion_tokens,
          prompt: last.prompt_tokens,
          total: last.total_tokens,
          context_limit: data.demo_limit,
          context_pct: last.context_pct,
          over_limit: last.over_limit,
          cost_usd: last.cost_usd,
        },
        {
          total_tokens: rows.reduce((s, r) => s + (r.total_tokens || 0), 0),
          estimated_cost_usd: rows.reduce((s, r) => s + (r.cost_usd || 0), 0),
        }
      );
    }
  } catch (err) {
    removeTypingIndicator();
    createMessage("assistant", "Не удалось запустить демо токенов.", "message--error");
  } finally {
    setLoading(false);
    inputEl.focus();
  }
}

async function runDesignCrew() {
  const wish = window.prompt("Что хотят агенты-дизайнеры?", DEFAULT_DESIGN_WISH);
  if (wish === null) return;

  setLoading(true);
  const requestBody = { wish: wish.trim() || DEFAULT_DESIGN_WISH, provider: currentProvider() };
  const started = performance.now();

  try {
    const res = await fetch("/api/design", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
    });
    const data = await res.json();
    const durationMs = Math.round(performance.now() - started);

    logExchange({
      url: "/api/design",
      requestBody,
      status: res.status,
      responseBody: data,
      durationMs,
    });

    if (!res.ok) {
      createMessage("assistant", data.error || "Дизайн-команда упала", "message--error");
      return;
    }

    applyTheme(data.css_vars, data.design);

    const steps = (data.steps || [])
      .map((s, i) => `${i + 1}. ${s.agent}`)
      .join(" → ");
    createMessage(
      "assistant",
      `Готово!\nЦепочка: ${steps}\nТема: ${data.design?.theme_name || "без имени"}\n\nПромпт:\n${data.prompt || ""}`
    );
  } catch (err) {
    createMessage("assistant", "Не удалось связаться с дизайн-командой.", "message--error");
    logExchange({
      url: "/api/design",
      requestBody,
      status: 0,
      responseBody: { error: String(err) },
      durationMs: Math.round(performance.now() - started),
    });
  } finally {
    setLoading(false);
    inputEl.focus();
  }
}

async function sendMessage(text) {
  createMessage("user", text);

  setLoading(true);
  createTypingIndicator();

  // История на сервере: агент сам подставляет сохранённый контекст.
  const requestBody = {
    message: text,
    provider: currentProvider(),
  };
  const started = performance.now();

  try {
    const headers = { "Content-Type": "application/json" };
    if (debugEnabled) headers["X-Debug"] = "true";

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
      updateTokenMeter(data.tokens, data.session);
      createMessage("assistant", data.error || "Неизвестная ошибка", "message--error", {
        durationMs: data.duration_ms ?? durationMs,
      });
      return;
    }

    createMessage("assistant", data.reply, "", {
      durationMs: data.duration_ms ?? durationMs,
    });
    history.push({ role: "user", content: text });
    history.push({ role: "assistant", content: data.reply });
    updateTokenMeter(data.tokens, data.session);
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
  history = [];
  chatEl.innerHTML = "";
  if (welcomeEl) {
    welcomeEl.style.display = "";
    chatEl.appendChild(welcomeEl);
  }
  updateTokenMeter(null, null);
  inputEl.focus();
});

designBtn.addEventListener("click", () => {
  if (!isLoading) runDesignCrew();
});

tokenDemoBtn?.addEventListener("click", () => {
  if (!isLoading) runTokenDemo();
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
