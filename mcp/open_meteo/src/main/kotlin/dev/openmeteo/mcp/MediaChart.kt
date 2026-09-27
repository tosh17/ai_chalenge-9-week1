package dev.openmeteo.mcp

import java.awt.Color
import java.awt.Font
import java.awt.Graphics2D
import java.awt.RenderingHints
import java.awt.image.BufferedImage
import java.nio.file.Files
import java.nio.file.Path
import javax.imageio.ImageIO
import kotlin.math.max

fun renderMediaChart(summary: MediaSummary): Path {
    System.setProperty("java.awt.headless", "true")
    val width = 960
    val height = 720
    val image = BufferedImage(width, height, BufferedImage.TYPE_INT_RGB)
    val g = image.createGraphics()
    g.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON)
    g.setRenderingHint(RenderingHints.KEY_TEXT_ANTIALIASING, RenderingHints.VALUE_TEXT_ANTIALIAS_ON)
    g.color = Color(0xF6, 0xF3, 0xEC)
    g.fillRect(0, 0, width, height)

    val title = Font("SansSerif", Font.BOLD, 28)
    val body = Font("SansSerif", Font.PLAIN, 16)
    val small = Font("SansSerif", Font.PLAIN, 14)
    g.color = Color(0x1C, 0x1A, 0x17)
    g.font = title
    g.drawString("Медиафайлы", 40, 52)
    g.font = body
    g.color = Color(0x5C, 0x56, 0x4E)
    g.drawString("${summary.files} файлов · ${summary.size}", 40, 82)

    drawSection(g, "По типу", summary.byKind.map { it.title.ifBlank { kindTitle(it.kind) } to it.bytes }, 110, body, small)
    val extRows = summary.byExt.take(8).map { ".${it.ext}" to it.bytes }
    drawSection(g, "По расширению, объём", extRows, 360, body, small)
    g.dispose()

    val dir = mediaWorkDir()
    Files.createDirectories(dir)
    val path = dir.resolve("chart.png")
    ImageIO.write(image, "png", path.toFile())
    return path
}

private fun drawSection(
    g: Graphics2D,
    title: String,
    rows: List<Pair<String, Long>>,
    top: Int,
    body: Font,
    small: Font,
) {
    g.color = Color(0x1C, 0x1A, 0x17)
    g.font = body
    g.drawString(title, 40, top)
    if (rows.isEmpty()) {
        g.font = small
        g.color = Color(0x5C, 0x56, 0x4E)
        g.drawString("нет данных", 40, top + 36)
        return
    }
    val maxBytes = max(1L, rows.maxOf { it.second })
    val colors = listOf(
        Color(0x2F, 0x6F, 0x4E),
        Color(0xC4, 0x5C, 0x26),
        Color(0x3D, 0x5A, 0x80),
        Color(0x8C, 0x4A, 0x6F),
        Color(0x6B, 0x70, 0x5C),
        Color(0xB0, 0x89, 0x2E),
    )
    rows.forEachIndexed { i, (label, bytes) ->
        val y = top + 28 + i * 28
        g.font = small
        g.color = Color(0x1C, 0x1A, 0x17)
        g.drawString(label, 40, y + 14)
        val barX = 180
        val barMax = 560
        val barW = (bytes.toDouble() / maxBytes.toDouble() * barMax).toInt().coerceAtLeast(if (bytes > 0) 4 else 0)
        g.color = colors[i % colors.size]
        g.fillRoundRect(barX, y, barW, 18, 8, 8)
        g.color = Color(0x5C, 0x56, 0x4E)
        g.drawString(formatBytes(bytes), barX + barW + 10, y + 14)
    }
}
