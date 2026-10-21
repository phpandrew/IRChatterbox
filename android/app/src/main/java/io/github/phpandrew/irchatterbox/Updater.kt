package io.github.phpandrew.irchatterbox

import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInstaller
import android.net.Uri
import android.os.Build
import android.provider.Settings
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import org.json.JSONObject
import java.io.File

/**
 * In-app updates (GitHub build): the Go core downloads and checks the new APK, then this
 * hands it to Android's PackageInstaller. Android shows "Do you want to update this app?"
 * and installs it only if it's signed with the same key as the installed app.
 *
 * The first time, Android asks you to allow IRChatterbox to install apps; the update
 * carries on when you come back. Once IRChatterbox has installed itself, Android 12 and
 * later may update it without asking.
 */
object Updater {
    private const val ACTION_STATUS = "io.github.phpandrew.irchatterbox.INSTALL_STATUS"
    @Volatile private var pending: String? = null

    /** Called from the Go core (CoreHost.installAPK). "" = the installer has it. */
    fun install(ctx: Context, path: String): String {
        val file = File(path)
        if (!file.isFile) return "The download is missing."
        if (BuildConfig.STORE == "play") return "Updates come from Google Play."
        if (!ctx.packageManager.canRequestPackageInstalls()) {
            // first update: Android needs "Install unknown apps" for IRChatterbox. Carry on
            // from MainActivity.onResume when the user comes back from that screen.
            pending = path
            val activity = MainActivity.current ?: return "Open IRChatterbox and try again."
            activity.runOnUiThread {
                try {
                    activity.startActivity(
                        Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${ctx.packageName}"))
                    )
                } catch (e: Exception) {
                    Log.w(CoreHost.TAG, "no unknown-sources settings screen", e)
                }
            }
            event("permission", "Allow IRChatterbox to install apps, then come back to finish the update.")
            return ""
        }
        return try {
            commit(ctx, file)
            ""
        } catch (e: Exception) {
            Log.e(CoreHost.TAG, "install failed", e)
            "Couldn't start the installer: ${e.message}"
        }
    }

    /** Back from the "Install unknown apps" screen: install the waiting update if allowed now. */
    fun resume(ctx: Context) {
        val path = pending ?: return
        if (!ctx.packageManager.canRequestPackageInstalls()) {
            pending = null
            event("error", "IRChatterbox wasn't allowed to install apps, so the update was cancelled.")
            return
        }
        pending = null
        Thread {
            val msg = try {
                commit(ctx, File(path)); null
            } catch (e: Exception) {
                Log.e(CoreHost.TAG, "install failed", e); "Couldn't start the installer: ${e.message}"
            }
            if (msg != null) event("error", msg)
        }.start()
    }

    private fun commit(ctx: Context, file: File) {
        val installer = ctx.packageManager.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL).apply {
            setAppPackageName(ctx.packageName)
            setSize(file.length())
            if (Build.VERSION.SDK_INT >= 31) setRequireUserAction(PackageInstaller.SessionParams.USER_ACTION_NOT_REQUIRED)
        }
        val id = installer.createSession(params)
        installer.openSession(id).use { session ->
            file.inputStream().use { input ->
                session.openWrite("IRChatterbox.apk", 0, file.length()).use { out ->
                    input.copyTo(out, 256 * 1024)
                    session.fsync(out)
                }
            }
            val flags = PendingIntent.FLAG_UPDATE_CURRENT or (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0)
            val status = PendingIntent.getBroadcast(
                ctx, id, Intent(ctx, InstallReceiver::class.java).setAction(ACTION_STATUS), flags
            )
            session.commit(status.intentSender)
        }
        event("installing", "Installing… tap Update if Android asks.")
    }

    /** Tells the update bar what happened (main.js: "updateStatus"). */
    fun event(state: String, message: String) {
        MainActivity.current?.queueEvent(
            "updateStatus", JSONObject().put("state", state).put("message", message).toString()
        )
    }

    /** Answers from PackageInstaller: ask the user to confirm, or report a failure. */
    class InstallReceiver : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            when (val status = intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)) {
                PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                    @Suppress("DEPRECATION")
                    val confirm = if (Build.VERSION.SDK_INT >= 33) intent.getParcelableExtra(Intent.EXTRA_INTENT, Intent::class.java)
                    else intent.getParcelableExtra(Intent.EXTRA_INTENT)
                    if (confirm != null) {
                        try {
                            context.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                        } catch (e: Exception) {
                            Log.w(CoreHost.TAG, "couldn't show the install prompt", e)
                            event("error", "Android didn't show the install prompt. Try again.")
                        }
                    }
                }
                PackageInstaller.STATUS_SUCCESS -> Unit // the app is replaced and restarted by Android
                PackageInstaller.STATUS_FAILURE_ABORTED -> event("error", "Update cancelled.")
                else -> {
                    val detail = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE).orEmpty()
                    Log.w(CoreHost.TAG, "install status $status: $detail")
                    event(
                        "error",
                        if (status == PackageInstaller.STATUS_FAILURE_CONFLICT || status == PackageInstaller.STATUS_FAILURE_INCOMPATIBLE)
                            "Android refused the update: it isn't signed with the same key as the installed app. Uninstall IRChatterbox and install the new version."
                        else "The update didn't install${if (detail.isNotEmpty()) ": $detail" else "."}"
                    )
                }
            }
        }
    }

    /** After an update Android doesn't reopen the app, so offer a tap to open it. */
    class ReplacedReceiver : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            if (intent.action != Intent.ACTION_MY_PACKAGE_REPLACED || BuildConfig.STORE == "play") return
            if (!Notifications.allowed(context)) return
            val open = PendingIntent.getActivity(
                context, 3, Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
            )
            val n = NotificationCompat.Builder(context, Notifications.CH_UPDATES)
                .setSmallIcon(R.drawable.ic_stat_chat)
                .setColor(ContextCompat.getColor(context, R.color.brand))
                .setContentTitle(context.getString(R.string.updated_title, BuildConfig.VERSION_NAME))
                .setContentText(context.getString(R.string.updated_text))
                .setContentIntent(open)
                .setAutoCancel(true)
                .build()
            context.getSystemService(NotificationManager::class.java).notify(UPDATED_ID, n)
        }
    }

    const val UPDATED_ID = 3
}
