package io.github.phpandrew.irchatterbox

import android.content.Context
import android.os.PowerManager
import android.os.VibrationEffect
import android.os.Vibrator
import android.webkit.JavascriptInterface
import org.json.JSONArray

/** window.IcbNative: what bridge.js calls. Runs on the WebView's binder thread. */
class JsBridge(private val activity: MainActivity) {

    /** window.go.core.App.<method>(…) — the reply goes back through MainActivity.resolve. */
    @JavascriptInterface
    fun call(id: Int, method: String, argsJson: String) {
        when (method) {
            // the user is looking at this buffer now
            "SetActive" -> runCatching {
                val a = JSONArray(argsJson)
                Notifications.clear(activity, a.optString(0), a.optString(1))
            }
        }
        CoreHost.call(method, argsJson) { reply -> activity.resolve(id, reply) }
    }

    @JavascriptInterface
    fun openUrl(url: String) = CoreHost.openURL(url)

    @JavascriptInterface
    fun setSystemBars(color: String, light: Boolean) {
        activity.runOnUiThread { activity.setSystemBars(color, light) }
    }

    @JavascriptInterface
    fun batteryUnrestricted(): Boolean =
        activity.getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(activity.packageName)

    @JavascriptInterface
    fun requestUnrestricted() = activity.runOnUiThread { activity.requestUnrestricted() }

    @JavascriptInterface
    fun notificationsAllowed(): Boolean = Notifications.allowed(activity)

    @JavascriptInterface
    fun requestNotifications() = activity.runOnUiThread { activity.requestNotifications() }

    @JavascriptInterface
    fun openNotificationSettings() = activity.runOnUiThread { activity.openNotificationSettings() }

    @JavascriptInterface
    fun vibrate(ms: Int) {
        val v = activity.getSystemService(Context.VIBRATOR_SERVICE) as? Vibrator ?: return
        val d = ms.coerceIn(1, 200).toLong()
        v.vibrate(VibrationEffect.createOneShot(d, VibrationEffect.DEFAULT_AMPLITUDE))
    }
}
