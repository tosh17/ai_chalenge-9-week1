package dev.openmeteo.mcp

import java.nio.file.Files
import kotlin.io.path.writeBytes
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class MediaTest {
    @Test
    fun searchSummarizeAndChartPassTheSameTotals() {
        val root = Files.createTempDirectory("media-chain")
        root.resolve("photo.jpg").writeBytes(ByteArray(10))
        root.resolve("clip.mp4").writeBytes(ByteArray(100))
        root.resolve("note.txt").writeBytes(ByteArray(50))
        val library = Files.createDirectory(root.resolve("Library"))
        library.resolve("cache.png").writeBytes(ByteArray(80))
        val nested = Files.createDirectories(root.resolve("Pictures"))
        nested.resolve("scan.png").writeBytes(ByteArray(20))

        val found = scanMedia(root)
        assertEquals(3, found.size)
        val inventoryPath = writeInventory(root, found)
        val summary = summarizeMedia(readInventory(inventoryPath), inventoryPath.toString())
        assertEquals(3, summary.files)
        assertEquals(130, summary.bytes)
        val images = summary.byKind.first { it.kind == "image" }
        val videos = summary.byKind.first { it.kind == "video" }
        assertEquals(2, images.files)
        assertEquals(30, images.bytes)
        assertEquals(1, videos.files)
        assertEquals(100, videos.bytes)

        val png = renderMediaChart(summary)
        val bytes = Files.readAllBytes(png)
        assertTrue(bytes.size > 8)
        assertEquals(0x89.toByte(), bytes[0])
        assertEquals('P'.code.toByte(), bytes[1])
        assertEquals('N'.code.toByte(), bytes[2])
        assertEquals('G'.code.toByte(), bytes[3])
    }
}
