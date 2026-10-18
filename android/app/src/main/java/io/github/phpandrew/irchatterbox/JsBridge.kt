package io.github.phpandrew.irchatterbox

import android.content.Context
import android.net.Uri
import android.os.VibrationEffect
import android.os.Vibrator
import android.util.Log
import android.webkit.WebView
import androidx.webkit.JavaScriptReplyProxy
import androidx.webkit.WebMessageCompat
import androidx.webkit.WebViewCompat
import org.json.JSONArray
import org.json.JSONObject

/**
 * window.IcbNative: messages from bridge.js. Registered as a WebMessageListener for the
 * app's own origin, and messages from anything but the main frame are dropped, so
 * embedded players can't reach the Go core. Runs on the main thread.
 */
class JsBridge(private val activity: MainActivity) : WebViewCompat.WebMessageListener {

    override fun onPostMessage(
        view: WebView, message: WebMessageCompat, sourceOrigin: Uri,
        isMainFrame: Boolean, replyProxy: JavaScriptReplyProxy,
    ) {
        if (!isMainFrame || sourceOrigin.scheme != "https" || sourceOrigin.host != MainActivity.HOST) return
        val msg = try { JSONObject(message.data ?: return) } catch (_: Exception) { return }
        when (msg.optString("t")) {
            "call" -> call(msg.optInt("id"), msg.optString("m"), msg.optString("a", "[]"))
            "openUrl" -> CoreHost.openURL(msg.optString("url"))
            "ready" -> activity.uiReady()
            "bars" -> activity.setSystemBars(msg.optString("color"), msg.optBoolean("light"))
            "battery" -> activity.requestUnrestricted()
            "notifications" -> activity.requestNotifications()
            "notificationSettings" -> activity.openNotificationSettings()
            "vibrate" -> vibrate(msg.optInt("ms", 10))
            "saveBackup" -> activity.saveBackupFile(msg.optString("name", "irchatterbox-settings.json"), msg.optString("data"))
            "openBackup" -> activity.openBackupFile()
        }
    }

    /** window.go.core.App.<method>(…) — the reply goes back through MainActivity.resolve. */
    private fun call(id: Int, method: String, argsJson: String) {
        if (method.isEmpty()) return
        when (method) {
            // the user is looking at this buffer now
            "SetActive" -> runCatching {
                val a = JSONArray(argsJson)
                Notifications.clear(activity, a.optString(0), a.optString(1))
            }
        }
        CoreHost.call(method, argsJson) { reply -> activity.resolve(id, reply) }
    }

    private fun vibrate(ms: Int) {
        val v = activity.getSystemService(Context.VIBRATOR_SERVICE) as? Vibrator ?: return
        try {
            v.vibrate(VibrationEffect.createOneShot(ms.coerceIn(1, 200).toLong(), VibrationEffect.DEFAULT_AMPLITUDE))
        } catch (e: Exception) {
            Log.w(CoreHost.TAG, "vibrate", e)
        }
    }
}
