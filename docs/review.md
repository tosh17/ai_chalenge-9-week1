# Ревью merge request

День 32. На каждый merge request в GitLab ассистент читает diff, ищет контекст в документации и коде и пишет ревью.

## Где запускается

GitLab стоит на s1, это домашний сервер `10.10.1.101`.

| | |
| --- | --- |
| Веб | http://10.10.1.101:8929 |
| Git SSH | `ssh://git@10.10.1.101:2224` |
| API | http://10.10.1.101:8929/api/v4 |
| Раннер | `local`, executor docker, сеть `lab`, `clone_url` `http://gitlab:8929` |

Файл пайплайна — `.gitlab-ci.yml`. Джоба `ai-review` стартует только на `merge_request_event`. Образ `golang:1.22-bookworm`. Команда: `go run ./cmd/review -out review.md`. Текст попадает в лог джобы и в артефакт `review.md`. Если заданы `CI_PROJECT_ID` и `CI_MERGE_REQUEST_IID`, тот же текст уходит комментарием к MR.

Комментарий авторизуется так: `REVIEW_GITLAB_TOKEN` заголовком `PRIVATE-TOKEN`, иначе `CI_JOB_TOKEN` заголовком `JOB-TOKEN`. Ошибка публикации не отменяет ревью: текст уже в логе и в артефакте.

## Что получает ассистент

1. Diff. В CI это `git diff $CI_MERGE_REQUEST_DIFF_BASE_SHA...$CI_COMMIT_SHA`. Локально без `-base` — `git diff HEAD`.
2. Список изменённых файлов из заголовков `diff --git`.
3. RAG. Индекс отдельный от `/help`: `README.md`, `docs/**/*.md` и все `*.go` кроме служебных каталогов. Эмбеддинги — Ollama `nomic-embed-text` на `http://10.10.1.101:11434`. Кэш — `data/rag/review-index.json`. За один запуск считается не больше 48 новых векторов, чтобы джоба не зависала на полном дереве. Остальное добирается из кэша и, если вектора нет, лексическим поиском.

В промпт кладутся пути, усечённый diff и до 6 фрагментов.

## Что возвращает

Три раздела, по-русски:

- потенциальные баги
- архитектурные проблемы
- рекомендации

Модель — DeepSeek, если в окружении есть `DEEPSEEK_API_KEY` или `REVIEW_API_KEY` и URL `REVIEW_API_URL` (в CI это `https://api.deepseek.com/chat/completions`). С s1 до API сеть есть. Локальная Qwen с s1 сейчас не открывается, поэтому пайплайн на неё не рассчитывает.

Без ключа ревью всё равно собирается: добавленные строки с `panic`, `TODO`/`FIXME` и `_ = err` попадают в баги, файлы вне `cmd`, `internal`, `docs` и `mcp` — в архитектуру, ближайшие фрагменты индекса — в рекомендации.

## Локальный прогон

```bash
set -a && source .env && set +a
go run ./cmd/review -base day31 -head HEAD -out review.md
```

## Переменная CI

В проекте GitLab: Settings → CI/CD → Variables → `DEEPSEEK_API_KEY`, masked. Ключ в репозиторий не кладётся.
