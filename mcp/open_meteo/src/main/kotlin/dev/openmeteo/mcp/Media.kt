package dev.openmeteo.mcp

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import java.nio.file.FileVisitResult
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.SimpleFileVisitor
import java.nio.file.attribute.BasicFileAttributes
import java.util.Locale
import kotlin.io.path.name

private val mediaJson = Json { encodeDefaults = true; ignoreUnknownKeys = true }

private val imageExt = setOf(
    "jpg", "jpeg", "png", "gif", "webp", "heic", "heif", "bmp", "tif", "tiff", "svg", "avif", "raw", "cr2", "nef", "dng",
)
private val videoExt = setOf(
    "mp4", "mov", "mkv", "avi", "webm", "m4v", "mpg", "mpeg", "wmv", "flv", "3gp",
)
private val skipDirNames = setOf("Library", "node_modules", ".git", ".Trash", ".npm", ".cache", "Caches", "DerivedData")

@Serializable
data class MediaFile(
    val path: String,
    val kind: String,
    val ext: String,
    val bytes: Long,
)

@Serializable
data class MediaInventory(
    val root: String,
    val files: List<MediaFile> = emptyList(),
)

@Serializable
data class MediaBucket(
    val kind: String = "",
    val ext: String = "",
    val title: String = "",
    val files: Int = 0,
    val bytes: Long = 0,
    @SerialName("size") val size: String = "",
)

@Serializable
data class MediaSummary(
    val root: String = "",
    @SerialName("inventory_path") val inventoryPath: String = "",
    val files: Int = 0,
    val bytes: Long = 0,
    val size: String = "",
    @SerialName("summary_path") val summaryPath: String = "",
    @SerialName("by_kind") val byKind: List<MediaBucket> = emptyList(),
    @SerialName("by_ext") val byExt: List<MediaBucket> = emptyList(),
)

@Serializable
data class MediaSearchResult(
    val root: String,
    @SerialName("inventory_path") val inventoryPath: String,
    val files: Int,
    val bytes: Long,
    val size: String,
    val skipped: List<String>,
)

fun defaultMediaRoot(): Path = Path.of(System.getProperty("user.home"))

fun mediaWorkDir(): Path = Path.of("data", "media").toAbsolutePath().normalize()

fun scanMedia(root: Path): List<MediaFile> {
    if (!Files.isDirectory(root)) {
        throw IllegalArgumentException("Каталог не найден: $root")
    }
    val out = mutableListOf<MediaFile>()
    Files.walkFileTree(
        root,
        object : SimpleFileVisitor<Path>() {
            override fun preVisitDirectory(dir: Path, attrs: BasicFileAttributes): FileVisitResult {
                val name = dir.name
                if (dir != root && name in skipDirNames) {
                    return FileVisitResult.SKIP_SUBTREE
                }
                return FileVisitResult.CONTINUE
            }

            override fun visitFile(file: Path, attrs: BasicFileAttributes): FileVisitResult {
                if (!attrs.isRegularFile) return FileVisitResult.CONTINUE
                val ext = file.name.substringAfterLast('.', "").lowercase(Locale.ROOT)
                val kind = when (ext) {
                    in imageExt -> "image"
                    in videoExt -> "video"
                    else -> return FileVisitResult.CONTINUE
                }
                out.add(MediaFile(file.toAbsolutePath().normalize().toString(), kind, ext, attrs.size()))
                return FileVisitResult.CONTINUE
            }

            override fun visitFileFailed(file: Path, exc: java.io.IOException): FileVisitResult =
                FileVisitResult.CONTINUE
        },
    )
    return out
}

fun writeInventory(root: Path, files: List<MediaFile>): Path {
    val dir = mediaWorkDir()
    Files.createDirectories(dir)
    val path = dir.resolve("inventory.json")
    val payload = MediaInventory(root.toAbsolutePath().normalize().toString(), files)
    Files.writeString(path, mediaJson.encodeToString(payload))
    return path
}

fun readInventory(path: Path): MediaInventory {
    if (!Files.isRegularFile(path)) {
        throw IllegalArgumentException("Нет файла инвентаря: $path")
    }
    return mediaJson.decodeFromString(Files.readString(path))
}

fun summarizeMedia(inventory: MediaInventory, inventoryPath: String): MediaSummary {
    val byKind = linkedMapOf<String, LongArray>()
    val byExt = linkedMapOf<String, Triple<String, Int, Long>>()
    var files = 0
    var bytes = 0L
    for (file in inventory.files) {
        files += 1
        bytes += file.bytes
        val kind = byKind.getOrPut(file.kind) { longArrayOf(0, 0) }
        kind[0] += 1
        kind[1] += file.bytes
        val prev = byExt[file.ext]
        if (prev == null) {
            byExt[file.ext] = Triple(file.kind, 1, file.bytes)
        } else {
            byExt[file.ext] = Triple(prev.first, prev.second + 1, prev.third + file.bytes)
        }
    }
    return MediaSummary(
        root = inventory.root,
        inventoryPath = inventoryPath,
        files = files,
        bytes = bytes,
        size = formatBytes(bytes),
        byKind = byKind.map { (kind, acc) ->
            MediaBucket(
                kind = kind,
                title = kindTitle(kind),
                files = acc[0].toInt(),
                bytes = acc[1],
                size = formatBytes(acc[1]),
            )
        }.sortedByDescending { it.bytes },
        byExt = byExt.map { (ext, acc) ->
            MediaBucket(kind = acc.first, ext = ext, title = ext, files = acc.second, bytes = acc.third, size = formatBytes(acc.third))
        }.sortedByDescending { it.bytes },
    )
}

fun summaryJson(summary: MediaSummary): String = mediaJson.encodeToString(summary)

fun writeSummary(summary: MediaSummary): Pair<Path, MediaSummary> {
    val dir = mediaWorkDir()
    Files.createDirectories(dir)
    val path = dir.resolve("summary.json")
    val stored = summary.copy(summaryPath = path.toAbsolutePath().normalize().toString())
    Files.writeString(path, summaryJson(stored))
    return path to stored
}

fun parseSummary(raw: String): MediaSummary = mediaJson.decodeFromString(raw)

fun kindTitle(kind: String) = when (kind) {
    "image" -> "картинки"
    "video" -> "видео"
    else -> kind
}

fun formatBytes(n: Long): String {
    val units = arrayOf("Б", "КБ", "МБ", "ГБ", "ТБ")
    var value = n.toDouble()
    var i = 0
    while (value >= 1024 && i < units.lastIndex) {
        value /= 1024
        i += 1
    }
    return if (i == 0) {
        "$n ${units[0]}"
    } else {
        String.format(Locale("ru"), "%.1f %s", value, units[i])
    }
}

fun skippedDirNames(): List<String> = skipDirNames.sorted()
