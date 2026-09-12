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
const providerSelect = document.getElementById("providerSelect");
const providerLabel = document.getElementById("providerLabel");
const strategyLabel = document.getElementById("strategyLabel");
const appSubtitle = document.getElementById("appSubtitle");
const tokenMeterText = document.getElementById("tokenMeterText");
const tokenBarFill = document.getElementById("tokenBarFill");
const settingsBtn = document.getElementById("settingsBtn");
const settingsPanel = document.getElementById("settingsPanel");
const settingsCloseBtn = document.getElementById("settingsCloseBtn");
const saveStrategyBtn = document.getElementById("saveStrategyBtn");
const compareBtn = document.getElementById("compareBtn");
const slidingN = document.getElementById("slidingN");
const factsN = document.getElementById("factsN");
const branchN = document.getElementById("branchN");
const factsPre = document.getElementById("factsPre");
const branchBar = document.getElementById("branchBar");
const branchList = document.getElementById("branchList");
const forkBtn = document.getElementById("forkBtn");

const DEBUG_STORAGE_KEY = "deepseek-chat-debug-day10";
const PROVIDER_STORAGE_KEY = "day10-chat-provider";

/** @type {{role: string, content: string}[]} */
let history = [];
let isLoading = false;
let debugEnabled = localStorage.getItem(DEBUG_STORAGE_KEY) === "true";
let debugEntryCount = 0;
/** @type {{id: string, title: string, model: string, context_limit?: number}[]} */
let providers = [];
let currentStrategy = { kind: "sliding", sliding_window_n: 8, facts_window_n: 6, branch_window_n: 0 };

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

function escapeHTML(text) {
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
}

function createMessage(role, content, extraClass = "", meta = {}) {
  hideWelcome();
  const isUser = role === "user";
  const isSystem = role === "system";
  const el = document.createElement("div");
  el.className = `message message--${role} ${extraClass}`.trim();
  let avatar = "AI";
  if (isUser) avatar = "Вы";
  if (isSystem) avatar = "⚙";
  const timeLabel = !isUser && !isSystem && meta.durationMs != null
    ? `<span class="message__time">${escapeHTML(formatDuration(meta.durationMs))}</span>`
    : "";
  el.innerHTML = `
    <div class="message__avatar">${avatar}</div>
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
    <div class="message__body"><div class="message__bubble">
      <span class="typing-dot"></span><span class="typing-dot"></span><span class="typing-dot"></span>
    </div></div>`;
  chatEl.appendChild(el);
  scrollToBottom();
}

function removeTypingIndicator() {
  document.getElementById("typingIndicator")?.remove();
}

function setLoading(loading) {
  isLoading = loading;
  sendBtn.disabled = loading;
  inputEl.disabled = loading;
  providerSelect.disabled = loading;
  if (compareBtn) compareBtn.disabled = loading;
  if (forkBtn) forkBtn.disabled = loading;
  if (saveStrategyBtn) saveStrategyBtn.disabled = loading;
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

function setSettingsOpen(open) {
  layoutEl.classList.toggle("layout--settings", open);
  settingsPanel.setAttribute("aria-hidden", open ? "false" : "true");
}

function clearDebugLog() {
  debugEntryCount = 0;
  debugLog.innerHTML = '<p class="debug-panel__empty">Запросы появятся здесь.</p>';
}

function appendDebugBlock(title, payload, extraClass = "") {
  const pre = document.createElement("pre");
  pre.className = `debug-block__code ${extraClass}`.trim();
  pre.textContent = formatJSON(payload);
  return `<div class="debug-block"><div class="debug-block__title">${escapeHTML(title)}</div>${pre.outerHTML}</div>`;
}

function logExchange({ requestBody, status, responseBody, durationMs, url = "/api/chat" }) {
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

function updateTokenMeter(tokens, session) {
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
    `prompt ${tokens.prompt || 0}/${tokens.context_limit || "?"} (${pct.toFixed(1)}%)` +
    ` · out ${tokens.completion || 0} · ${formatCost(tokens.cost_usd)}${sess}`;
}

function updateStrategyFieldsVisibility() {
  const kind = document.querySelector('input[name="strategyKind"]:checked')?.value || "sliding";
  document.getElementById("fieldSliding").style.display = kind === "sliding" ? "" : "none";
  document.getElementById("fieldFacts").style.display = kind === "facts" ? "" : "none";
  document.getElementById("fieldBranch").style.display = kind === "branch" ? "" : "none";
  branchBar.hidden = kind !== "branch";
}

function applyStrategyToForm(st) {
  currentStrategy = st || currentStrategy;
  const kind = currentStrategy.kind || "sliding";
  const radio = document.querySelector(`input[name="strategyKind"][value="${kind}"]`);
  if (radio) radio.checked = true;
  slidingN.value = currentStrategy.sliding_window_n || 8;
  factsN.value = currentStrategy.facts_window_n || 6;
  branchN.value = currentStrategy.branch_window_n ?? 0;
  strategyLabel.textContent = kind;
  updateStrategyFieldsVisibility();
  updateHint();
}

function updateHint() {
  const kind = currentStrategy.kind || "sliding";
  if (kind === "sliding") {
    appSubtitle.textContent = `Sliding: храним только последние ${currentStrategy.sliding_window_n} сообщ.`;
  } else if (kind === "facts") {
    appSubtitle.textContent = `Facts: KV-память + последние ${currentStrategy.facts_window_n} сообщ.`;
  } else {
    appSubtitle.textContent = `Branching: checkpoint и независимые ветки (окно ${currentStrategy.branch_window_n || "вся"})`;
  }
}

function renderFacts(facts) {
  if (!facts || !Object.keys(facts).length) {
    factsPre.textContent = "—";
    return;
  }
  factsPre.textContent = Object.entries(facts)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, v]) => `${k}: ${v}`)
    .join("\n");
}

function renderBranches(branches, active) {
  branchList.innerHTML = "";
  if (!Array.isArray(branches) || !branches.length) {
    branchList.innerHTML = '<span class="branch-bar__empty">нет веток — сделай checkpoint</span>';
    return;
  }
  for (const b of branches) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = `btn btn--ghost btn--sm branch-chip${b.active || b.id === active ? " branch-chip--active" : ""}`;
    btn.textContent = `${b.title || b.id} (${b.messages || 0})`;
    btn.addEventListener("click", () => switchBranch(b.id));
    branchList.appendChild(btn);
  }
}

function renderFactEvents(events) {
  if (!Array.isArray(events)) return;
  for (const ev of events) {
    const tok = ev.total_tokens || 0;
    createMessage(
      "system",
      `Обновлены sticky facts (${Object.keys(ev.facts || {}).length} ключей). На извлечение: ${tok} tok · ${formatCost(ev.cost_usd)}`,
      "message--system",
    );
    renderFacts(ev.facts);
  }
}

function renderStrategyMeta(strategy) {
  if (!strategy) return;
  if (strategy.discarded_messages > 0 && strategy.kind === "sliding") {
    createMessage(
      "system",
      `Sliding: отброшено ${strategy.discarded_messages} старых сообщений. В окне осталось ${strategy.messages_in_prompt}.`,
      "message--system",
    );
  }
  if (strategy.facts) renderFacts(strategy.facts);
}

async function loadStrategy() {
  try {
    const res = await fetch("/api/strategy");
    if (!res.ok) return;
    const data = await res.json();
    applyStrategyToForm(data.strategy);
    renderFacts(data.facts);
    renderBranches(data.branches, data.active_branch);
  } catch {
    /* ignore */
  }
}

async function saveStrategy() {
  const kind = document.querySelector('input[name="strategyKind"]:checked')?.value || "sliding";
  const body = {
    kind,
    sliding_window_n: Number(slidingN.value) || 8,
    facts_window_n: Number(factsN.value) || 6,
    branch_window_n: Number(branchN.value) || 0,
  };
  setLoading(true);
  try {
    const res = await fetch("/api/strategy", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    if (!res.ok) {
      createMessage("assistant", data.error || "Не удалось сохранить стратегию", "message--error");
      return;
    }
    applyStrategyToForm(data.strategy);
    renderFacts(data.facts);
    renderBranches(data.branches, data.active_branch);
    createMessage("system", `Стратегия: ${data.strategy.kind}`, "message--system");
  } catch {
    createMessage("assistant", "Не удалось сохранить стратегию", "message--error");
  } finally {
    setLoading(false);
  }
}

async function forkBranches() {
  setLoading(true);
  try {
    const res = await fetch("/api/branch/fork", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title_a: "Ветка A", title_b: "Ветка B" }),
    });
    const data = await res.json();
    if (!res.ok) {
      createMessage("assistant", data.error || "Fork не удался", "message--error");
      return;
    }
    renderBranches(data.branches, data.active_branch);
    createMessage(
      "system",
      `Checkpoint @${data.checkpoint_at ?? "?"} · созданы ветки A/B. Активна: ${data.active_branch}`,
      "message--system",
    );
  } catch {
    createMessage("assistant", "Fork не удался", "message--error");
  } finally {
    setLoading(false);
  }
}

async function switchBranch(id) {
  setLoading(true);
  try {
    const res = await fetch("/api/branch/switch", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ branch: id }),
    });
    const data = await res.json();
    if (!res.ok) {
      createMessage("assistant", data.error || "Не удалось переключить ветку", "message--error");
      return;
    }
    history = [];
    chatEl.innerHTML = "";
    if (welcomeEl) {
      welcomeEl.style.display = "";
      chatEl.appendChild(welcomeEl);
    }
    const messages = Array.isArray(data.messages) ? data.messages : [];
    for (const m of messages) {
      if (m?.role && m?.content) createMessage(m.role, m.content);
    }
    history = messages.filter((m) => m.role === "user" || m.role === "assistant");
    renderBranches(data.branches, data.active_branch);
    renderFacts(data.facts);
    updateTokenMeter(data.tokens, data.session);
    createMessage("system", `Переключились на ветку ${data.active_branch}`, "message--system");
  } catch {
    createMessage("assistant", "Не удалось переключить ветку", "message--error");
  } finally {
    setLoading(false);
  }
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
    if (data.strategy) applyStrategyToForm(data.strategy);
    renderFacts(data.facts);
    renderBranches(data.branches, data.active_branch);
    if (!history.length) return;
    for (const m of history) createMessage(m.role, m.content);
  } catch {
    /* empty */
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
    /* defaults */
  }
}

async function sendMessage(text) {
  createMessage("user", text);
  setLoading(true);
  createTypingIndicator();
  const requestBody = { message: text, provider: currentProvider() };
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
    logExchange({ requestBody, status: res.status, responseBody: data, durationMs });
    if (!res.ok) {
      updateTokenMeter(data.tokens, data.session);
      createMessage("assistant", data.error || "Ошибка", "message--error", {
        durationMs: data.duration_ms ?? durationMs,
      });
      return;
    }
    renderFactEvents(data.fact_events);
    createMessage("assistant", data.reply, "", { durationMs: data.duration_ms ?? durationMs });
    renderStrategyMeta(data.strategy);
    history.push({ role: "user", content: text });
    history.push({ role: "assistant", content: data.reply });
    updateTokenMeter(data.tokens, data.session);
    if (data.strategy) {
      currentStrategy.kind = data.strategy.kind || currentStrategy.kind;
      strategyLabel.textContent = currentStrategy.kind;
    }
    await loadStrategy();
  } catch (err) {
    removeTypingIndicator();
    logExchange({
      requestBody,
      status: 0,
      responseBody: { error: String(err) },
      durationMs: Math.round(performance.now() - started),
    });
    createMessage("assistant", "Не удалось связаться с сервером.", "message--error");
  } finally {
    setLoading(false);
    inputEl.focus();
  }
}

async function runCompare() {
  setLoading(true);
  createMessage("user", "Сравни 3 стратегии на сценарии «собираем ТЗ»");
  createTypingIndicator();
  const requestBody = { provider: currentProvider() };
  const started = performance.now();
  try {
    const res = await fetch("/api/strategy/compare", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
    });
    const data = await res.json();
    const durationMs = Math.round(performance.now() - started);
    removeTypingIndicator();
    logExchange({ url: "/api/strategy/compare", requestBody, status: res.status, responseBody: data, durationMs });
    if (!res.ok) {
      createMessage("assistant", data.error || "Сравнение упало", "message--error", { durationMs });
      return;
    }
    createMessage("assistant", data.report || JSON.stringify(data, null, 2), "message--mono", { durationMs });
  } catch {
    removeTypingIndicator();
    createMessage("assistant", "Не удалось сравнить стратегии.", "message--error");
  } finally {
    setLoading(false);
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
    /* ui clear anyway */
  }
  history = [];
  chatEl.innerHTML = "";
  if (welcomeEl) {
    welcomeEl.style.display = "";
    chatEl.appendChild(welcomeEl);
  }
  updateTokenMeter(null, null);
  renderFacts(null);
  renderBranches([], "");
  inputEl.focus();
});

settingsBtn.addEventListener("click", () => setSettingsOpen(!layoutEl.classList.contains("layout--settings")));
settingsCloseBtn.addEventListener("click", () => setSettingsOpen(false));
saveStrategyBtn.addEventListener("click", saveStrategy);
compareBtn.addEventListener("click", () => {
  if (!isLoading) runCompare();
});
forkBtn.addEventListener("click", () => {
  if (!isLoading) forkBranches();
});

document.querySelectorAll('input[name="strategyKind"]').forEach((el) => {
  el.addEventListener("change", updateStrategyFieldsVisibility);
});

providerSelect.addEventListener("change", () => {
  localStorage.setItem(PROVIDER_STORAGE_KEY, currentProvider());
  updateProviderLabel();
});
debugToggle.addEventListener("change", () => setDebugMode(debugToggle.checked));
clearDebugBtn.addEventListener("click", clearDebugLog);

setDebugMode(debugEnabled);
Promise.all([loadProviders(), loadStrategy(), loadHistory()]).then(() => inputEl.focus());
