# DeepSeek Web Service

Веб-сервис на Go, который проксирует запросы к DeepSeek API.

## Требования

- Go 1.22+
- API-ключ DeepSeek ([platform.deepseek.com](https://platform.deepseek.com))

## Быстрый старт

```bash
# Скопируйте переменные окружения
cp .env.example .env
# Укажите DEEPSEEK_API_KEY в .env

export $(grep -v '^#' .env | xargs)

# Запуск
go run ./cmd/server
```

Откройте в браузере: **http://localhost:8080**

## API

### `GET /`

Веб-интерфейс чата.

### `GET /health`

Проверка состояния сервиса.

```bash
curl http://localhost:8080/health
```

### `POST /api/chat`

Отправка сообщения в DeepSeek.

```bash
curl -X POST http://localhost:8080/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "Привет! Расскажи про Go."}'
```

С историей диалога:

```bash
curl -X POST http://localhost:8080/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "А что насчёт goroutines?",
    "history": [
      {"role": "user", "content": "Привет! Расскажи про Go."},
      {"role": "assistant", "content": "Go — язык от Google..."}
    ]
  }'
```

## Переменные окружения

| Переменная         | По умолчанию                                  | Описание              |
|--------------------|-----------------------------------------------|-----------------------|
| `DEEPSEEK_API_KEY` | —                                             | API-ключ (обязательно)|
| `DEEPSEEK_MODEL`   | `deepseek-v4-flash`                           | Модель DeepSeek       |
| `DEEPSEEK_API_URL` | `https://api.deepseek.com/chat/completions`   | URL API               |
| `PORT`             | `8080`                                        | Порт сервера          |

## Структура проекта

```
cmd/server/              — точка входа
internal/config/         — конфигурация из env
internal/deepseek/       — клиент DeepSeek API
internal/handler/        — HTTP-обработчики и веб-UI
internal/handler/web/    — HTML, CSS, JS чата
```
