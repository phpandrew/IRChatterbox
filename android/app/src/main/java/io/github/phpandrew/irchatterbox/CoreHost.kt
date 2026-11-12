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

    // Calls run in order on one thread. Sending has a lane of its own (still in order, so
    // messages go out in the order they were typed), so nothing else stuck in the queue can
    // stop you from sending. A few calls wait on the network and get their own threads.
    private val ordered = Executors.newSingleThreadExecutor()
    private val sends = Executors.newSingleThreadExecutor()
    private val slow = Executors.newCachedThreadPool()
    private val sendMethods = setOf("SendInput", "SendReply", "React", "Redact")
    // A call still running after this long is noted in app.log with what the app is doing,
    // so a hang can be tracked down (Settings → About → Share app log).
    private val watchdog = Executors.newSingleThreadScheduledExecutor()
    private const val STUCK_AFTER_S = 15L
    // Anything that waits on the network (or a slow disk search) runs alongside, so a link
    // preview or a picture that takes ten seconds doesn't hold up sending a message.
    private val slowMethods = setOf(
        "TestURLCommand", "CheckForUpdate", "InstallUpdate", "ReloadPlugins", "TestMusic", "UploadImage", "UploadPaste",
        "GetLinkPreview", "GetThumbnail", "SendReport", "DeleteUpload", "SearchHistory", "HistoryAround",
        "SyncNow", "SyncPoll", "SyncJoin", "SyncStart", "SyncStop", "SyncSignInStart", "SyncSignInFinish",
    )

    @Volatile var foreground = false
        private set
    @Volatile private var connected = 0

    fun init(context: Context) {
        app = context.applicationContext
    }

    /** Runs an App method from the UI; [done] gets the JSON reply on the main thread. */
    fun call(method: String, argsJson: String, done: (String) -> Unit) {
        val pool = when (method) {
            in slowMethods -> slow
            in sendMethods -> sends
            else -> ordered
        }
        pool.execute {
            val check = if (pool === slow) null else watchdog.scheduleWithFixedDelay({
                Log.w(TAG, "$method still running")
                slow.execute { runCatching { Mobile.reportStuck(method, STUCK_AFTER_S) } }
            }, STUCK_AFTER_S, 60, java.util.concurrent.TimeUnit.SECONDS)
            val reply = try {
                Mobile.call(method, argsJson)
            } catch (t: Throwable) {
                Log.e(TAG, "call $method", t)
                """{"error":${org.json.JSONObject.quote(t.message ?: "failed")}}"""
            } finally {
                check?.cancel(false)
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
