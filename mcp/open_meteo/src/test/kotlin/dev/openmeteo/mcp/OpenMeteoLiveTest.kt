package dev.openmeteo.mcp

import kotlinx.coroutines.runBlocking
import kotlin.test.Test
import kotlin.test.assertTrue

class OpenMeteoLiveTest {
    @Test
    fun moscowForecastComesFromOpenMeteo() = runBlocking {
        OpenMeteoClient().use { client ->
            val text = client.report("Москва", 2)
            assertTrue(text.contains("Москва"), text)
            assertTrue(text.contains("°C"), text)
            assertTrue(text.contains("https://open-meteo.com/"), text)
        }
    }
}
