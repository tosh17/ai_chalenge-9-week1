# Конфигурация

Файл `.env` в корне. Образец — `.env.example`. Сервер читает переменные окружения, сам файл `.env` не парсит: его подхватывает оболочка или способ запуска. Обязательна только `DEEPSEEK_API_KEY`, даже если по умолчанию выбран локальный провайдер.

## Модели

| Переменная | По умолчанию | Смысл |
| --- | --- | --- |
| `DEEPSEEK_API_KEY` | нет | ключ облака, без него процесс не стартует |
| `DEEPSEEK_MODEL` | `deepseek-v4-flash` | id модели |
| `DEEPSEEK_API_URL` | `https://api.deepseek.com/chat/completions` | полный URL метода |
| `PORT` | `8080` | порт чата |
| `LOCAL_ENABLED` | включён, если задан URL | локальная Qwen |
| `LOCAL_API_URL` | пусто | `http://10.10.0.220:8787/v1` или сразу `.../chat/completions` |
| `LOCAL_API_KEY` | пусто | bearer, если хост его требует |
| `LOCAL_MODEL` | `qwen-local` | id в `/v1/models` |
| `LOCAL_TITLE` | `Local-Qwen` | подпись в списке |
| `LOCAL_CONTEXT_LIMIT` | `256000` | лимит счётчика для local |
| `CONTEXT_TOKEN_LIMIT` | `1000000` | лимит счётчика для DeepSeek |

## Память и стратегия

| Переменная | По умолчанию | Смысл |
| --- | --- | --- |
| `CHAT_HISTORY_PATH` | `data/chat-history.json` | файл старого хранилища |
| `MEMORY_DIR` | `data/memory` | каталог слоёв, профилей, инвариантов |
| `STM_WINDOW_N` | `8` | сколько реплик short-term брать |
| `MEMORY_INJECT_STM` | `false` | подмешивать короткую память |
| `MEMORY_INJECT_WM` | `false` | подмешивать рабочую |
| `MEMORY_INJECT_LTM` | `false` | подмешивать долгую |
| `MEMORY_INJECT_PROFILE` | `true` | подмешивать активный профиль |
| `MEMORY_INJECT_INVARIANTS` | `false` | подмешивать включённые правила |
| `CONTEXT_STRATEGY` | `sliding` | `sliding`, `facts` или `branch` |
| `SLIDING_WINDOW_N` | `8` | окно sliding |
| `FACTS_WINDOW_N` | `6` | хвост рядом с фактами |
| `BRANCH_WINDOW_N` | `0` | 0 — вся активная ветка диалога |
| `COMPRESS_ENABLED` | в примере `false` | сжатие дня 9, сервер всё равно выключает его |

## MCP, наблюдения, RAG

| Переменная | По умолчанию | Смысл |
| --- | --- | --- |
| `MCP_ENABLED` | `true` | поднимать stdio-серверы |
| `MCP_JAR` | путь к JAR в репозитории | Open-Meteo |
| `MCP_JAVA` | `java` из PATH | интерпретатор JAR |
| `MCP_BIN` | `bin/mcp` | бинарник Go-серверов |
| `PROJECT_ROOT` | текущий каталог | корень для `git_branch` и `list_files` |
| `OBSERVE_ENABLED` | `true` | процесс `cmd/observe` |
| `OBSERVE_CITY` | `Волгоград` | город архива |
| `OBSERVE_EVERY` | `15m` | пауза между замерами |
| `OBSERVE_DIR` | `data/observations` | каталог JSONL |
| `RAG_ENABLED` | `true` | строить индекс и принимать `/help` |
| `RAG_OLLAMA_URL` | `http://10.10.1.101:11434` | Ollama с эмбеддингами |
| `RAG_EMBED_MODEL` | `nomic-embed-text` | имя модели в `/api/embed` |
| `RAG_TOP_K` | `5` | сколько чанков класть в промпт |
| `RAG_CACHE_PATH` | `data/rag/index.json` | кэш векторов |

`RAG_ENABLED=false` оставляет кулинарный чат и отвечает на `/help` отказом. Сами файлы `docs/` при этом никуда не деваются.
