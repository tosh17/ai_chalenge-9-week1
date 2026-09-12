const nameA = document.getElementById("nameA");
const nameB = document.getElementById("nameB");
const promptA = document.getElementById("promptA");
const promptB = document.getElementById("promptB");
const topic = document.getElementById("topic");
const providerSelect = document.getElementById("providerSelect");
const startBtn = document.getElementById("startBtn");
const stopBtn = document.getElementById("stopBtn");
const logEl = document.getElementById("log");
const statusEl = document.getElementById("status");

let abortCtrl = null;
let running = false;

function escapeHTML(text) {
  const div = document.createElement("div");
  div.textContent = text;
  return div.innerHTML;
}

function formatDuration(ms) {
  if (ms == null || Number.isNaN(ms)) return "";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

function formatTokens(tokens) {
  if (!tokens) return "";
  const out = tokens.completion ?? 0;
  const prompt = tokens.prompt ?? 0;
  const total = tokens.total ?? prompt + out;
  return `${total} tok (in ${prompt} / out ${out})`;
}

function setBusy(busy) {
  running = busy;
  startBtn.disabled = busy;
  stopBtn.disabled = !busy;
  nameA.disabled = busy;
  nameB.disabled = busy;
  promptA.disabled = busy;
  promptB.disabled = busy;
  topic.disabled = busy;
  providerSelect.disabled = busy;
}

function addBubble(speaker, content, side, durationMs, tokens) {
  const el = document.createElement("article");
  el.className = `bubble bubble--${side}`;
  const metaParts = [];
  if (durationMs != null) metaParts.push(formatDuration(durationMs));
  const tok = formatTokens(tokens);
  if (tok) metaParts.push(tok);
  const meta = metaParts.length
    ? `<span class="bubble__time">${escapeHTML(metaParts.join(" · "))}</span>`
    : "";
  el.innerHTML = `
    <div class="bubble__who">${escapeHTML(speaker)}</div>
    <div class="bubble__text">${escapeHTML(content)}</div>
    ${meta}
  `;
  logEl.appendChild(el);
  el.scrollIntoView({ behavior: "smooth", block: "end" });
  return el;
}

function showTyping(speaker, side) {
  removeTyping();
  const el = document.createElement("article");
  el.className = `bubble bubble--${side} bubble--typing`;
  el.id = "typingBubble";
  el.innerHTML = `
    <div class="bubble__who">${escapeHTML(speaker)}</div>
    <div class="bubble__text bubble__text--typing">
      <span class="dot"></span><span class="dot"></span><span class="dot"></span>
    </div>
  `;
  logEl.appendChild(el);
  el.scrollIntoView({ behavior: "smooth", block: "end" });
  return el;
}

function removeTyping() {
  document.getElementById("typingBubble")?.remove();
}

function addError(msg) {
  removeTyping();
  const el = document.createElement("article");
  el.className = "bubble bubble--error";
  el.textContent = msg;
  logEl.appendChild(el);
}

async function loadProviders() {
  try {
    const res = await fetch("/api/providers");
    if (!res.ok) return;
    const data = await res.json();
    const list = Array.isArray(data.providers) ? data.providers : [];
    if (!list.length) return;
    providerSelect.innerHTML = "";
    for (const p of list) {
      const opt = document.createElement("option");
      opt.value = p.id;
      opt.textContent = `${p.title} (${p.model})`;
      providerSelect.appendChild(opt);
    }
    const def = data.default_provider || list.find((p) => p.default)?.id || list[0].id;
    if (def) providerSelect.value = def;
  } catch {
    /* keep defaults */
  }
}

function nextSpeaker(base, pastLen) {
  return pastLen % 2 === 0
    ? { name: base.a.name, side: "a" }
    : { name: base.b.name, side: "b" };
}

async function startDialogue() {
  logEl.innerHTML = "";
  statusEl.textContent = "Диалог идёт по одной реплике… Стоп — остановить.";
  setBusy(true);
  abortCtrl = new AbortController();

  const base = {
    a: { name: nameA.value.trim() || "Алиса", prompt: promptA.value.trim() },
    b: { name: nameB.value.trim() || "Боб", prompt: promptB.value.trim() },
    topic: topic.value.trim(),
    provider: providerSelect.value,
  };
  const past = [];
  const started = performance.now();

  try {
    while (running && !abortCtrl.signal.aborted) {
      const who = nextSpeaker(base, past.length);
      statusEl.textContent = `${who.name} отвечает…`;
      showTyping(who.name, who.side);

      const res = await fetch("/api/chat-ai", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...base, past }),
        signal: abortCtrl.signal,
      });
      const data = await res.json();
      removeTyping();

      if (!res.ok) {
        addError(data.error || "Не удалось получить реплику");
        statusEl.textContent = "Ошибка";
        break;
      }
      const turn = (data.turns || [])[0];
      if (!turn) {
        statusEl.textContent = "Пустой ответ";
        break;
      }

      // Сразу рисуем реплику, затем запрашиваем следующую.
      past.push({
        index: turn.index,
        speaker: turn.speaker,
        content: turn.content,
      });
      addBubble(turn.speaker, turn.content, who.side, turn.duration_ms, turn.tokens);
      statusEl.textContent =
        `Сказано: ${past.length} · дальше отвечает ${nextSpeaker(base, past.length).name} · Стоп — остановить`;
    }

    removeTyping();
    if (abortCtrl?.signal.aborted || !running) {
      statusEl.textContent = `Остановлено · ${past.length} реплик · ${formatDuration(performance.now() - started)}`;
    } else if (past.length) {
      statusEl.textContent = `Готово · ${past.length} реплик · ${formatDuration(performance.now() - started)}`;
    }
  } catch (err) {
    removeTyping();
    if (err.name === "AbortError") {
      statusEl.textContent = `Остановлено · ${past.length} реплик`;
    } else {
      statusEl.textContent = "Ошибка сети";
      addError("Не удалось связаться с сервером");
    }
  } finally {
    abortCtrl = null;
    setBusy(false);
  }
}

startBtn.addEventListener("click", () => {
  if (!running) startDialogue();
});

stopBtn.addEventListener("click", () => {
  running = false;
  abortCtrl?.abort();
  removeTyping();
});

loadProviders();
