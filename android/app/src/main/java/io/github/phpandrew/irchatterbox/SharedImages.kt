package io.github.phpandrew.irchatterbox

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.util.Log
import android.webkit.WebResourceResponse
import androidx.core.content.FileProvider
import java.io.File
import java.util.UUID

/**
 * Photo files the app hands to the page for uploading:
 *  - camera shots: the camera app writes into cache/camera/ through our FileProvider
 *  - photos shared from other apps: copied into cache/shared/ and served to the page at
 *    https://appassets.androidplatform.net/shared/<name> (the page fetches and uploads them)
 * Files older than an hour are removed when the app starts.
 */
object SharedImages {
    const val PATH = "shared"
    private const val MAX_BYTES = 40L * 1024 * 1024 // the page shrinks photos before uploading
    private val NAME = Regex("^[0-9a-f-]{36}\\.(jpg|png|webp|bmp|gif|heic)$")

    data class Item(val name: String, val type: String)

    fun clean(ctx: Context) {
        // not everything: a photo being shared right as the app starts must survive
        val cutoff = System.currentTimeMillis() - 60 * 60 * 1000
        for (dir in listOf("camera", PATH))
            File(ctx.cacheDir, dir).listFiles()?.filter { it.lastModified() < cutoff }?.forEach { it.delete() }
    }

    fun newCameraFile(ctx: Context): Uri {
        val dir = File(ctx.cacheDir, "camera").apply { mkdirs() }
        val f = File(dir, "photo-${System.currentTimeMillis()}.jpg")
        return FileProvider.getUriForFile(ctx, ctx.packageName + ".files", f)
    }

    @Suppress("DEPRECATION")
    fun streams(i: Intent): List<Uri> = when (i.action) {
        Intent.ACTION_SEND -> listOfNotNull(
            if (Build.VERSION.SDK_INT >= 33) i.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
            else i.getParcelableExtra(Intent.EXTRA_STREAM)
        )
        Intent.ACTION_SEND_MULTIPLE ->
            (if (Build.VERSION.SDK_INT >= 33) i.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java)
            else i.getParcelableArrayListExtra(Intent.EXTRA_STREAM)).orEmpty()
        else -> emptyList()
    }.take(10)

    /** Copies shared images into the app's cache. Runs off the main thread. */
    fun copyIn(ctx: Context, uris: List<Uri>): List<Item> {
        val dir = File(ctx.cacheDir, PATH).apply { mkdirs() }
        return uris.mapNotNull { uri ->
            try {
                val type = ctx.contentResolver.getType(uri) ?: "image/jpeg"
                val ext = when (type) {
                    "image/png" -> "png"; "image/webp" -> "webp"; "image/bmp" -> "bmp"
                    "image/gif" -> "gif"; "image/heic", "image/heif" -> "heic"; else -> "jpg"
                }
                if (!type.startsWith("image/")) return@mapNotNull null
                if (ext == "heic" && Build.VERSION.SDK_INT >= 28) return@mapNotNull heicToJpeg(ctx, uri, dir)
                val name = "${UUID.randomUUID()}.$ext"
                val out = File(dir, name)
                ctx.contentResolver.openInputStream(uri)?.use { input ->
                    out.outputStream().use { o ->
                        val buf = ByteArray(64 * 1024)
                        var total = 0L
                        while (true) {
                            val n = input.read(buf)
                            if (n < 0) break
                            total += n
                            if (total > MAX_BYTES) throw IllegalStateException("too large")
                            o.write(buf, 0, n)
                        }
                    }
                } ?: return@mapNotNull null
                Item(name, type)
            } catch (e: Exception) {
                Log.w(CoreHost.TAG, "couldn't read shared image", e)
                null
            }
        }
    }

    /** HEIC/HEIF (some Samsung cameras) isn't something the page can read: decode it here. */
    private fun heicToJpeg(ctx: Context, uri: Uri, dir: File): Item? {
        if (Build.VERSION.SDK_INT < 28) return null
        val src = android.graphics.ImageDecoder.createSource(ctx.contentResolver, uri)
        val bmp = android.graphics.ImageDecoder.decodeBitmap(src) { d, info, _ ->
            // the page shrinks to 2048 px anyway; decoding smaller saves memory
            val edge = maxOf(info.size.width, info.size.height)
            if (edge > 2048) d.setTargetSampleSize(edge / 2048)
            d.allocator = android.graphics.ImageDecoder.ALLOCATOR_SOFTWARE
        }
        val name = "${UUID.randomUUID()}.jpg"
        File(dir, name).outputStream().use { bmp.compress(android.graphics.Bitmap.CompressFormat.JPEG, 88, it) }
        bmp.recycle()
        return Item(name, "image/jpeg")
    }

    fun serve(ctx: Context, name: String): WebResourceResponse? {
        if (!NAME.matches(name)) return null
        val f = File(File(ctx.cacheDir, PATH), name)
        if (!f.isFile) return null
        val type = when (f.extension) {
            "png" -> "image/png"; "webp" -> "image/webp"; "bmp" -> "image/bmp"
            "gif" -> "image/gif"; "heic" -> "image/heic"; else -> "image/jpeg"
        }
        return WebResourceResponse(type, null, f.inputStream())
    }
}
