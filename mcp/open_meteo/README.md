# Open-Meteo MCP

Учебный MCP-сервер на Kotlin. Инструмент `get_weather` берёт погоду с [Open-Meteo](https://open-meteo.com/) без ключа и регистрации.

Бесплатный доступ — некоммерческий, до 10 000 запросов в день. В каждом ответе есть ссылка на источник.

## Сборка

Нужен JDK 17+, чтобы запустить Gradle. Компиляция идёт toolchain JDK 17.

```bash
./gradlew shadowJar
```

Jar: `build/libs/open-meteo-0.1.0-all.jar`

## Запуск

Сервер говорит по stdin/stdout. Логи только в stderr.

```bash
java -jar build/libs/open-meteo-0.1.0-all.jar
```

Подключение в Cursor (`~/.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "open-meteo": {
      "command": "java",
      "args": [
        "-jar",
        "mcp/open_meteo/build/libs/open-meteo-0.1.0-all.jar"
      ]
    }
  }
}
```

## Инструмент

`get_weather`

| Параметр | Обязателен | Смысл |
| --- | --- | --- |
| `city` | да | Город: `Москва`, `Berlin` |
| `days` | нет | Дней прогноза, 1–7, по умолчанию 3 |
