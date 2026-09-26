package dev.openmeteo.mcp

import java.util.Locale

data class Place(
    val name: String,
    val country: String?,
    val admin1: String?,
    val latitude: Double,
    val longitude: Double,
    val timezone: String?,
)

data class CurrentWeather(
    val time: String,
    val temperature: Double,
    val apparentTemperature: Double?,
    val humidity: Double?,
    val weatherCode: Int,
    val windSpeed: Double?,
)

data class DayForecast(
    val date: String,
    val weatherCode: Int,
    val temperatureMin: Double,
    val temperatureMax: Double,
    val precipitationMm: Double?,
    val sunrise: String? = null,
    val sunset: String? = null,
)

data class Forecast(
    val timezone: String,
    val current: CurrentWeather,
    val days: List<DayForecast>,
    val moonPhase: Double? = null,
    val moonName: String? = null,
)

private val ru = Locale("ru")

fun weatherText(code: Int): String = when (code) {
    0 -> "ясно"
    1 -> "преимущественно ясно"
    2 -> "переменная облачность"
    3 -> "пасмурно"
    45 -> "туман"
    48 -> "туман с изморозью"
    51 -> "слабая морось"
    53 -> "морось"
    55 -> "сильная морось"
    56 -> "слабая ледяная морось"
    57 -> "сильная ледяная морось"
    61 -> "слабый дождь"
    63 -> "дождь"
    65 -> "сильный дождь"
    66 -> "слабый ледяной дождь"
    67 -> "сильный ледяной дождь"
    71 -> "слабый снег"
    73 -> "снег"
    75 -> "сильный снег"
    77 -> "снежные зёрна"
    80 -> "слабый ливень"
    81 -> "ливень"
    82 -> "сильный ливень"
    85 -> "слабый снегопад"
    86 -> "сильный снегопад"
    95 -> "гроза"
    96 -> "гроза с градом"
    99 -> "гроза с сильным градом"
    else -> "код погоды $code"
}

fun formatReport(place: Place, forecast: Forecast): String = buildString {
    append(placeLabel(place))
    append('\n')
    append(currentLine(forecast.current))
    forecast.days.firstOrNull()?.let { today ->
        if (!today.sunrise.isNullOrBlank() || !today.sunset.isNullOrBlank()) {
            append("\nСолнце: восход ")
            append(today.sunrise ?: "—")
            append(", закат ")
            append(today.sunset ?: "—")
        }
    }
    if (forecast.moonName != null && forecast.moonPhase != null) {
        append("\nЛуна: ")
        append(forecast.moonName)
        append(" (фаза ")
        append(String.format(ru, "%.2f", forecast.moonPhase))
        append(")")
    }
    if (forecast.days.isNotEmpty()) {
        append("\nПрогноз:\n")
        forecast.days.forEach { day ->
            append("- ")
            append(day.date)
            append(": ")
            append(weatherText(day.weatherCode))
            append(", ")
            append(temp(day.temperatureMin))
            append("…")
            append(temp(day.temperatureMax))
            append(" °C")
            day.precipitationMm?.let { append(", осадки ${mm(it)} мм") }
            append('\n')
        }
    }
    append("Данные: Open-Meteo.com https://open-meteo.com/")
}

private fun placeLabel(place: Place): String {
    val where = listOfNotNull(place.admin1, place.country)
        .filter { it.isNotBlank() && !it.equals(place.name, ignoreCase = true) }
        .distinct()
    return if (where.isEmpty()) place.name else "${place.name}, ${where.joinToString(", ")}"
}

private fun currentLine(current: CurrentWeather): String = buildString {
    append("Сейчас (")
    append(current.time)
    append("): ")
    append(temp(current.temperature))
    append(" °C, ")
    append(weatherText(current.weatherCode))
    current.apparentTemperature?.let {
        append(", ощущается как ")
        append(temp(it))
        append(" °C")
    }
    current.windSpeed?.let {
        append(", ветер ")
        append(temp(it))
        append(" км/ч")
    }
    current.humidity?.let {
        append(", влажность ")
        append(it.toInt())
        append('%')
    }
}

private fun temp(value: Double): String = String.format(ru, "%.1f", value)

private fun mm(value: Double): String = String.format(ru, "%.1f", value)
