package io.github.phpandrew.irchatterbox

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.util.Log
import io.github.phpandrew.irchatterbox.core.mobile.Host
import io.github.phpandrew.irchatterbox.core.mobile.Mobile
import java.util.concurrent.Executors

/**
 * The Go core's view of Android (mobile.Host): UI events, notifications, links and the
 * connection count that drives the keep-alive service. Also runs UI calls into the core.
 */
object CoreHost : Host {
    private lateinit var app: Context
    private val main = Handler(Looper.getMainLooper())

    // Calls run in order on one thread, so messages are sent in the order they were typed.
    // A few calls wait on the network and get their own threads.
    private val ordered = Executors.newSingleThreadExecutor()
    private val slow = Executors.newCachedThreadPool()
    private val slowMethods = setOf("TestURLCommand", "CheckForUpdate", "InstallUpdate", "ReloadPlugins", "TestMusic", "UploadImage")

    @Volatile var foreground = false
        private set
    @Volatile private var connected = 0

    fun init(context: Context) {
        app = context.applicationContext
    }

    /** Runs an App method from the UI; [done] gets the JSON reply on the main thread. */
    fun call(method: String, argsJson: String, done: (String) -> Unit) {
        val pool = if (method in slowMethods) slow else ordered
        pool.execute {
            val reply = try {
                Mobile.call(method, argsJson)
            } catch (t: Throwable) {
                Log.e(TAG, "call $method", t)
                """{"error":${org.json.JSONObject.quote(t.message ?: "failed")}}"""
            }
            main.post { done(reply) }
            when (method) {
                "SaveSettings" -> main.post { updateService() }
            }
        }
    }

    fun setForeground(fg: Boolean) {
        foreground = fg
        ordered.execute { Mobile.setBackground(!fg) }
        if (fg) Notifications.clearAll(app)
        updateService()
    }

    // ---- mobile.Host ----

    override fun emit(event: String, dataJSON: String) {
        // In the background the notification makes the sound; the page playing its own
        // chime as well would be heard twice.
        if (event == "playSound" && !foreground) return
        MainActivity.current?.queueEvent(event, dataJSON)
    }

    override fun showNotification(title: String, body: String, server: String, buffer: String, silent: Boolean) {
        // the UI's own sound only plays while the app is on screen
        main.post { Notifications.message(app, title, body, server, buffer, silent && foreground) }
    }

    override fun openURL(url: String) {
        val uri = Uri.parse(url)
        if (uri.scheme != "http" && uri.scheme != "https") return
        main.post {
            try {
                app.startActivity(Intent(Intent.ACTION_VIEW, uri).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            } catch (e: Exception) {
                Log.w(TAG, "no app for $url", e)
            }
        }
    }

    override fun installAPK(path: String): String = Updater.install(app, path)

    override fun connected(n: Long) {
        connected = n.toInt()
        main.post { updateService() }
    }

    /** Starts or stops the keep-alive service to match the setting and connection count. */
    fun updateService() {
        val n = connected
        val keep = try { Mobile.keepAlive() } catch (_: Throwable) { true }
        if (n > 0 && keep) KeepAliveService.start(app, n) else KeepAliveService.stop(app)
    }

    const val TAG = "IRChatterbox"
}
