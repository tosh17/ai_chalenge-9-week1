# Структура проекта

Корень репозитория — модуль Go и документация. Каталоги `bin/` и `data/` в git не входят: туда складываются собранный MCP и живые JSON.

## Каталоги

```
cmd/server      HTTP-чат и сборка RAG
cmd/mcp         stdio-серверы: search, translate, diagram, project
cmd/observe     фоновый сбор погоды
internal/agent  агент, стратегии контекста, вызов инструментов
internal/config чтение окружения
internal/deepseek клиент chat/completions
internal/handler HTTP и вшитый UI
internal/help   команда /help
internal/rag    чанки, эмбеддинги Ollama, поиск
internal/mcp    клиент stdio MCP
internal/mcpsrv реализация инструментов на Go
internal/memory слои, профили, инварианты, автомат задачи
internal/observe архив JSONL
internal/tokens оценка токенов и стоимости
mcp/open_meteo  Kotlin-сервер погоды и медиа (Gradle)
docs            документация, которую индексирует RAG
```

## Ключевые файлы

| Путь | Зачем |
| --- | --- |
| `cmd/server/main.go` | поднимает агента, MCP и индекс RAG |
| `internal/handler/handler.go` | `/api/chat`; `/help` перехватывается здесь |
| `internal/handler/web/index.html` | страница чата |
| `internal/handler/web/static/chat.js` | отправка сообщений и карточка источников |
| `internal/help/help.go` | сборка промпта из RAG и MCP |
| `internal/rag/index.go` | индекс `README.md` + `docs/**/*.md` |
| `internal/mcpsrv/project.go` | `git_branch`, `list_files`, `git_diff` |
| `internal/memory/layers.go` | три JSON-слоя |
| `.env.example` | образец окружения |

## Что не является исходниками

- `data/memory` — память чата на этой машине.
- `data/chat-history.json` — старый файл истории, если стратегия пишет его.
- `data/observations` — архив погоды.
- `data/rag/index.json` — кэш векторов. Его можно удалить: следующий `/help` посчитает эмбеддинги заново.
- `bin/mcp` — собранный `cmd/mcp`. Без него сервер пробует `go run ./cmd/mcp`.
- `mcp/open_meteo/build` — JAR погоды. Путь по умолчанию: `mcp/open_meteo/build/libs/open-meteo-0.1.0-all.jar`.

## Как читать репозиторий

Сначала этот файл и [architecture.md](architecture.md). Ручки — [api.md](api.md). Поля JSON — [data-schemas.md](data-schemas.md). Вопрос «что сейчас в git» закрывает не дерево файлов, а MCP из [mcp.md](mcp.md): ветка меняется, документация — нет.
