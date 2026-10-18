# Как устроен ассистент

MCP `s1` не получает произвольный shell. Модель выбирает инструмент, а сервер сам собирает команду `sudo -n docker` и выполняет её на `10.10.1.101`.

Инструменты дня 33: `list_services`, `service_logs`, `service_status`, `get_ticket`, `get_user`, `list_tickets`.
