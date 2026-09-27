package dev.openmeteo.mcp

import io.modelcontextprotocol.kotlin.sdk.server.Server
import io.modelcontextprotocol.kotlin.sdk.types.CallToolResult
import io.modelcontextprotocol.kotlin.sdk.types.TextContent
import io.modelcontextprotocol.kotlin.sdk.types.ToolAnnotations
import io.modelcontextprotocol.kotlin.sdk.types.ToolSchema
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonObject
import java.nio.file.Path

private val toolJson = Json { encodeDefaults = true }

fun Server.registerMediaTools() {
    registerSearchMedia()
    registerSummarizeMedia()
    registerChartMedia()
}

private fun Server.registerSearchMedia() {
    addTool(
        name = "search_media",
        description = "Первый шаг цепочки. Ищет картинки и видео в домашней папке и сохраняет инвентарь. " +
            "В ответе inventory_path — его целиком передай в summarize_media. Числа по типам не считай сам.",
        inputSchema = ToolSchema(
            properties = buildJsonObject {
                putJsonObject("root") {
                    put("type", "string")
                    put("description", "Каталог. Пусто — домашняя папка пользователя.")
                }
            },
        ),
        toolAnnotations = ToolAnnotations(readOnlyHint = true, openWorldHint = false),
    ) { request ->
        val rawRoot = request.arguments?.get("root").asText()?.trim().orEmpty()
        val root = if (rawRoot.isEmpty()) defaultMediaRoot() else Path.of(rawRoot)
        try {
            val files = scanMedia(root)
            val inventory = writeInventory(root, files)
            val bytes = files.sumOf { it.bytes }
            val payload = MediaSearchResult(
                root = root.toAbsolutePath().normalize().toString(),
                inventoryPath = inventory.toString(),
                files = files.size,
                bytes = bytes,
                size = formatBytes(bytes),
                skipped = skippedDirNames(),
            )
            CallToolResult(content = listOf(TextContent(toolJson.encodeToString(payload))))
        } catch (e: Exception) {
            errorResult("Не удалось обойти каталог: ${e.message ?: e::class.simpleName}")
        }
    }
}

private fun Server.registerSummarizeMedia() {
    addTool(
        name = "summarize_media",
        description = "Второй шаг цепочки. Считает, сколько картинок и видео и какой объём, по inventory_path из search_media. " +
            "В chart_media передай только summary_path, не сам JSON.",
        inputSchema = ToolSchema(
            properties = buildJsonObject {
                putJsonObject("inventory_path") {
                    put("type", "string")
                    put("description", "inventory_path из ответа search_media")
                }
            },
            required = listOf("inventory_path"),
        ),
        toolAnnotations = ToolAnnotations(readOnlyHint = true, openWorldHint = false),
    ) { request ->
        val path = request.arguments?.get("inventory_path").asText()?.trim().orEmpty()
        if (path.isEmpty()) {
            return@addTool errorResult("Нужен inventory_path из search_media.")
        }
        try {
            val inventory = readInventory(Path.of(path))
            val (_, stored) = writeSummary(summarizeMedia(inventory, path))
            CallToolResult(content = listOf(TextContent(summaryJson(stored))))
        } catch (e: Exception) {
            errorResult("Не удалось собрать сводку: ${e.message ?: e::class.simpleName}")
        }
    }
}

private fun Server.registerChartMedia() {
    addTool(
        name = "chart_media",
        description = "Третий шаг цепочки. Строит график по summary_path из summarize_media, сохраняет PNG и возвращает image_url. JSON сводки не передавай.",
        inputSchema = ToolSchema(
            properties = buildJsonObject {
                putJsonObject("summary_path") {
                    put("type", "string")
                    put("description", "summary_path из ответа summarize_media")
                }
            },
            required = listOf("summary_path"),
        ),
        toolAnnotations = ToolAnnotations(readOnlyHint = false, openWorldHint = false),
    ) { request ->
        val summaryPath = request.arguments?.get("summary_path").asText()?.trim().orEmpty()
        if (summaryPath.isEmpty()) {
            return@addTool errorResult("Нужен summary_path из summarize_media, не JSON сводки.")
        }
        try {
            val summary = parseSummary(java.nio.file.Files.readString(Path.of(summaryPath)))
            val png = renderMediaChart(summary)
            val payload = ChartResult(
                savedPath = png.toString(),
                imageUrl = "/media/chart.png",
                bytes = png.toFile().length(),
            )
            CallToolResult(content = listOf(TextContent(toolJson.encodeToString(payload))))
        } catch (e: Exception) {
            errorResult("Не удалось построить график: ${e.message ?: e::class.simpleName}")
        }
    }
}

@Serializable
private data class ChartResult(
    @kotlinx.serialization.SerialName("saved_path") val savedPath: String,
    @kotlinx.serialization.SerialName("image_url") val imageUrl: String,
    val bytes: Long,
)
