package dev.openmeteo.mcp

import java.time.Instant
import java.time.LocalDateTime
import java.time.ZoneId

data class Moon(val fraction: Double, val name: String)

private const val SYNODIC_MILLIS = 29.530588853 * 24.0 * 60.0 * 60.0 * 1000.0
private val KNOWN_NEW_MOON: Instant = Instant.parse("2000-01-06T18:14:00Z")

fun moonPhase(instant: Instant): Moon {
    var age = (instant.toEpochMilli() - KNOWN_NEW_MOON.toEpochMilli()).toDouble() % SYNODIC_MILLIS
    if (age < 0) age += SYNODIC_MILLIS
    val fraction = age / SYNODIC_MILLIS
    return Moon(fraction, moonName(fraction))
}

fun moonAt(localDateTime: String, zone: String?): Moon {
    val zoneId = runCatching { ZoneId.of(zone?.takeIf { it.isNotBlank() } ?: "UTC") }.getOrDefault(ZoneId.of("UTC"))
    val parsed = runCatching { LocalDateTime.parse(localDateTime) }.getOrNull()
        ?: return moonPhase(Instant.now())
    return moonPhase(parsed.atZone(zoneId).toInstant())
}

fun moonName(fraction: Double): String {
    val x = ((fraction % 1.0) + 1.0) % 1.0
    return when {
        x < 0.03 || x >= 0.97 -> "новолуние"
        x < 0.22 -> "растущий серп"
        x < 0.28 -> "первая четверть"
        x < 0.47 -> "растущая луна"
        x < 0.53 -> "полнолуние"
        x < 0.72 -> "убывающая луна"
        x < 0.78 -> "последняя четверть"
        else -> "убывающий серп"
    }
}
