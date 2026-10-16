# HTTP API

База — `http://127.0.0.1:8080`. Тела — JSON, если не сказано иное. Страница чата: `GET /`.

## Чат

### POST /api/chat

Обычное сообщение уходит кулинарному агенту. Текст, который начинается с `/help`, обрабатывает ассистент документации и не пишет слои памяти.

Запрос:

```json
{
  "message": "/help где лежит схема памяти",
  "provider": "local"
}
```

`provider` — `local` или `deepseek`. Пустой id берёт провайдер по умолчанию.

Ответ 200:

```json
{
  "reply": "Слои лежат в data/memory.",
  "agent": "day31-help",
  "provider": "local",
  "model": "qwen-local",
  "duration_ms": 1200,
  "help": {
    "question": "где лежит схема памяти",
    "mode": "embeddings",
    "branch": "day31",
    "head": "abc1234",
    "dirty": 0,
    "sources": [
      {"path": "docs/data-schemas.md", "heading": "Слои памяти", "score": 0.72}
    ],
    "tools": ["git_branch", "list_files", "git_diff"]
  }
}
```

Поле `help` есть только у команды `/help`. `mode` — `embeddings`, `mixed` или `lexical`. `degraded: true` значит, что модель не ответила и `reply` собран из фрагментов.

Ошибки агента: 400 (битый JSON), 413 (контекст длиннее лимита), 502 (провайдер недоступен и для `/help` не из чего собрать даже выдержку).

### GET /api/help

Состояние индекса, без вопроса и без вызова модели.

```json
{
  "ready": true,
  "mode": "embeddings",
  "chunks": 40,
  "files": ["README.md", "docs/api.md"],
  "embed_model": "nomic-embed-text",
  "ollama": "http://10.10.1.101:11434",
  "top_k": 5,
  "mcp": ["git_branch", "list_files", "git_diff"]
}
```

Пока индекс строится, `ready` равен `false`.

### POST /api/day30

Проверка локальной Qwen: сеть, три коротких ответа, окно контекста, заголовки rate limit. Тела нет. Карточка в UI — кнопка «Проверить модель».

### GET /api/history и DELETE /api/history

История короткой памяти и сброс. `/help` в эту историю не попадает.

### GET /api/providers

Список бэкендов и id по умолчанию.

### GET /health

Сводка: агент, провайдеры, стратегия, политика памяти, имена инструментов кулинарного MCP.

## Память, профиль, задача

Эти ручки обслуживают панель настроек. Схемы тел совпадают с файлами из [data-schemas.md](data-schemas.md).

| Метод и путь | Действие |
| --- | --- |
| GET /api/memory | снимок слоёв и политика подмешивания |
| POST /api/memory/policy | какие слои класть в запрос |
| POST /api/memory/write | ручная запись в working или long-term |
| POST /api/memory/demo | демонстрация маршрутизации |
| DELETE /api/memory | очистить все слои |
| DELETE /api/memory/{layer} | очистить один слой |
| GET /api/profile, POST /api/profile | прочитать или сохранить профиль |
| POST /api/profile/activate | сделать профиль активным |
| GET /api/invariants, POST /api/invariants | список и запись правила |
| POST /api/invariants/toggle | включить или выключить |
| DELETE /api/invariants/{id} | удалить |
| POST /api/task/event | событие автомата: start, pause, resume, advance, fail, finish, set, reset |
| POST /api/task/seed | пример задачи |

## Стратегия контекста

| Метод и путь | Действие |
| --- | --- |
| GET /api/strategy, POST /api/strategy | sliding, facts или branch и размеры окон |
| POST /api/branch/fork, POST /api/branch/switch | ветки диалога после checkpoint |
| GET /api/compression, POST /api/compression | наследие сжатия дня 9, в сервере выключено |
| POST /api/strategy/compare, POST /api/compress-demo | сравнения для прошлых дней |

## Прочее

- `GET /chatAi` — вторая страница, сравнение диалогов.
- `POST /api/chat-ai` — прогон этой страницы.
- `GET /media/{name}` — png/jpg, которые нарисовал инструмент схемы.
- `GET /static/...` — css и js чата.
