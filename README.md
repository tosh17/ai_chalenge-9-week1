# Day 6 · Simple Chat Agent

Веб-чат на Go, где **агент — отдельная сущность**: он принимает запрос пользователя,
вызывает LLM через HTTP API и возвращает ответ в интерфейс.

## Архитектура

```
UI / HTTP handler  →  agent.Agent.Handle()  →  deepseek.Client (LLM API)
```

- `internal/agent` — инкапсуляция логики запрос/ответ
- `internal/deepseek` — HTTP-клиент к модели
- `internal/handler` — только транспорт (web + JSON API)

## Быстрый старт

```bash
cp .env.example .env
# укажите DEEPSEEK_API_KEY

export $(grep -v '^#' .env | xargs)
go run ./cmd/server
```

Откройте: **http://localhost:8080**

## API

### `POST /api/chat`

```bash
curl -X POST http://localhost:8080/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Привет! Кто ты?"}'
```

Ответ включает `reply`, `agent`, `duration_ms`.

### `GET /health`

```bash
curl http://localhost:8080/health
```

## Структура

```
cmd/server/           — точка входа, сборка Agent + HTTP
internal/agent/       — сущность агента (Handle / buildMessages)
internal/deepseek/    — клиент LLM API
internal/handler/     — HTTP + web UI
internal/config/      — env
```
