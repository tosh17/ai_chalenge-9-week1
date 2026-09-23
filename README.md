# Day 16 · MCP Open-Meteo

Чат подключает отдельный MCP-сервер и даёт модели инструмент `get_weather`. Температуру агент берёт из [Open-Meteo](https://open-meteo.com/), а не из весов модели.

Сервер на Kotlin лежит в `mcp/open_meteo`. Протокол — JSON по строке на stdin/stdout.

## Проверка

1. Соберите jar, если его ещё нет:

```bash
cd mcp/open_meteo
./gradlew shadowJar
```

2. Запустите чат из этой папки:

```bash
cp .env.example .env
export $(grep -v '^#' .env | xargs)
go run ./cmd/server
```

3. Откройте **http://localhost:8080** и спросите: «Какая сейчас погода в Москве?»

Над ответом будет строка `MCP get_weather · Москва`. В тексте — температура и ссылка на Open-Meteo.

`GET /health` показывает `"mcp": {"connected": true, "tools": ["get_weather"]}`.

Жизненный цикл задачи с дня 15 на месте: этап по-прежнему нельзя перепрыгнуть.

## Настройка

| Переменная | Смысл |
| --- | --- |
| `MCP_ENABLED` | `true` по умолчанию |
| `MCP_JAR` | путь к `open-meteo-0.1.0-all.jar`, иначе `mcp/open_meteo/build/libs/...` |
| `MCP_JAVA` | бинарник JDK 17+. Пусто — `JAVA_HOME`, затем JDK из кэша Gradle |

## Структура

```
internal/mcp/client.go   — stdio-клиент MCP
internal/agent/tools.go  — цикл: модель просит инструмент → MCP → ответ модели
cmd/server/main.go       — поднимает процесс Open-Meteo при старте
```
