package dev.openmeteo.mcp

fun main() {
    // kotlin-logging по умолчанию печатает баннер в stdout и ломает MCP.
    System.setProperty("kotlin-logging.logStartupMessage", "false")
    runMcpServer()
}
