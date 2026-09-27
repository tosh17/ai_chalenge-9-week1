package dev.openmeteo.mcp

import io.ktor.utils.io.streams.asInput
import io.modelcontextprotocol.kotlin.sdk.server.Server
import io.modelcontextprotocol.kotlin.sdk.server.ServerOptions
import io.modelcontextprotocol.kotlin.sdk.server.StdioServerTransport
import io.modelcontextprotocol.kotlin.sdk.types.CallToolResult
import io.modelcontextprotocol.kotlin.sdk.types.Implementation
import io.modelcontextprotocol.kotlin.sdk.types.ServerCapabilities
import io.modelcontextprotocol.kotlin.sdk.types.TextContent
import io.modelcontextprotocol.kotlin.sdk.types.ToolAnnotations
import io.modelcontextprotocol.kotlin.sdk.types.ToolSchema
import kotlinx.coroutines.Job
import kotlinx.coroutines.runBlocking
import kotlinx.io.asSink
import kotlinx.io.buffered
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonObject

const val MIN_FORECAST_DAYS = 1
const val MAX_FORECAST_DAYS = 7
const val DEFAULT_FORECAST_DAYS = 3

fun runMcpServer() {
    run {
        val server = Server(
            Implementation(name = "media", version = "0.1.0"),
            ServerOptions(
                capabilities = ServerCapabilities(
                    tools = ServerCapabilities.Tools(listChanged = true),
                ),
            ),
        )
        server.registerMediaTools()

        val transport = StdioServerTransport(
            input = System.`in`.asInput(),
            output = System.out.asSink().buffered(),
        )

        runBlocking {
            val session = server.createSession(transport)
            val done = Job()
            session.onClose { done.complete() }
            done.join()
        }
    }
}

private fun Server.registerWeatherTool(client: OpenMeteoClient) {
    addTool(
        name = "get_weather",
        description = "Текущая погода, восход, закат, фаза луны и короткий прогноз по городу через Open-Meteo. " +
            "Учебное некоммерческое использование. В ответе уже есть ссылка на источник.",
        inputSchema = ToolSchema(
            properties = buildJsonObject {
                putJsonObject("city") {
                    put("type", "string")
                    put("description", "Название города, например Москва или Berlin")
                }
                putJsonObject("days") {
                    put("type", "integer")
                    put("description", "Сколько дней прогноза, от $MIN_FORECAST_DAYS до $MAX_FORECAST_DAYS. По умолчанию $DEFAULT_FORECAST_DAYS")
                    put("minimum", MIN_FORECAST_DAYS)
                    put("maximum", MAX_FORECAST_DAYS)
                }
            },
            required = listOf("city"),
        ),
        toolAnnotations = ToolAnnotations(readOnlyHint = true, openWorldHint = true),
    ) { request ->
        val city = request.arguments?.get("city").asText()?.trim().orEmpty()
        if (city.isEmpty()) {
            return@addTool errorResult("Нужен параметр city — название города.")
        }
        val days = when (val parsed = parseDays(request.arguments?.get("days"))) {
            is DaysParse.Ok -> parsed.value
            is DaysParse.Invalid -> return@addTool errorResult(parsed.message)
        }
        try {
            CallToolResult(content = listOf(TextContent(client.report(city, days))))
        } catch (e: OpenMeteoException) {
            errorResult(e.message ?: "Open-Meteo недоступен")
        } catch (e: Exception) {
            errorResult("Не удалось получить погоду: ${e.message ?: e::class.simpleName}")
        }
    }
}

sealed interface DaysParse {
    data class Ok(val value: Int) : DaysParse
    data class Invalid(val message: String) : DaysParse
}

fun parseDays(raw: JsonElement?): DaysParse {
    if (raw == null || raw is kotlinx.serialization.json.JsonNull) {
        return DaysParse.Ok(DEFAULT_FORECAST_DAYS)
    }
    if (raw is JsonPrimitive && raw.isString && raw.contentOrNull.isNullOrBlank()) {
        return DaysParse.Ok(DEFAULT_FORECAST_DAYS)
    }
    val days = raw.jsonPrimitive.intOrNull ?: raw.jsonPrimitive.contentOrNull?.toIntOrNull()
        ?: return DaysParse.Invalid("Параметр days должен быть целым числом от $MIN_FORECAST_DAYS до $MAX_FORECAST_DAYS.")
    if (days !in MIN_FORECAST_DAYS..MAX_FORECAST_DAYS) {
        return DaysParse.Invalid("Параметр days должен быть от $MIN_FORECAST_DAYS до $MAX_FORECAST_DAYS, получено $days.")
    }
    return DaysParse.Ok(days)
}

internal fun JsonElement?.asText(): String? {
    val primitive = this as? JsonPrimitive ?: return null
    return primitive.contentOrNull
}

private fun Server.registerObserveTool(client: OpenMeteoClient) {
    addTool(
        name = "observe_city",
        description = "Структурированные наблюдения города: температура, погода, восход, закат и фаза луны. Ответ — один JSON-объект.",
        inputSchema = ToolSchema(
            properties = buildJsonObject {
                putJsonObject("city") {
                    put("type", "string")
                    put("description", "Название города, например Волгоград")
                }
            },
            required = listOf("city"),
        ),
        toolAnnotations = ToolAnnotations(readOnlyHint = true, openWorldHint = true),
    ) { request ->
        val city = request.arguments?.get("city").asText()?.trim().orEmpty()
        if (city.isEmpty()) {
            return@addTool errorResult("Нужен параметр city — название города.")
        }
        try {
            CallToolResult(content = listOf(TextContent(client.observe(city))))
        } catch (e: OpenMeteoException) {
            errorResult(e.message ?: "Open-Meteo недоступен")
        } catch (e: Exception) {
            errorResult("Не удалось снять наблюдения: ${e.message ?: e::class.simpleName}")
        }
    }
}

internal fun errorResult(message: String) = CallToolResult(
    content = listOf(TextContent(message)),
    isError = true,
)
