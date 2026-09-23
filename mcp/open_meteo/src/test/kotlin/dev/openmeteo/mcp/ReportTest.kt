package dev.openmeteo.mcp

import kotlinx.serialization.json.JsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class ReportTest {
    @Test
    fun weatherCodesInRussian() {
        assertEquals("ясно", weatherText(0))
        assertEquals("пасмурно", weatherText(3))
        assertEquals("гроза", weatherText(95))
        assertEquals("код погоды 123", weatherText(123))
    }

    @Test
    fun reportMentionsPlaceAndAttribution() {
        val text = formatReport(
            Place(
                name = "Москва",
                country = "Россия",
                admin1 = "Москва",
                latitude = 55.75,
                longitude = 37.62,
                timezone = "Europe/Moscow",
            ),
            Forecast(
                timezone = "Europe/Moscow",
                current = CurrentWeather(
                    time = "2026-09-23T12:30",
                    temperature = 17.5,
                    apparentTemperature = 16.2,
                    humidity = 60.0,
                    weatherCode = 3,
                    windSpeed = 6.2,
                ),
                days = listOf(
                    DayForecast("2026-09-23", 3, 12.0, 20.4, 0.0),
                ),
            ),
        )
        assertTrue(text.contains("Москва, Россия"))
        assertTrue(text.contains("пасмурно"))
        assertTrue(text.contains("17,5"))
        assertTrue(text.contains("https://open-meteo.com/"))
    }

    @Test
    fun daysDefaultAndRange() {
        assertEquals(DEFAULT_FORECAST_DAYS, (parseDays(null) as DaysParse.Ok).value)
        assertEquals(2, (parseDays(JsonPrimitive(2)) as DaysParse.Ok).value)
        assertTrue(parseDays(JsonPrimitive(0)) is DaysParse.Invalid)
        assertTrue(parseDays(JsonPrimitive(8)) is DaysParse.Invalid)
    }
}
