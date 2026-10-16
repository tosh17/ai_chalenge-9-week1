# Схемы данных

Состояние чата — JSON в `data/`. Исходники его не коммитят. RAG эти файлы не индексирует: в индекс попадают только `README.md` и `docs/`.

Слои памяти лежат в `data/memory/short-term.json`, `data/memory/working.json` и `data/memory/long-term.json`. Профили — `data/memory/profiles.json`, инварианты — `data/memory/invariants.json`. Наблюдения — `data/observations/<город>.jsonl`. Кэш эмбеддингов — `data/rag/index.json`.

## Слои памяти

Каталог `MEMORY_DIR` (по умолчанию `data/memory`). Три независимых файла.

### data/memory/short-term.json

Короткая память: хвост диалога. В запрос попадает не больше `STM_WINDOW_N` последних реплик, если включён `MEMORY_INJECT_STM`.

```json
{
  "messages": [
    {"role": "user", "content": "что приготовить из тыквы"},
    {"role": "assistant", "content": "Суп."}
  ]
}
```

`role` — `user`, `assistant` или `system`. У вызовов инструментов могут быть `tool_calls` и `tool_call_id`.

### data/memory/working.json

Рабочая память одной задачи.

```json
{
  "task_id": "task-1",
  "goal": "суп из тыквы",
  "status": "active",
  "constraints": ["без орехов"],
  "notes": [],
  "artifacts": {},
  "task": {
    "stage": "execution",
    "step": "найти рецепт",
    "step_index": 1,
    "expect": "перевод страницы",
    "paused": false,
    "done_so_far": ["план"],
    "last_event": "advance"
  },
  "updated_at": "2026-10-16T15:00:00Z"
}
```

`status`: `idle`, `active`, `done`.

`task.stage`: `idle`, `planning`, `execution`, `validation`, `done`. Переход только по событию. Пропуск этапа возвращает `skips`, а не молчаливую смену стадии. `paused: true` ставит событие `pause`.

### data/memory/long-term.json

То, что должно пережить задачу.

```json
{
  "profile": {
    "name": "Антон",
    "language": "ru",
    "preferences": {"diet": "без орехов"}
  },
  "decisions": [
    {"key": "units", "value": "граммы", "why": "так в рецептах"}
  ],
  "knowledge": [
    {"topic": "kitchen", "fact": "духовки нет"}
  ]
}
```

## Профили

`data/memory/profiles.json` — несколько именованных профилей. Активный подмешивается в каждый запрос, пока включён `MEMORY_INJECT_PROFILE`.

```json
{
  "active_id": "anton",
  "profiles": {
    "anton": {
      "id": "anton",
      "title": "Антон",
      "name": "Антон",
      "role": "домашний повар",
      "language": "ru",
      "style": "коротко",
      "format": "список",
      "constraints": ["без орехов"]
    }
  }
}
```

## Инварианты

`data/memory/invariants.json`. Правило живёт вне диалога. Пока `enabled: true`, агент отказывает запросу, который его ломает.

```json
{
  "invariants": [
    {
      "id": "stack-go",
      "kind": "stack",
      "title": "Стек",
      "rule": "сервер остаётся на Go",
      "forbid": ["переписать на python"],
      "enabled": true
    }
  ]
}
```

`kind`: `architecture`, `decision`, `stack`, `business`.

## История стратегий

`CHAT_HISTORY_PATH` (`data/chat-history.json`) — файл дня 7–10, если агент открыт через `memory.Store`, а не через слои. В текущем сервере основной диалог лежит в слоях.

```json
{
  "messages": [],
  "summary": "",
  "summarized_up_to": 0,
  "facts": {"diet": "без орехов"},
  "checkpoint_at": 4,
  "active_branch": "a",
  "branches": {
    "a": {"id": "a", "title": "суп", "messages": []}
  }
}
```

`branches` здесь — ветки диалога, не git.

## Наблюдения

`data/observations/<город>.jsonl`. Одна строка — один замер. Пишет `cmd/observe`, чат только читает.

```json
{
  "city": "Волгоград",
  "country": "Россия",
  "latitude": 48.7,
  "longitude": 44.5,
  "timezone": "Europe/Volgograd",
  "observed_at": "2026-10-16T12:00",
  "temperature_c": 12.4,
  "weather": "облачно",
  "humidity": 70,
  "wind_kmh": 14,
  "sunrise": "2026-10-16T06:12",
  "sunset": "2026-10-16T17:40",
  "moon_phase": 0.42,
  "moon_name": "растущая луна",
  "saved_at": "2026-10-16T09:00:00Z"
}
```

## Индекс RAG

`data/rag/index.json`. Кэш, не источник правды. Правда — markdown в корне и в `docs/`.

```json
{
  "version": 1,
  "model": "nomic-embed-text",
  "chunks": [
    {
      "id": "0123456789abcdef",
      "path": "docs/api.md",
      "heading": "Чат",
      "text": "POST /api/chat ...",
      "vector": [0.01, 0.02]
    }
  ]
}
```

`id` — усечённый SHA-256 пути, заголовка и текста. Смена абзаца меняет id, и этот чанк эмбеддится заново. `vector` — 768 чисел модели `nomic-embed-text`.
