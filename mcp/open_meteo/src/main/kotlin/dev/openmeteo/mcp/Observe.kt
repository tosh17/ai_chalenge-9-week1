package dev.openmeteo.mcp

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

private val observationJson = Json { encodeDefaults = true }

@Serializable
data class ObservationPayload(
    val city: String,
    val country: String? = null,
    val latitude: Double,
    val longitude: Double,
    val timezone: String,
    @SerialName("observed_at") val observedAt: String,
    @SerialName("temperature_c") val temperatureC: Double,
    val weather: String,
    val humidity: Double? = null,
    @SerialName("wind_kmh") val windKmh: Double? = null,
    val sunrise: String? = null,
    val sunset: String? = null,
    @SerialName("moon_phase") val moonPhase: Double,
    @SerialName("moon_name") val moonName: String,
)

fun observationJson(place: Place, forecast: Forecast): String {
    val today = forecast.days.firstOrNull()
    val payload = ObservationPayload(
        city = place.name,
        country = place.country,
        latitude = place.latitude,
        longitude = place.longitude,
        timezone = forecast.timezone.ifBlank { place.timezone ?: "UTC" },
        observedAt = forecast.current.time,
        temperatureC = forecast.current.temperature,
        weather = weatherText(forecast.current.weatherCode),
        humidity = forecast.current.humidity,
        windKmh = forecast.current.windSpeed,
        sunrise = today?.sunrise,
        sunset = today?.sunset,
        moonPhase = forecast.moonPhase ?: 0.0,
        moonName = forecast.moonName ?: "неизвестно",
    )
    return observationJson.encodeToString(payload)
}
