package dev.openmeteo.mcp

import java.time.Instant
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class MoonTest {
    @Test
    fun knownNewMoonIsNearZero() {
        val moon = moonPhase(Instant.parse("2000-01-06T18:14:00Z"))
        assertTrue(moon.fraction < 0.02 || moon.fraction > 0.98, "fraction=${moon.fraction}")
        assertEquals("новолуние", moon.name)
    }

    @Test
    fun halfSynodicMonthIsFull() {
        val moon = moonPhase(Instant.parse("2000-01-21T12:24:00Z"))
        assertTrue(moon.fraction in 0.47..0.53, "fraction=${moon.fraction}")
        assertEquals("полнолуние", moon.name)
    }
}
