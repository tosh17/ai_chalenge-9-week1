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
const injectProfile = document.getElementById("injectProfile");
const injectInvariants = document.getElementById("injectInvariants");
const profileSelect = document.getElementById("profileSelect");
const profileName = document.getElementById("profileName");
const profileRole = document.getElementById("profileRole");
const profileLanguage = document.getElementById("profileLanguage");
const profileStyle = document.getElementById("profileStyle");
const profileStyleCustom = document.getElementById("profileStyleCustom");
const profileFormat = document.getElementById("profileFormat");
const profileFormatCustom = document.getElementById("profileFormatCustom");
const profileConstraintList = document.getElementById("profileConstraintList");
const profileConstraintInput = document.getElementById("profileConstraintInput");
const addConstraintBtn = document.getElementById("addConstraintBtn");
const profileMeta = document.getElementById("profileMeta");
const saveProfileBtn = document.getElementById("saveProfileBtn");
const profileDemoBtn = document.getElementById("profileDemoBtn");
const taskStage = document.getElementById("taskStage");
const taskStep = document.getElementById("taskStep");
const taskExpect = document.getElementById("taskExpect");
const taskMeta = document.getElementById("taskMeta");
const taskPauseBtn = document.getElementById("taskPauseBtn");
const taskResumeBtn = document.getElementById("taskResumeBtn");
const taskAdvanceBtn = document.getElementById("taskAdvanceBtn");
const taskSaveBtn = document.getElementById("taskSaveBtn");
const taskSeedBtn = document.getElementById("taskSeedBtn");
const taskDemoBtn = document.getElementById("taskDemoBtn");
const lifecycleDemoBtn = document.getElementById("lifecycleDemoBtn");
const invariantDemoBtn = document.getElementById("invariantDemoBtn");
const invariantList = document.getElementById("invariantList");
const invariantKind = document.getElementById("invariantKind");
const invariantTitle = document.getElementById("invariantTitle");
const invariantRule = document.getElementById("invariantRule");
const invariantForbid = document.getElementById("invariantForbid");
const saveInvariantBtn = document.getElementById("saveInvariantBtn");
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

const DEBUG_STORAGE_KEY = "deepseek-chat-debug-day12";
const PROVIDER_STORAGE_KEY = "day12-chat-provider";
const UNSENT_STORAGE_KEY = "day12-unsent-messages";

/** @type {{role: string, content: string}[]} */
let history = [];
let isLoading = false;
let chatAbort = null;
let chatGen = 0;
let debugEnabled = localStorage.getItem(DEBUG_STORAGE_KEY) === "true";
let debugEntryCount = 0;
/** @type {{id: string, title: string, model: string, context_limit?: number}[]} */
let providers = [];
let currentPolicy = { stm_window_n: 8, inject_stm: false, inject_wm: false, inject_ltm: false, inject_profile: true, inject_invariants: false };
/** @type {{id: string, title?: string, name?: string, role?: string, language?: string, style?: string, format?: string, constraints?: string[]}[]} */
let profileList = [];
let activeProfileID = "";
/** @type {string[]} */
let constraintItems = [];
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
  const trace = !isUser && !isSystem ? buildTraceHTML(meta) : "";
  const steps = !isUser && !isSystem ? turnStepsHTML(meta.steps) : "";
  el.innerHTML = `
    <div class="message__avatar">${avatar}</div>
    <div class="message__body">
      ${trace}
      <div class="message__bubble">${escapeHTML(content)}</div>
      ${steps}
      ${timeLabel}
    </div>
  `;
  chatEl.appendChild(el);
  scrollToBottom();
  return el;
}

function prettyPayload(value) {
  if (value == null || value === "") return "";
  if (typeof value === "string") {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  return formatJSON(value);
}

function turnStepsHTML(steps) {
  if (!Array.isArray(steps) || !steps.length) return "";
  return `<div class="turn-steps">${steps.map((step, i) => {
    const bits = [`${i + 1}. ${step.title || step.kind || "шаг"}`];
    if (step.status) bits.push(String(step.status));
    if (step.duration_ms) bits.push(`${step.duration_ms} мс`);
    if (step.error) bits.push("ошибка");
    let body = "";
    if (step.url) body += `<p class="turn-step__url">${escapeHTML(step.url)}</p>`;
    if (step.error) body += `<p class="turn-step__err">${escapeHTML(step.error)}</p>`;
    const request = prettyPayload(step.request);
    const response = prettyPayload(step.response);
    if (request) body += `<h4>Запрос</h4><pre>${escapeHTML(request)}</pre>`;
    if (response) body += `<h4>Ответ</h4><pre>${escapeHTML(response)}</pre>`;
    return `<details class="turn-step"><summary>${escapeHTML(bits.join(" · "))}</summary><div class="turn-step__body">${body}</div></details>`;
  }).join("")}</div>`;
}

function kindTitle(kind) {
  switch (String(kind || "").toLowerCase()) {
    case "architecture": return "архитектура";
    case "stack": return "стек";
    case "business": return "бизнес-правило";
    default: return "решение";
  }
}

function humanStage(stage) {
  switch (String(stage || "").toLowerCase()) {
    case "planning":
      return "планирование";
    case "execution":
      return "выполнение";
    case "validation":
      return "проверка";
    case "done":
      return "готово";
    case "idle":
    case "":
      return "нет задачи";
    default:
      return String(stage);
  }
}

function phaseTitle(task) {
  const name = humanStage(task && task.stage);
  const cap = name.charAt(0).toUpperCase() + name.slice(1);
  if (task && task.paused && name !== "нет задачи") return `${cap} · пауза`;
  return cap;
}

function phaseLead(task) {
  const t = task || {};
  if (t.paused) {
    return `Сейчас пауза на фазе «${humanStage(t.stage)}». Этап не двигаю, жду продолжения.`;
  }
  const step = (t.step || "").trim();
  switch (String(t.stage || "").toLowerCase()) {
    case "planning":
      return step
        ? `Фаза — планирование. Сейчас: ${step}.`
        : "Фаза — планирование. Собираю цель и план.";
    case "execution":
      return step
        ? `Фаза — выполнение. Сейчас: ${step}.`
        : "Фаза — выполнение. Иду по согласованному плану.";
    case "validation":
      return step
        ? `Фаза — проверка. Сейчас: ${step}.`
        : "Фаза — проверка. Смотрю, всё ли сходится.";
    case "done":
      return "Фаза — готово. Задачу закрыл.";
    default:
      return "Пока нет активной задачи — если напишете цель, начну с планирования.";
  }
}

function humanizeReason(raw) {
  let s = String(raw || "").trim();
  if (!s) return "";
  const auto = s.match(/автоэтап:\s*([a-z_]+)\s*→\s*([a-z_]+)(?:\s*·\s*шаг\s*[«"]([^»"]+)[»"])?/i);
  if (auto) {
    let line = `Сменил фазу: ${humanStage(auto[1])} → ${humanStage(auto[2])}`;
    if (auto[3]) line += `. Шаг: ${auto[3]}`;
    return line;
  }
  if (/llm_fallback/i.test(s)) {
    return "Классификатор памяти не ответил, пошёл по простым правилам";
  }
  if (/^STM:/i.test(s) || /реплика текущего диалога/i.test(s)) {
    return "Оставил реплику в текущем диалоге";
  }
  if (/^инвариант:/i.test(s)) {
    return s.replace(/^инвариант:\s*/i, "Инвариант: ");
  }
  if (/жизненный цикл/i.test(s)) {
    return s.replace(/^жизненный цикл:\s*/i, "Жизненный цикл: ");
  }
  s = s.replace(/^long_term:\s*/i, "");
  s = s.replace(/^LTM:\s*/i, "");
  s = s.replace(/^working(?:\.[a-zA-Z_]+)?(?:=[^:]*)?:\s*/i, "");
  s = s.replace(/working\.stage=([a-z]+)/gi, (_, st) => `фаза «${humanStage(st)}»`);
  s = s.replace(/\bstage=([a-z]+)/gi, (_, st) => `фаза «${humanStage(st)}»`);
  s = s.replace(/\bstatus=active\b/gi, "задача активна");
  s = s.replace(/\bstatus=idle\b/gi, "задачи нет");
  s = s.replace(/\bstatus=done\b/gi, "задача закрыта");
  return s.replace(/\s+/g, " ").trim();
}

function memoryWrites(routes) {
  const tags = new Set();
  for (const ev of routes) {
    if (ev.wrote_stm) tags.add("диалог");
    if (ev.wrote_wm) tags.add("текущую задачу");
    if (ev.wrote_ltm) tags.add("долгую память");
    if (ev.wrote_profile) tags.add("профиль");
    if (ev.wrote_invariant) tags.add("инварианты");
  }
  return [...tags];
}

function buildTraceHTML(meta) {
  const routes = Array.isArray(meta.routeEvents) ? meta.routeEvents : [];
  const debug = meta.debug;
  const task = meta.task || {};
  const conflicts = Array.isArray(meta.conflicts) ? meta.conflicts : [];
  const skips = Array.isArray(meta.skips) ? meta.skips : [];
  const invariants = Array.isArray(meta.invariants) ? meta.invariants : [];
  const reasons = routes.flatMap((r) => r.reasons || []);
  const hasRouter = routes.some((r) => r.prompt || r.raw_reply || r.system);
  if (!reasons.length && !debug && !task.stage && !hasRouter && !conflicts.length && !invariants.length && !skips.length) return "";
  const title = (conflicts.length || skips.length) ? `Отказ · ${phaseTitle(task)}` : phaseTitle(task);
  const humanReasons = [...new Set(reasons.map(humanizeReason).filter(Boolean))];
  const writes = memoryWrites(routes);
  let body = `<p class="trace__lead">${escapeHTML(phaseLead(task))}</p>`;
  if (invariants.length) {
    body += `<section class="trace__sec"><h4>Инварианты</h4><ul>${invariants
      .map((inv) => `<li><strong>${escapeHTML(kindTitle(inv.kind))}</strong> — ${escapeHTML(inv.title)}: ${escapeHTML(inv.rule)}</li>`)
      .join("")}</ul></section>`;
  }
  if (conflicts.length) {
    body += `<section class="trace__sec trace__sec--conflict"><h4>Конфликт</h4><ul>${conflicts
      .map((c) => `<li>${escapeHTML(c.reason || c.title)}</li>`)
      .join("")}</ul><p>Отказываюсь предлагать решение, которое ломает рамку. Объясняю почему и что можно внутри неё.</p></section>`;
  }
  if (skips.length) {
    body += `<section class="trace__sec trace__sec--conflict"><h4>Недопустимый переход</h4><ul>${skips
      .map((s) => `<li>${escapeHTML(s.reason || "")}${s.allowed ? ` · можно: ${escapeHTML(s.allowed)}` : ""}</li>`)
      .join("")}</ul><p>Этап не перепрыгиваю. Остаюсь в текущей фазе.</p></section>`;
  }
  if (task.expect) {
    body += `<p class="trace__expect">Жду: ${escapeHTML(task.expect)}</p>`;
  }
  if (humanReasons.length) {
    body += `<section class="trace__sec"><h4>Что учёл</h4><ul>${humanReasons
      .map((r) => `<li>${escapeHTML(r)}</li>`)
      .join("")}</ul></section>`;
  }
  if (writes.length) {
    body += `<p class="trace__wrote">Обновил: ${escapeHTML(writes.join(", "))}</p>`;
  }
  let raw = "";
  for (const r of routes) {
    if (!r.system && !r.prompt && !r.raw_reply) continue;
    raw += `<section class="trace__sec"><h4>Классификатор памяти</h4>`;
    if (r.system) raw += `<pre>${escapeHTML(r.system)}</pre>`;
    if (r.prompt) raw += `<pre>${escapeHTML(r.prompt)}</pre>`;
    if (r.raw_reply) raw += `<pre>${escapeHTML(r.raw_reply)}</pre>`;
    raw += `</section>`;
  }
  if (debug && (debug.request || debug.response)) {
    raw += `<section class="trace__sec"><h4>Запрос к модели</h4>`;
    if (debug.request) raw += `<pre>${escapeHTML(formatJSON(debug.request))}</pre>`;
    if (debug.response) raw += `<pre>${escapeHTML(formatJSON(debug.response))}</pre>`;
    raw += `</section>`;
  }
  if (raw) {
    body += `<details class="trace-raw"><summary>Технические подробности</summary>${raw}</details>`;
  }
  return `<details class="trace"><summary><span class="trace__phase">${escapeHTML(title)}</span></summary><div class="trace__body">${body}</div></details>`;
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
  if (saveStrategyBtn) saveStrategyBtn.disabled = loading;
  if (saveProfileBtn) saveProfileBtn.disabled = loading;
  if (profileDemoBtn) profileDemoBtn.disabled = loading;
  if (taskDemoBtn) taskDemoBtn.disabled = loading;
  if (lifecycleDemoBtn) lifecycleDemoBtn.disabled = loading;
  if (invariantDemoBtn) invariantDemoBtn.disabled = loading;
  if (saveInvariantBtn) saveInvariantBtn.disabled = loading;
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
  layoutEl.classList.toggle("layout--settings", open);
  if (settingsPanel) settingsPanel.setAttribute("aria-hidden", open ? "false" : "true");
  const backdrop = document.getElementById("settingsBackdrop");
  if (backdrop) backdrop.hidden = !open;
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
  if (p.inject_profile !== false) on.push("PROF");
  if (p.inject_invariants !== false) on.push("INV");
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
  if (injectProfile) injectProfile.checked = currentPolicy.inject_profile !== false;
  if (injectInvariants) injectInvariants.checked = currentPolicy.inject_invariants !== false;
  stmWindow.value = currentPolicy.stm_window_n || 8;
  strategyLabel.textContent = policyLabel(currentPolicy);
  if (appSubtitle) appSubtitle.textContent = "#инвариант стек: только Go";
}

function formatSTM(messages) {
  if (!Array.isArray(messages) || !messages.length) return "диалог пуст";
  return messages
    .slice(-8)
    .map((m) => `${m.role}: ${m.content}`)
    .join("\n");
}

function formatWM(w) {
  if (!w || (!w.goal && !(w.notes || []).length && !(w.constraints || []).length && !(w.task && w.task.stage && w.task.stage !== "idle"))) {
    return "нет активной задачи";
  }
  const lines = [];
  const t = w.task || {};
  if (t.stage) lines.push(`этап: ${t.stage}${t.paused ? " · ПАУЗА" : ""}`);
  if (t.step) lines.push(`шаг: ${t.step}`);
  if (t.expect) lines.push(`ожидаю: ${t.expect}`);
  (t.done_so_far || []).forEach((s) => lines.push(`сделано: ${s}`));
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
  const t = wm.task || {};
  wmMeta.textContent = wm.goal
    ? `${t.stage || wm.status || "active"}${t.paused ? " · пауза" : ""} · есть задача`
    : "нет задачи";
  wmPre.textContent = formatWM(wm);
  fillTaskForm(wm);
  const lt = mem.long_term || {};
  const bits = [];
  if (lt.profile?.name) bits.push(lt.profile.name);
  bits.push(`реш. ${(lt.decisions || []).length}`);
  bits.push(`знан. ${(lt.knowledge || []).length}`);
  ltmMeta.textContent = bits.join(" · ");
  ltmPre.textContent = formatLTM(lt);
  if (mem.policy) applyPolicyToForm(mem.policy);
  if (mem.profile) fillProfileForm(mem.profile, mem.profile.id || activeProfileID);
}

function setSelectOrCustom(selectEl, customEl, value) {
  if (!selectEl) return;
  const v = (value || "").trim();
  const match = [...selectEl.options].some((o) => o.value === v && o.value !== "__custom");
  if (match) {
    selectEl.value = v;
    if (customEl) {
      customEl.hidden = true;
      customEl.value = "";
    }
    return;
  }
  if (v) {
    selectEl.value = "__custom";
    if (customEl) {
      customEl.hidden = false;
      customEl.value = v;
    }
    return;
  }
  selectEl.selectedIndex = 0;
  if (customEl) {
    customEl.hidden = true;
    customEl.value = "";
  }
}

function valueFromSelectOrCustom(selectEl, customEl) {
  if (!selectEl) return "";
  if (selectEl.value === "__custom") return customEl ? customEl.value.trim() : "";
  return selectEl.value.trim();
}

function toggleCustomField(selectEl, customEl) {
  if (!selectEl || !customEl) return;
  const custom = selectEl.value === "__custom";
  customEl.hidden = !custom;
  if (custom) customEl.focus();
}

function renderConstraintChips() {
  if (!profileConstraintList) return;
  profileConstraintList.innerHTML = "";
  constraintItems.forEach((text, i) => {
    const li = document.createElement("li");
    li.className = "profile-chip";
    const label = document.createElement("span");
    label.textContent = text;
    const btn = document.createElement("button");
    btn.type = "button";
    btn.setAttribute("aria-label", "убрать ограничение");
    btn.textContent = "×";
    btn.addEventListener("click", () => {
      constraintItems.splice(i, 1);
      renderConstraintChips();
    });
    li.append(label, btn);
    profileConstraintList.appendChild(li);
  });
}

function addConstraintFromInput() {
  if (!profileConstraintInput) return;
  const v = profileConstraintInput.value.trim();
  if (!v) return;
  if (!constraintItems.includes(v)) constraintItems.push(v);
  profileConstraintInput.value = "";
  renderConstraintChips();
}

function fillProfileForm(p, id) {
  if (!p) p = {};
  if (id) activeProfileID = id;
  if (profileSelect && activeProfileID) profileSelect.value = activeProfileID;
  if (profileName) profileName.value = p.name || "";
  if (profileRole) profileRole.value = p.role || "";
  if (profileLanguage) profileLanguage.value = p.language === "en" ? "en" : "ru";
  setSelectOrCustom(profileStyle, profileStyleCustom, p.style);
  setSelectOrCustom(profileFormat, profileFormatCustom, p.format);
  constraintItems = Array.isArray(p.constraints) ? p.constraints.filter(Boolean) : [];
  renderConstraintChips();
  if (profileMeta) {
    profileMeta.textContent = [p.title || id || "профиль", p.style].filter(Boolean).join(" · ");
  }
  const title = document.querySelector(".chat-bar__title");
  if (title) title.textContent = p.name ? `Чат · ${p.name}` : "Чат";
}

function phaseRank(stage) {
  switch (String(stage || "").toLowerCase()) {
    case "planning": return 1;
    case "execution": return 2;
    case "validation": return 3;
    case "done": return 4;
    default: return 0;
  }
}

function fillTaskForm(w) {
  const t = (w && w.task) || {};
  const hasTask = !!(w && w.goal);
  const stage = hasTask ? String(t.stage || "planning").toLowerCase() : "idle";
  const track = document.getElementById("phaseTrack");
  if (track) {
    track.classList.toggle("phase-track--paused", !!(hasTask && t.paused));
    track.querySelectorAll("[data-phase]").forEach((el) => {
      const ph = el.getAttribute("data-phase");
      const r = phaseRank(ph);
      const cur = phaseRank(stage);
      el.classList.toggle("is-current", hasTask && r === cur);
      el.classList.toggle("is-done", hasTask && r > 0 && r < cur);
    });
  }
  if (taskMeta) {
    if (!hasTask) {
      taskMeta.textContent = "нет задачи";
    } else {
      taskMeta.textContent = [humanStage(t.stage), t.paused ? "пауза" : ""].filter(Boolean).join(" · ");
    }
  }
}

function renderProfileBook(data) {
  if (!data) return;
  profileList = Array.isArray(data.profiles) ? data.profiles : [];
  activeProfileID = data.active_id || "";
  if (profileSelect) {
    profileSelect.innerHTML = "";
    for (const p of profileList) {
      const opt = document.createElement("option");
      opt.value = p.id;
      opt.textContent = p.title || p.id;
      profileSelect.appendChild(opt);
    }
    if (activeProfileID) profileSelect.value = activeProfileID;
  }
  const active = data.active || profileList.find((x) => x.id === activeProfileID) || {};
  fillProfileForm(active, activeProfileID);
}

function renderInvariants(data) {
  if (!invariantList || !data) return;
  const items = Array.isArray(data.items) ? data.items : [];
  invariantList.innerHTML = "";
  if (!items.length) {
    invariantList.innerHTML = '<li class="nav__meta">пока нет рамок</li>';
    return;
  }
  for (const it of items) {
    const li = document.createElement("li");
    li.className = "invariant-item" + (it.enabled ? "" : " invariant-item--off");
    li.innerHTML = `
      <label class="invariant-item__head">
        <input type="checkbox" data-id="${escapeHTML(it.id)}" ${it.enabled ? "checked" : ""}>
        <span><strong>${escapeHTML(kindTitle(it.kind))}</strong> · ${escapeHTML(it.title || it.id)}</span>
      </label>
      <p>${escapeHTML(it.rule || "")}</p>
      <button type="button" class="btn btn--ghost btn--xs" data-del="${escapeHTML(it.id)}">удалить</button>`;
    const box = li.querySelector("input[type=checkbox]");
    box.addEventListener("change", () => toggleInvariant(it.id, box.checked));
    li.querySelector("[data-del]").addEventListener("click", () => deleteInvariant(it.id));
    invariantList.appendChild(li);
  }
}

async function saveInvariant() {
  const rule = invariantRule ? invariantRule.value.trim() : "";
  const title = invariantTitle ? invariantTitle.value.trim() : "";
  if (!rule) return;
  const forbid = (invariantForbid ? invariantForbid.value : "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  setLoading(true);
  try {
    const res = await fetch("/api/invariants", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        kind: invariantKind ? invariantKind.value : "decision",
        title: title || rule.slice(0, 40),
        rule,
        forbid,
        enabled: true,
      }),
    });
    const data = await res.json();
    if (!res.ok) {
      createMessage("assistant", data.error || "Не удалось сохранить инвариант", "message--error");
      return;
    }
    renderInvariants(data);
    if (invariantTitle) invariantTitle.value = "";
    if (invariantRule) invariantRule.value = "";
    if (invariantForbid) invariantForbid.value = "";
  } catch {
    createMessage("assistant", "Не удалось сохранить инвариант", "message--error");
  } finally {
    setLoading(false);
  }
}

async function toggleInvariant(id, enabled) {
  try {
    const res = await fetch("/api/invariants/toggle", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id, enabled }),
    });
    const data = await res.json();
    if (res.ok) renderInvariants(data);
  } catch {
    /* ignore */
  }
}

async function deleteInvariant(id) {
  try {
    const res = await fetch(`/api/invariants/${encodeURIComponent(id)}`, { method: "DELETE" });
    const data = await res.json();
    if (res.ok) renderInvariants(data);
  } catch {
    /* ignore */
  }
}

function profileFromForm() {
  return {
    id: (profileSelect && profileSelect.value) || activeProfileID || "custom",
    title: (profileList.find((x) => x.id === ((profileSelect && profileSelect.value) || activeProfileID)) || {}).title,
    name: profileName ? profileName.value.trim() : "",
    role: profileRole ? profileRole.value.trim() : "",
    language: profileLanguage ? profileLanguage.value : "ru",
    style: valueFromSelectOrCustom(profileStyle, profileStyleCustom),
    format: valueFromSelectOrCustom(profileFormat, profileFormatCustom),
    constraints: constraintItems.slice(),
  };
}

async function loadProfiles() {
  try {
    const res = await fetch("/api/profile");
    if (!res.ok) return;
    renderProfileBook(await res.json());
  } catch {
    /* ignore */
  }
}

async function saveProfile() {
  setLoading(true);
  try {
    const res = await fetch("/api/profile", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(profileFromForm()),
    });
    const data = await res.json();
    if (!res.ok) {
      createMessage("assistant", data.error || "Не удалось сохранить профиль", "message--error");
      return;
    }
    renderProfileBook(data);
    createMessage("system", `Профиль активен: ${data.active?.title || data.active_id}`, "message--system");
  } catch {
    createMessage("assistant", "Не удалось сохранить профиль", "message--error");
  } finally {
    setLoading(false);
  }
}

async function activateProfile(id) {
  if (!id || isLoading) return;
  setLoading(true);
  try {
    const res = await fetch("/api/profile/activate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id }),
    });
    const data = await res.json();
    if (!res.ok) {
      createMessage("assistant", data.error || "Не удалось переключить профиль", "message--error");
      return;
    }
    renderProfileBook(data);
    createMessage("system", `Профиль: ${data.active?.title || id} — стиль подмешивается в каждый запрос`, "message--system");
  } catch {
    createMessage("assistant", "Не удалось переключить профиль", "message--error");
  } finally {
    setLoading(false);
  }
}

async function runProfileDemo() {
  setLoading(true);
  createMessage("user", "Демо: один вопрос — три профиля и без профиля");
  createTypingIndicator();
  const requestBody = { provider: currentProvider() };
  const started = performance.now();
  try {
    const res = await fetch("/api/profile/demo", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
    });
    const data = await res.json();
    const durationMs = Math.round(performance.now() - started);
    removeTypingIndicator();
    logExchange({ url: "/api/profile/demo", requestBody, status: res.status, responseBody: data, durationMs });
    if (!res.ok) {
      createMessage("assistant", data.error || "Демо профилей упало", "message--error", { durationMs });
      return;
    }
    createMessage("assistant", data.report || JSON.stringify(data, null, 2), "message--mono", { durationMs });
  } catch {
    removeTypingIndicator();
    createMessage("assistant", "Не удалось запустить демо профилей.", "message--error");
  } finally {
    setLoading(false);
  }
}

async function postTask(url, body, label) {
  if (isLoading) return;
  setLoading(true);
  try {
    const res = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: body ? JSON.stringify(body) : "{}",
    });
    const data = await res.json();
    if (!res.ok) {
      createMessage("assistant", data.error || label, "message--error");
      return;
    }
    if (data.memory) renderMemory(data.memory);
    else renderMemory(data);
    const t = (data.memory && data.memory.working && data.memory.working.task) || (data.working && data.working.task) || {};
    createMessage("system", `${label}: ${t.stage || "—"}${t.paused ? " · пауза" : ""} · ${t.step || t.expect || ""}`.trim(), "message--system");
  } catch {
    createMessage("assistant", label, "message--error");
  } finally {
    setLoading(false);
  }
}

async function runTaskDemo() {
  setLoading(true);
  createMessage("user", "Демо: пауза на execution, затем продолжение без повторного плана");
  createTypingIndicator();
  const requestBody = { provider: currentProvider() };
  const started = performance.now();
  try {
    const res = await fetch("/api/task/demo", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
    });
    const data = await res.json();
    const durationMs = Math.round(performance.now() - started);
    removeTypingIndicator();
    logExchange({ url: "/api/task/demo", requestBody, status: res.status, responseBody: data, durationMs });
    if (!res.ok) {
      createMessage("assistant", data.error || "Демо паузы упало", "message--error", { durationMs });
      return;
    }
    createMessage("assistant", data.report || JSON.stringify(data, null, 2), "message--mono", { durationMs });
  } catch {
    removeTypingIndicator();
    createMessage("assistant", "Не удалось запустить демо паузы.", "message--error");
  } finally {
    setLoading(false);
  }
}

async function runLifecycleDemo() {
  setLoading(true);
  createMessage("user", "Демо: перепрыжка этапа, отказ и продолжение после паузы");
  createTypingIndicator();
  const requestBody = { provider: currentProvider() };
  const started = performance.now();
  try {
    const res = await fetch("/api/task/lifecycle", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
    });
    const data = await res.json();
    const durationMs = Math.round(performance.now() - started);
    removeTypingIndicator();
    logExchange({ url: "/api/task/lifecycle", requestBody, status: res.status, responseBody: data, durationMs });
    if (!res.ok) {
      createMessage("assistant", data.error || "Демо переходов упало", "message--error", { durationMs });
      return;
    }
    createMessage("assistant", data.report || JSON.stringify(data, null, 2), "message--mono", { durationMs });
  } catch {
    removeTypingIndicator();
    createMessage("assistant", "Не удалось запустить демо переходов.", "message--error");
  } finally {
    setLoading(false);
  }
}

async function runInvariantDemo() {
  setLoading(true);
  createMessage("user", "Демо: конфликт запроса с инвариантом и запрос внутри рамки");
  createTypingIndicator();
  const requestBody = { provider: currentProvider() };
  const started = performance.now();
  try {
    const res = await fetch("/api/invariants/demo", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(requestBody),
    });
    const data = await res.json();
    const durationMs = Math.round(performance.now() - started);
    removeTypingIndicator();
    logExchange({ url: "/api/invariants/demo", requestBody, status: res.status, responseBody: data, durationMs });
    if (!res.ok) {
      createMessage("assistant", data.error || "Демо инвариантов упало", "message--error", { durationMs });
      return;
    }
    createMessage("assistant", data.report || JSON.stringify(data, null, 2), "message--mono", { durationMs });
  } catch {
    removeTypingIndicator();
    createMessage("assistant", "Не удалось запустить демо инвариантов.", "message--error");
  } finally {
    setLoading(false);
  }
}

function renderRouteEvents(events) {
  if (!routeList) return;
  if (!Array.isArray(events) || !events.length) return;
  routeList.innerHTML = "";
  for (const ev of events) {
    const wrote = [];
    if (ev.wrote_stm) wrote.push("STM");
    if (ev.wrote_wm) wrote.push("WM");
    if (ev.wrote_ltm) wrote.push("LTM");
    if (ev.wrote_profile) wrote.push("PROF");
    if (ev.wrote_invariant) wrote.push("INV");
    const chip = document.createElement("span");
    chip.className = "route-chip";
    chip.textContent = `${ev.source || "route"} → ${wrote.join("+") || "STM"}`;
    routeList.appendChild(chip);
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
    if (data.profiles) renderProfileBook(data.profiles);
    if (data.invariants) renderInvariants(data.invariants);
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
    inject_profile: injectProfile ? injectProfile.checked : true,
    inject_invariants: injectInvariants ? injectInvariants.checked : true,
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
    if (layer !== "all") {
      createMessage("system", `Очищен слой: ${layer}`, "message--system");
    }
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
  if (chatAbort) {
    chatAbort.abort();
    const typing = document.getElementById("typingIndicator");
    if (typing) {
      typing.classList.remove("message--typing");
      typing.classList.add("message--interrupted");
      const bubble = typing.querySelector(".message__bubble");
      if (bubble) bubble.textContent = "Прервано новым сообщением";
      typing.removeAttribute("id");
    }
  }
  const userEl = opts.userEl || createMessage("user", text);
  dropPending(userEl);
  const ac = new AbortController();
  const gen = ++chatGen;
  chatAbort = ac;
  setLoading(true);
  createTypingIndicator();
  const requestBody = {
    message: text,
    provider: currentProvider(),
    inject_stm: injectSTM ? injectSTM.checked : true,
    inject_wm: injectWM ? injectWM.checked : true,
    inject_ltm: injectLTM ? injectLTM.checked : true,
    inject_profile: injectProfile ? injectProfile.checked : true,
    inject_invariants: injectInvariants ? injectInvariants.checked : true,
  };
  const started = performance.now();
  try {
    const headers = { "Content-Type": "application/json", "X-Debug": "true" };
    const res = await fetch("/api/chat", {
      method: "POST",
      headers,
      body: JSON.stringify(requestBody),
      signal: ac.signal,
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
        routeEvents: data.route_events,
        debug: data.debug,
        task: data.memory?.working?.task,
        steps: data.steps,
      });
      return;
    }
    renderRouteEvents(data.route_events);
    createMessage("assistant", data.reply, (data.conflicts?.length || data.skips?.length) ? "message--refused" : "", {
      durationMs: data.duration_ms ?? durationMs,
      routeEvents: data.route_events,
      debug: data.debug,
      task: data.memory?.working?.task,
      conflicts: data.conflicts || data.memory?.conflicts,
      skips: data.skips || data.memory?.skips,
      invariants: data.memory?.invariants,
      steps: data.steps,
    });
    renderMemoryMeta(data.memory);
    history.push({ role: "user", content: text });
    history.push({ role: "assistant", content: data.reply });
    updateTokenMeter(data.tokens, data.session);
    await loadMemory();
  } catch (err) {
    if (err.name === "AbortError") return;
    removeTypingIndicator();
    logExchange({
      requestBody,
      status: 0,
      responseBody: { error: String(err) },
      durationMs: Math.round(performance.now() - started),
    });
    showNetworkError(text, userEl, networkErrorText(err, null));
  } finally {
    if (gen === chatGen) {
      chatAbort = null;
      setLoading(false);
      inputEl.focus();
    }
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
  if (!text) return;
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
  if (!isLoading) clearLayer("all", true);
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
const settingsBackdrop = document.getElementById("settingsBackdrop");
if (settingsBackdrop) {
  settingsBackdrop.addEventListener("click", () => setSettingsOpen(false));
}
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") setSettingsOpen(false);
});
saveStrategyBtn.addEventListener("click", savePolicy);
if (saveProfileBtn) saveProfileBtn.addEventListener("click", saveProfile);
compareBtn.addEventListener("click", () => {
  if (!isLoading) runCompare();
});
if (profileDemoBtn) {
  profileDemoBtn.addEventListener("click", () => {
    if (!isLoading) runProfileDemo();
  });
}
if (taskDemoBtn) {
  taskDemoBtn.addEventListener("click", () => {
    if (!isLoading) runTaskDemo();
  });
}
if (lifecycleDemoBtn) {
  lifecycleDemoBtn.addEventListener("click", () => {
    if (!isLoading) runLifecycleDemo();
  });
}
if (invariantDemoBtn) {
  invariantDemoBtn.addEventListener("click", () => {
    if (!isLoading) runInvariantDemo();
  });
}
if (saveInvariantBtn) saveInvariantBtn.addEventListener("click", saveInvariant);
if (taskPauseBtn) taskPauseBtn.addEventListener("click", () => postTask("/api/task/event", { event: "pause" }, "Пауза"));
if (taskResumeBtn) taskResumeBtn.addEventListener("click", () => postTask("/api/task/event", { event: "resume" }, "Продолжить"));
if (taskAdvanceBtn) taskAdvanceBtn.addEventListener("click", () => postTask("/api/task/event", { event: "advance" }, "Дальше"));
if (taskSaveBtn) {
  taskSaveBtn.addEventListener("click", () => postTask("/api/task/event", {
    event: "set",
    stage: taskStage ? taskStage.value : "",
    step: taskStep ? taskStep.value.trim() : "",
    expect: taskExpect ? taskExpect.value.trim() : "",
  }, "Шаг сохранён"));
}
if (taskSeedBtn) taskSeedBtn.addEventListener("click", () => postTask("/api/task/seed", {}, "Пример задачи"));
if (taskStage) {
  taskStage.addEventListener("change", () => {
    if (!isLoading) postTask("/api/task/event", { event: "set", stage: taskStage.value }, "Этап");
  });
}
if (profileSelect) {
  profileSelect.addEventListener("change", () => activateProfile(profileSelect.value));
}
if (profileStyle) {
  profileStyle.addEventListener("change", () => toggleCustomField(profileStyle, profileStyleCustom));
}
if (profileFormat) {
  profileFormat.addEventListener("change", () => toggleCustomField(profileFormat, profileFormatCustom));
}
if (addConstraintBtn) addConstraintBtn.addEventListener("click", addConstraintFromInput);
if (profileConstraintInput) {
  profileConstraintInput.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter") {
      ev.preventDefault();
      addConstraintFromInput();
    }
  });
}

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
setSettingsOpen(false);
updateNetBanner();
Promise.all([loadProviders(), loadMemory(), loadHistory(), loadProfiles()]).then(() => {
  restoreUnsent();
  inputEl.focus();
});
