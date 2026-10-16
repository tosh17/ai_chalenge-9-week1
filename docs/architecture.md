# Архитектура

Сервис — HTTP-чат на Go. Модуль `github.com/tosh17/deepseek-service`. Точка входа чата — `cmd/server`. Рядом живут два отдельных процесса: `cmd/mcp` (инструменты по stdin/stdout) и `cmd/observe` (сбор наблюдений погоды).

## Процессы

| Процесс | Как запускается | Роль |
| --- | --- | --- |
| Чат | `go run ./cmd/server` | UI, `/api/chat`, память, RAG, `/help` |
| MCP | `bin/mcp search\|translate\|diagram\|project` | инструменты, которые чат поднимает сам |
| Погода | JAR `mcp/open_meteo` | `get_weather`, `observe_city`, медиа-цепочка |
| Наблюдения | `go run ./cmd/observe` | раз в `OBSERVE_EVERY` дописывает JSONL |

Чат слушает `PORT` (по умолчанию 8080) только на localhost. Статика вшита в бинарник из `internal/handler/web`.

## Провайдеры модели

У агента два бэкенда. Оба говорят по OpenAI-совместимому `POST /v1/chat/completions`.

- `local` — Qwen на `LOCAL_API_URL` (сейчас `http://10.10.0.220:8787/v1`, модель `qwen-local`). Это провайдер по умолчанию, окно счётчика `LOCAL_CONTEXT_LIMIT`.
- `deepseek` — облачный DeepSeek, ключ `DEEPSEEK_API_KEY`, окно `CONTEXT_TOKEN_LIMIT`.

Выбор в UI — список «Модель». `/help` использует тот же выбранный провайдер, но не кулинарный system prompt и не слои памяти.

Эмбеддинги для RAG — отдельный вызов. Их считает Ollama `nomic-embed-text` на `RAG_OLLAMA_URL` (`http://10.10.1.101:11434`). Генерация текста и эмбеддинги — разные хосты.

## Ход обычного сообщения

1. Браузер шлёт `POST /api/chat` с `message` и `provider`.
2. Если текст начинается с `/help`, обработчик не идёт в кулинарного агента. Дальше см. [assistant.md](assistant.md).
3. Иначе `agent.Handle` собирает контекст: стратегия (`sliding`, `facts` или `branch`), слои памяти, профиль, инварианты.
4. Модель может звать инструменты MCP, пока ход не закончится.
5. Ответ и короткая память пишутся в `data/memory`.

Кулинарный system prompt просит искать рецепты на английских страницах (`web_search` → `web_fetch` → `translate_en_ru`) и ставить фото через `web_image`.

## Память агента

Три слоя в каталоге `MEMORY_DIR` (`data/memory`):

- short-term — последние реплики диалога;
- working — цель текущей задачи и конечный автомат этапов `idle → planning → execution → validation → done`;
- long-term — профиль, решения и знания.

Поверх слоёв лежат именованные профили (`profiles.json`) и инварианты (`invariants.json`). Инвариант, который нельзя нарушить, заставляет агента отказать, а не «договориться» в диалоге. Схемы полей — в [data-schemas.md](data-schemas.md).

Стратегия контекста режет историю до запроса: скользящее окно, липкие факты или ветки после checkpoint. Это не git-ветки. Git-ветку репозитория читает только MCP `git_branch`.

## Наблюдения

`cmd/observe` сам ходит в Open-Meteo через JAR и дописывает `data/observations/<город>.jsonl`. Чат этот файл только читает инструментом `observation_report`. Город по умолчанию — Волгоград, период — 15 минут.
