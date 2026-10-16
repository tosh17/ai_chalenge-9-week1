# MCP

Инструменты ходят по JSON-RPC через stdin/stdout, протокол `2024-11-05`. Клиент — `internal/mcp`. Сервер на Go — `cmd/mcp`, реализация — `internal/mcpsrv`. Погода — отдельный процесс Kotlin.

Чат поднимает процессы в `cmd/server/main.go`, если `MCP_ENABLED=true`. Бинарник ищется в `MCP_BIN` или `bin/mcp`. JAR погоды — в `MCP_JAR` или `mcp/open_meteo/build/libs/open-meteo-0.1.0-all.jar`.

## project — репозиторий

Запуск: `bin/mcp project`. Корень — `PROJECT_ROOT` или текущий каталог процесса. Этот сервер подключён к `/help`, а не к кулинарному агенту: рецептам незачем читать git.

### git_branch

Аргументов нет. Ответ:

```json
{"branch": "day31", "head": "abc1234", "dirty": 2}
```

`branch` — `git rev-parse --abbrev-ref HEAD`. `head` — короткий SHA. `dirty` — число строк `git status --porcelain`.

### list_files

```json
{"path": "docs", "depth": 2}
```

`path` относительный, выход из корня репозитория отвергается. `depth` по умолчанию 2, максимум 6. Скрываются `.git`, `bin`, `data`, `build`, `.gradle`, `node_modules`. В ответе поле `tree` — текст с отступами, не больше 200 записей.

### git_diff

```json
{"path": "README.md", "staged": false, "stat_only": true}
```

`status` — `git status -sb`. `stat` — `git diff --stat`. `patch` пустой, когда `stat_only` истинно. `/help` просит патч, только если вопрос про diff или незакоммиченные изменения. Иначе в промпт попадает сводка.

## search — страницы и фото

`bin/mcp search`.

| Инструмент | Назначение |
| --- | --- |
| `web_search` | английский запрос `q`, до 5 ссылок |
| `web_fetch` | текст одной http(s) страницы |
| `web_image` | `image_url` фото блюда |

## translate

`bin/mcp translate`. Один инструмент `translate_en_ru`: английский фрагмент рецепта в русский. В ответ пользователю английские абзацы не кладутся.

## diagram

`bin/mcp diagram`. Инструмент `draw_flow` рисует png блок-схемы хода и отдаёт путь `/media/...`.

## open-meteo

Kotlin-процесс, JAR. Инструменты `get_weather` и `observe_city`. В том же JAR есть медиа-цепочка `search_media`, `summarize_media`, `chart_media` — её использует отдельный сценарий, не каждый кулинарный ход.

`cmd/observe` запускает тот же JAR и сам пишет архив. Чат архив не собирает.

## Наблюдения как инструменты агента

Поверх архива агент регистрирует `record_observation` и `observation_report`. Это не stdio-сервер `project`: отчёт читает JSONL из `OBSERVE_DIR`.
