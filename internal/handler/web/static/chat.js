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
const injectSTM = document.getElementById("injectSTM");
const injectWM = document.getElementById("injectWM");
const injectLTM = document.getElementById("injectLTM");
const stmWindow = document.getElementById("stmWindow");
const stmMeta = document.getElementById("stmMeta");
const stmPre = document.getElementById("stmPre");
const wmMeta = document.getElementById("wmMeta");
const wmPre = document.getElementById("wmPre");
const ltmMeta = document.getElementById("ltmMeta");
const ltmPre = document.getElementById("ltmPre");
const routeList = document.getElementById("routeList");
const clearStmBtn = document.getElementById("clearStmBtn");
const clearWmBtn = document.getElementById("clearWmBtn");
const clearLtmBtn = document.getElementById("clearLtmBtn");
const netBanner = document.getElementById("netBanner");
const netBannerText = document.getElementById("netBannerText");
const netRetryBtn = document.getElementById("netRetryBtn");

const DEBUG_STORAGE_KEY = "deepseek-chat-debug-day11";
const PROVIDER_STORAGE_KEY = "day11-chat-provider";
const UNSENT_STORAGE_KEY = "day11-unsent-messages";

/** @type {{role: string, content: string}[]} */
let history = [];
let isLoading = false;
let debugEnabled = localStorage.getItem(DEBUG_STORAGE_KEY) === "true";
let debugEntryCount = 0;
/** @type {{id: string, title: string, model: string, context_limit?: number}[]} */
let providers = [];
let currentPolicy = { stm_window_n: 8, inject_stm: true, inject_wm: true, inject_ltm: true };
/** @type {{text: string, userEl: HTMLElement, errorEl: HTMLElement}[]} */
let pendingSends = [];
let retryingQueue = false;

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
  if (saveStrategyBtn) saveStrategyBtn.disabled = loading;
  [clearStmBtn, clearWmBtn, clearLtmBtn].forEach((b) => {
    if (b) b.disabled = loading;
  });
  document.querySelectorAll(".message__retry").forEach((b) => {
    b.disabled = loading;
  });
  if (netRetryBtn) netRetryBtn.disabled = loading || pendingSends.length === 0;
}

function autoResizeTextarea() {
  inputEl.style.height = "auto";
  inputEl.style.height = Math.min(inputEl.scrollHeight, 88) + "px";
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
  layoutEl.classList.toggle("layout--nav", open);
  if (settingsPanel) settingsPanel.setAttribute("aria-hidden", open ? "false" : "true");
}

function clearDebugLog() {
  debugEntryCount = 0;
  debugLog.innerHTML = '<p class="debug-panel__empty">Запросы появятся здесь.</p>';
}

function appendDebugBlock(title, payload, extraClass = "") {
  const pre = document.createElement("pre");
  pre.className = `debug-block__code ${extraClass}`.trim();
  pre.textContent = typeof payload === "string" ? payload : formatJSON(payload);
  return `<div class="debug-block"><div class="debug-block__title">${escapeHTML(title)}</div>${pre.outerHTML}</div>`;
}

function routerDebugBlocks(responseBody) {
  const routes = Array.isArray(responseBody?.route_events) ? responseBody.route_events : [];
  let html = "";
  routes.forEach((ev, i) => {
    if (!ev || (!ev.system && !ev.prompt && !ev.raw_reply && !ev.error)) return;
    const n = routes.length > 1 ? ` #${i + 1}` : "";
    html += appendDebugBlock(`роутер${n} → system`, ev.system || "—");
    html += appendDebugBlock(`роутер${n} → user`, ev.prompt || "—");
    if (ev.error) {
      html += appendDebugBlock(`роутер${n} ✕ ошибка`, ev.error, "debug-block__code--error");
    }
    html += appendDebugBlock(
      `роутер${n} ← JSON`,
      ev.raw_reply || "нет ответа",
      ev.raw_reply ? "" : "debug-block__code--error",
    );
    html += appendDebugBlock(`роутер${n} ← запись`, {
      source: ev.source,
      reasons: ev.reasons,
      working: ev.working || null,
      long_term: ev.long_term || null,
    });
  });
  return html;
}

function chatResponseForLog(responseBody) {
  if (!responseBody || !Array.isArray(responseBody.route_events)) return responseBody;
  return {
    ...responseBody,
    route_events: responseBody.route_events.map((ev) => {
      const copy = { ...ev };
      delete copy.system;
      delete copy.prompt;
      delete copy.raw_reply;
      return copy;
    }),
  };
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
    ${routerDebugBlocks(responseBody)}
    ${appendDebugBlock(`→ чат ${url}`, { method: "POST", url, body: requestBody })}
    ${appendDebugBlock("← чат ответ", chatResponseForLog(responseBody), status >= 400 ? "debug-block__code--error" : "")}
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

function policyLabel(p) {
  const on = [];
  if (p.inject_ltm) on.push("LTM");
  if (p.inject_wm) on.push("WM");
  if (p.inject_stm) on.push(`STM/${p.stm_window_n || 8}`);
  return on.length ? on.join("+") : "слои выкл";
}

function applyPolicyToForm(p) {
  currentPolicy = p || currentPolicy;
  injectSTM.checked = currentPolicy.inject_stm !== false;
  injectWM.checked = currentPolicy.inject_wm !== false;
  injectLTM.checked = currentPolicy.inject_ltm !== false;
  stmWindow.value = currentPolicy.stm_window_n || 8;
  strategyLabel.textContent = policyLabel(currentPolicy);
  if (appSubtitle) appSubtitle.textContent = "#задача · #профиль · #решение · #запомни";
}

function formatSTM(messages) {
  if (!Array.isArray(messages) || !messages.length) return "диалог пуст";
  return messages
    .slice(-8)
    .map((m) => `${m.role}: ${m.content}`)
    .join("\n");
}

function formatWM(w) {
  if (!w || (!w.goal && !(w.notes || []).length && !(w.constraints || []).length)) return "нет активной задачи";
  const lines = [];
  if (w.status) lines.push(`статус: ${w.status}`);
  if (w.goal) lines.push(`цель: ${w.goal}`);
  (w.constraints || []).forEach((c) => lines.push(`огр.: ${c}`));
  (w.notes || []).forEach((n) => lines.push(`заметка: ${n}`));
  Object.entries(w.artifacts || {}).forEach(([k, v]) => lines.push(`${k}: ${v}`));
  return lines.join("\n") || "—";
}

function formatLTM(lt) {
  if (!lt) return "пусто";
  const lines = [];
  const p = lt.profile || {};
  if (p.name) lines.push(`имя: ${p.name}`);
  if (p.language) lines.push(`язык: ${p.language}`);
  Object.entries(p.preferences || {}).forEach(([k, v]) => lines.push(`pref.${k}: ${v}`));
  (lt.decisions || []).forEach((d) => lines.push(`решение ${d.key}: ${d.value}`));
  (lt.knowledge || []).forEach((k) => lines.push(`[${k.topic}] ${k.fact}`));
  return lines.join("\n") || "пусто";
}

function renderMemory(mem) {
  if (!mem) return;
  const stm = mem.stm_preview || mem.short_term || [];
  const count = mem.short_term_count ?? stm.length;
  stmMeta.textContent = `${count} сообщ. · окно ${mem.policy?.stm_window_n || currentPolicy.stm_window_n || 8}`;
  stmPre.textContent = formatSTM(stm);
  const wm = mem.working || {};
  wmMeta.textContent = wm.goal ? `${wm.status || "active"} · есть задача` : "нет задачи";
  wmPre.textContent = formatWM(wm);
  const lt = mem.long_term || {};
  const bits = [];
  if (lt.profile?.name) bits.push(lt.profile.name);
  bits.push(`реш. ${(lt.decisions || []).length}`);
  bits.push(`знан. ${(lt.knowledge || []).length}`);
  ltmMeta.textContent = bits.join(" · ");
  ltmPre.textContent = formatLTM(lt);
  if (mem.policy) applyPolicyToForm(mem.policy);
}

function renderRouteEvents(events) {
  if (!Array.isArray(events) || !events.length) return;
  routeList.innerHTML = "";
  for (const ev of events) {
    const wrote = [];
    if (ev.wrote_stm) wrote.push("STM");
    if (ev.wrote_wm) wrote.push("WM");
    if (ev.wrote_ltm) wrote.push("LTM");
    const chip = document.createElement("span");
    chip.className = "route-chip";
    chip.textContent = `${ev.source || "route"} → ${wrote.join("+") || "только STM позже"}`;
    routeList.appendChild(chip);
    (ev.reasons || []).forEach((reason) => {
      createMessage("system", reason, "message--system");
    });
    if (ev.discarded > 0) {
      createMessage("system", `STM: вытеснено ${ev.discarded} старых реплик.`, "message--system");
    }
  }
}

function renderMemoryMeta(mem) {
  if (!mem) return;
  renderMemory(mem);
  if (mem.discarded > 0) {
    createMessage("system", `STM-окно: вытеснено ${mem.discarded} реплик. В слое осталось ${mem.short_term_count}.`, "message--system");
  }
}

async function loadMemory() {
  try {
    const res = await fetch("/api/memory");
    if (!res.ok) return;
    const data = await res.json();
    if (data.policy) applyPolicyToForm(data.policy);
    renderMemory(data.memory);
  } catch {
    /* ignore */
  }
}

async function savePolicy() {
  const body = {
    stm_window_n: Number(stmWindow.value) || 8,
    inject_stm: injectSTM.checked,
    inject_wm: injectWM.checked,
    inject_ltm: injectLTM.checked,
  };
  setLoading(true);
  try {
    const res = await fetch("/api/memory/policy", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    if (!res.ok) {
      createMessage("assistant", data.error || "Не удалось сохранить политику", "message--error");
      return;
    }
    applyPolicyToForm(data.policy);
    renderMemory(data.memory);
    createMessage("system", `Политика памяти: ${policyLabel(data.policy)}`, "message--system");
  } catch {
    createMessage("assistant", "Не удалось сохранить политику", "message--error");
  } finally {
    setLoading(false);
  }
}

async function clearLayer(layer, reloadChat) {
  setLoading(true);
  try {
    const res = await fetch(`/api/memory/${layer}`, { method: "DELETE" });
    const data = await res.json();
    if (!res.ok) {
      createMessage("assistant", data.error || "Не удалось очистить слой", "message--error");
      return;
    }
    renderMemory(data.memory);
    if (data.policy) applyPolicyToForm(data.policy);
    if (reloadChat) {
      history = [];
      chatEl.innerHTML = "";
      if (welcomeEl) {
        welcomeEl.style.display = "";
        chatEl.appendChild(welcomeEl);
      }
      updateTokenMeter(null, null);
    }
    createMessage("system", `Очищен слой: ${layer}`, "message--system");
  } catch {
    createMessage("assistant", "Не удалось очистить слой", "message--error");
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
    if (data.memory) renderMemory(data.memory);
    if (data.memory_policy) applyPolicyToForm(data.memory_policy);
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

function isBrowserOffline() {
  return typeof navigator !== "undefined" && navigator.onLine === false;
}

function isRetryableFailure(status, err, data) {
  if (isBrowserOffline()) return true;
  if (!status) return true;
  if (status === 429 || status === 502 || status === 503 || status === 504) return true;
  const msg = String((data && data.error) || (err && err.message) || err || "").toLowerCase();
  return /failed to fetch|networkerror|connection reset|reset by peer|i\/o timeout|no route|connection refused|tls handshake|unavailable|timeout|eof/.test(msg);
}

function persistUnsent() {
  try {
    sessionStorage.setItem(UNSENT_STORAGE_KEY, JSON.stringify(pendingSends.map((p) => p.text)));
  } catch {
    /* ignore quota */
  }
}

function updateNetBanner() {
  if (!netBanner || !netBannerText) return;
  const offline = isBrowserOffline();
  if (!offline && pendingSends.length === 0) {
    netBanner.hidden = true;
    return;
  }
  netBanner.hidden = false;
  if (offline && pendingSends.length) {
    netBannerText.textContent = `Нет интернета · не отправлено: ${pendingSends.length}`;
  } else if (offline) {
    netBannerText.textContent = "Нет интернета. Сообщение можно отправить позже.";
  } else {
    netBannerText.textContent = `Нет связи · не отправлено: ${pendingSends.length}`;
  }
  if (netRetryBtn) netRetryBtn.disabled = isLoading || pendingSends.length === 0;
}

function dropPending(userEl) {
  const item = pendingSends.find((p) => p.userEl === userEl);
  if (item?.errorEl) item.errorEl.remove();
  pendingSends = pendingSends.filter((p) => p.userEl !== userEl);
  userEl.classList.remove("message--unsent");
  persistUnsent();
  updateNetBanner();
}

function showNetworkError(text, userEl, message) {
  dropPending(userEl);
  userEl.classList.add("message--unsent");
  const errorEl = document.createElement("div");
  errorEl.className = "message message--assistant message--error message--retry";
  errorEl.innerHTML = `
    <div class="message__avatar">AI</div>
    <div class="message__body">
      <div class="message__bubble">${escapeHTML(message)}</div>
      <button type="button" class="btn btn--accent btn--sm message__retry">Повторить</button>
    </div>
  `;
  const btn = errorEl.querySelector(".message__retry");
  btn.disabled = isLoading;
  btn.addEventListener("click", () => {
    if (isLoading) return;
    sendMessage(text, { userEl, errorEl });
  });
  chatEl.appendChild(errorEl);
  pendingSends.push({ text, userEl, errorEl });
  persistUnsent();
  updateNetBanner();
  scrollToBottom();
}

function networkErrorText(err, data) {
  if (isBrowserOffline()) {
    return "Нет интернета. Сообщение не отправлено — нажмите «Повторить» или дождитесь сети.";
  }
  const raw = (data && data.error) || (err && err.message) || String(err || "");
  if (/failed to fetch|networkerror/i.test(String(err))) {
    return "Нет связи с сервером. Проверьте интернет и нажмите «Повторить».";
  }
  if (raw) {
    return `${raw}\nМожно повторить отправку.`;
  }
  return "Сеть недоступна. Можно повторить отправку.";
}

function restoreUnsent() {
  let texts = [];
  try {
    texts = JSON.parse(sessionStorage.getItem(UNSENT_STORAGE_KEY) || "[]");
  } catch {
    texts = [];
  }
  if (!Array.isArray(texts) || !texts.length) return;
  for (const text of texts) {
    if (typeof text !== "string" || !text.trim()) continue;
    const userEl = createMessage("user", text);
    showNetworkError(text, userEl, "Сообщение не отправлено. Повторите, когда будет сеть.");
  }
}

async function retryPendingSends() {
  if (retryingQueue || isLoading || !pendingSends.length) return;
  retryingQueue = true;
  try {
    while (pendingSends.length) {
      if (isBrowserOffline()) break;
      const next = pendingSends[0];
      const before = next.userEl;
      await sendMessage(next.text, { userEl: next.userEl, errorEl: next.errorEl });
      const stillFailed = pendingSends.some((p) => p.userEl === before);
      if (stillFailed) break;
    }
  } finally {
    retryingQueue = false;
  }
}

async function sendMessage(text, opts = {}) {
  const userEl = opts.userEl || createMessage("user", text);
  dropPending(userEl);
  setLoading(true);
  createTypingIndicator();
  const requestBody = {
    message: text,
    provider: currentProvider(),
    inject_stm: injectSTM.checked,
    inject_wm: injectWM.checked,
    inject_ltm: injectLTM.checked,
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
    let data = {};
    try {
      data = await res.json();
    } catch {
      data = { error: `Ответ сервера не JSON (HTTP ${res.status})` };
    }
    const durationMs = Math.round(performance.now() - started);
    removeTypingIndicator();
    logExchange({ requestBody, status: res.status, responseBody: data, durationMs });
    if (!res.ok) {
      updateTokenMeter(data.tokens, data.session);
      if (isRetryableFailure(res.status, null, data)) {
        showNetworkError(text, userEl, networkErrorText(null, data));
        return;
      }
      createMessage("assistant", data.error || "Ошибка", "message--error", {
        durationMs: data.duration_ms ?? durationMs,
      });
      return;
    }
    renderRouteEvents(data.route_events);
    createMessage("assistant", data.reply, "", { durationMs: data.duration_ms ?? durationMs });
    renderMemoryMeta(data.memory);
    history.push({ role: "user", content: text });
    history.push({ role: "assistant", content: data.reply });
    updateTokenMeter(data.tokens, data.session);
    await loadMemory();
  } catch (err) {
    removeTypingIndicator();
    logExchange({
      requestBody,
      status: 0,
      responseBody: { error: String(err) },
      durationMs: Math.round(performance.now() - started),
    });
    showNetworkError(text, userEl, networkErrorText(err, null));
  } finally {
    setLoading(false);
    inputEl.focus();
  }
}

async function runCompare() {
  setLoading(true);
  createMessage("user", "Демо: посеять слои, вытеснить STM и сравнить ответы");
  createTypingIndicator();
  const requestBody = { provider: currentProvider() };
  const started = performance.now();
  try {
    const res = await fetch("/api/memory/demo", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
    });
    const data = await res.json();
    const durationMs = Math.round(performance.now() - started);
    removeTypingIndicator();
    logExchange({ url: "/api/memory/demo", requestBody, status: res.status, responseBody: data, durationMs });
    if (!res.ok) {
      createMessage("assistant", data.error || "Демо упало", "message--error", { durationMs });
      return;
    }
    createMessage("assistant", data.report || JSON.stringify(data, null, 2), "message--mono", { durationMs });
  } catch {
    removeTypingIndicator();
    createMessage("assistant", "Не удалось запустить демо слоёв.", "message--error");
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

clearBtn.addEventListener("click", () => {
  if (!isLoading) clearLayer("short_term", true);
});
clearStmBtn.addEventListener("click", () => {
  if (!isLoading) clearLayer("short_term", true);
});
clearWmBtn.addEventListener("click", () => {
  if (!isLoading) clearLayer("working", false);
});
clearLtmBtn.addEventListener("click", () => {
  if (!isLoading) clearLayer("long_term", false);
});

settingsBtn.addEventListener("click", () => setSettingsOpen(!layoutEl.classList.contains("layout--nav")));
settingsCloseBtn.addEventListener("click", () => setSettingsOpen(false));
saveStrategyBtn.addEventListener("click", savePolicy);
compareBtn.addEventListener("click", () => {
  if (!isLoading) runCompare();
});

providerSelect.addEventListener("change", () => {
  localStorage.setItem(PROVIDER_STORAGE_KEY, currentProvider());
  updateProviderLabel();
});
debugToggle.addEventListener("change", () => setDebugMode(debugToggle.checked));
clearDebugBtn.addEventListener("click", clearDebugLog);

if (netRetryBtn) {
  netRetryBtn.addEventListener("click", () => {
    if (!isLoading) retryPendingSends();
  });
}
window.addEventListener("online", () => {
  updateNetBanner();
  retryPendingSends();
});
window.addEventListener("offline", updateNetBanner);

setDebugMode(debugEnabled);
setSettingsOpen(true);
updateNetBanner();
Promise.all([loadProviders(), loadMemory(), loadHistory()]).then(() => {
  restoreUnsent();
  inputEl.focus();
});
