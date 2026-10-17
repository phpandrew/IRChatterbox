package io.github.phpandrew.irchatterbox

import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/**
 * "Keep connection live in background": a foreground service that keeps the process (and
 * the Go core's connections) alive while the app isn't on screen. It does no work itself.
 */
class KeepAliveService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val n = intent?.getIntExtra(EXTRA_COUNT, lastCount.coerceAtLeast(1)) ?: lastCount.coerceAtLeast(1)
        val type = if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0
        try {
            ServiceCompat.startForeground(this, NOTIFICATION_ID, notification(this, n), type)
            running = true
            lastCount = n
        } catch (e: Exception) {
            Log.w(CoreHost.TAG, "couldn't start keep-alive", e)
            running = false
            stopSelf()
            return START_NOT_STICKY
        }
        if (intent == null) {
            // restarted by Android after it stopped the process: reconnect what was open
            CoreHost.call("AutoConnect", "[]") {}
        }
        return START_STICKY
    }

    override fun onDestroy() {
        // stopped by us, by the Disconnect button, or by Android: either way it's gone
        running = false
        lastCount = 0
        super.onDestroy()
    }

    companion object {
        private const val NOTIFICATION_ID = 2
        private const val EXTRA_COUNT = "count"
        @Volatile private var running = false
        @Volatile private var lastCount = 0

        fun start(ctx: Context, n: Int) {
            if (running) {
                // already in the foreground: only the text changes. Updating the notification
                // is allowed from the background, unlike starting the service again.
                if (n != lastCount) {
                    lastCount = n
                    try {
                        NotificationManagerCompat.from(ctx).notify(NOTIFICATION_ID, notification(ctx, n))
                    } catch (_: SecurityException) {
                    }
                }
                return
            }
            try {
                ContextCompat.startForegroundService(ctx, Intent(ctx, KeepAliveService::class.java).putExtra(EXTRA_COUNT, n))
            } catch (e: Exception) {
                // Android won't start a foreground service from the background; it starts
                // next time the app is opened.
                Log.w(CoreHost.TAG, "keep-alive not started", e)
            }
        }

        fun stop(ctx: Context) {
            ctx.stopService(Intent(ctx, KeepAliveService::class.java))
        }

        private fun notification(ctx: Context, n: Int) = NotificationCompat.Builder(ctx, Notifications.CH_SERVICE)
        .setSmallIcon(R.drawable.ic_stat_chat)
        .setColor(ContextCompat.getColor(ctx, R.color.brand))
        .setContentTitle(ctx.getString(R.string.service_title))
        .setContentText(if (n == 1) ctx.getString(R.string.service_text_one) else ctx.getString(R.string.service_text_many, n))
        .setOngoing(true)
        .setSilent(true)
        .setShowWhen(false)
        .setPriority(NotificationCompat.PRIORITY_MIN)
        .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
        .setContentIntent(
            PendingIntent.getActivity(
                ctx, 0, Intent(ctx, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
                PendingIntent.FLAG_IMMUTABLE
            )
        )
        .addAction(
            0, ctx.getString(R.string.quit),
            PendingIntent.getBroadcast(
                ctx, 1, Intent(ctx, ActionReceiver::class.java).setAction(ActionReceiver.ACTION_QUIT),
                PendingIntent.FLAG_IMMUTABLE
            )
        )
        .build()
    }
}
