# week1 — Go web-чат

Рабочая папка: `/Users/tosh17/pet/ai-chalenge/week1` (ветка day9+). Это не Node/TypeScript: `package.json` нет.

Стек: Go. UI → `internal/handler` → `internal/agent.Agent` → LLM (`internal/deepseek`, DeepSeek и локальный Qwen).

- `cmd/server` — HTTP-сервер, `:8080`
- `internal/agent` — агент, память, сжатие истории, стратегии контекста (sliding/facts/branch)
- `internal/memory` — JSON-история чата
- `internal/tokens` — подсчёт токенов
- `internal/handler/web` — HTML/JS чат

Правила: всегда вызывай инструменты (`glob`, `read`, `bash`). Не угадывай пути и файлы. История диалога в этой сессии валидна — не проси «начать с нуля».
