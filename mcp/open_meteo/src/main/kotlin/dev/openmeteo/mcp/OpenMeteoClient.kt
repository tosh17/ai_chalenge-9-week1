package dev.openmeteo.mcp

import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.engine.cio.CIO
import io.ktor.client.plugins.HttpTimeout
import io.ktor.client.plugins.contentnegotiation.ContentNegotiation
import io.ktor.client.request.get
import io.ktor.client.request.parameter
import io.ktor.http.HttpStatusCode
import io.ktor.http.isSuccess
import io.ktor.serialization.kotlinx.json.json
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

class OpenMeteoException(message: String) : Exception(message)

class OpenMeteoClient(
    private val http: HttpClient = defaultHttpClient(),
) : AutoCloseable {
    override fun close() {
        http.close()
    }

    suspend fun report(city: String, days: Int): String {
        val place = geocode(city)
        val forecast = forecast(place, days)
        return formatReport(place, forecast)
    }

    suspend fun observe(city: String): String {
        val place = geocode(city)
        val forecast = forecast(place, 1)
        return observationJson(place, forecast)
    }

    private suspend fun geocode(city: String): Place {
        val response = http.get("https://geocoding-api.open-meteo.com/v1/search") {
            parameter("name", city)
            parameter("count", 1)
            parameter("language", "ru")
            parameter("format", "json")
        }
        ensureOk(response.status, "геокодинг")
        val body = response.body<GeocodingResponse>()
        val hit = body.results?.firstOrNull()
            ?: throw OpenMeteoException("Город «$city» не найден")
        return Place(
            name = hit.name,
            country = hit.country,
            admin1 = hit.admin1,
            latitude = hit.latitude,
            longitude = hit.longitude,
            timezone = hit.timezone,
        )
    }

    private suspend fun forecast(place: Place, days: Int): Forecast {
        val response = http.get("https://api.open-meteo.com/v1/forecast") {
            parameter("latitude", place.latitude)
            parameter("longitude", place.longitude)
            parameter(
                "current",
                "temperature_2m,apparent_temperature,relative_humidity_2m,weather_code,wind_speed_10m",
            )
            parameter(
                "daily",
                "weather_code,temperature_2m_max,temperature_2m_min,precipitation_sum,sunrise,sunset",
            )
            parameter("forecast_days", days)
            parameter("timezone", place.timezone ?: "auto")
        }
        ensureOk(response.status, "прогноз")
        return response.body<ForecastResponse>().toForecast()
    }

    private fun ensureOk(status: HttpStatusCode, what: String) {
        if (!status.isSuccess()) {
            throw OpenMeteoException("Open-Meteo ($what) ответил ${status.value}")
        }
    }
}

fun defaultHttpClient(): HttpClient = HttpClient(CIO) {
    install(HttpTimeout) {
        requestTimeoutMillis = 15_000
        connectTimeoutMillis = 10_000
    }
    install(ContentNegotiation) {
        json(
            Json {
                ignoreUnknownKeys = true
            },
        )
    }
}

@Serializable
private data class GeocodingResponse(
    val results: List<GeocodingHit>? = null,
)

@Serializable
private data class GeocodingHit(
    val name: String,
    val latitude: Double,
    val longitude: Double,
    val country: String? = null,
    val admin1: String? = null,
    val timezone: String? = null,
)

@Serializable
private data class ForecastResponse(
    val timezone: String = "",
    val current: CurrentDto,
    val daily: DailyDto,
) {
    fun toForecast(): Forecast {
        val days = daily.time.indices.map { i ->
            DayForecast(
                date = daily.time[i],
                weatherCode = daily.weatherCode.getOrElse(i) { -1 },
                temperatureMin = daily.temperatureMin.getOrElse(i) { Double.NaN },
                temperatureMax = daily.temperatureMax.getOrElse(i) { Double.NaN },
                precipitationMm = daily.precipitationSum.getOrNull(i),
                sunrise = daily.sunrise.getOrNull(i),
                sunset = daily.sunset.getOrNull(i),
            )
        }
        val moon = moonAt(current.time, timezone)
        return Forecast(
            timezone = timezone,
            moonPhase = moon.fraction,
            moonName = moon.name,
            current = CurrentWeather(
                time = current.time,
                temperature = current.temperature,
                apparentTemperature = current.apparentTemperature,
                humidity = current.humidity,
                weatherCode = current.weatherCode,
                windSpeed = current.windSpeed,
            ),
            days = days,
        )
    }
}

@Serializable
private data class CurrentDto(
    val time: String,
    @SerialName("temperature_2m") val temperature: Double,
    @SerialName("apparent_temperature") val apparentTemperature: Double? = null,
    @SerialName("relative_humidity_2m") val humidity: Double? = null,
    @SerialName("weather_code") val weatherCode: Int,
    @SerialName("wind_speed_10m") val windSpeed: Double? = null,
)

@Serializable
private data class DailyDto(
    val time: List<String> = emptyList(),
    @SerialName("weather_code") val weatherCode: List<Int> = emptyList(),
    @SerialName("temperature_2m_max") val temperatureMax: List<Double> = emptyList(),
    @SerialName("temperature_2m_min") val temperatureMin: List<Double> = emptyList(),
    @SerialName("precipitation_sum") val precipitationSum: List<Double> = emptyList(),
    val sunrise: List<String> = emptyList(),
    val sunset: List<String> = emptyList(),
)
