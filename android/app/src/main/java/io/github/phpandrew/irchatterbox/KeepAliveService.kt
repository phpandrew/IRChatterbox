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
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/**
 * "Keep connection live in background": a foreground service that keeps the process (and
 * the Go core's connections) alive while the app isn't on screen. It does no work itself.
 */
class KeepAliveService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        running = true
        val n = intent?.getIntExtra(EXTRA_COUNT, 1) ?: 1
        val type = if (Build.VERSION.SDK_INT >= 34) ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0
        try {
            ServiceCompat.startForeground(this, NOTIFICATION_ID, notification(n), type)
        } catch (e: Exception) {
            Log.w(CoreHost.TAG, "couldn't start keep-alive", e)
            stopSelf()
        }
        if (intent == null) {
            // restarted by Android after it stopped the process: reconnect what was open
            CoreHost.call("AutoConnect", "[]") {}
        }
        return START_STICKY
    }

    private fun notification(n: Int) = NotificationCompat.Builder(this, Notifications.CH_SERVICE)
        .setSmallIcon(R.drawable.ic_stat_chat)
        .setColor(ContextCompat.getColor(this, R.color.brand))
        .setContentTitle(getString(R.string.service_title))
        .setContentText(if (n == 1) getString(R.string.service_text_one) else getString(R.string.service_text_many, n))
        .setOngoing(true)
        .setSilent(true)
        .setShowWhen(false)
        .setPriority(NotificationCompat.PRIORITY_MIN)
        .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
        .setContentIntent(
            PendingIntent.getActivity(
                this, 0, Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
                PendingIntent.FLAG_IMMUTABLE
            )
        )
        .addAction(
            0, getString(R.string.quit),
            PendingIntent.getBroadcast(
                this, 1, Intent(this, ActionReceiver::class.java).setAction(ActionReceiver.ACTION_QUIT),
                PendingIntent.FLAG_IMMUTABLE
            )
        )
        .build()

    companion object {
        private const val NOTIFICATION_ID = 2
        private const val EXTRA_COUNT = "count"
        @Volatile private var running = false
        @Volatile private var lastCount = 0

        fun start(ctx: Context, n: Int) {
            if (running && lastCount == n) return
            try {
                ContextCompat.startForegroundService(ctx, Intent(ctx, KeepAliveService::class.java).putExtra(EXTRA_COUNT, n))
                running = true
                lastCount = n
            } catch (e: Exception) {
                // Android won't start a foreground service from the background; it starts
                // next time the app is opened.
                Log.w(CoreHost.TAG, "keep-alive not started", e)
            }
        }

        fun stop(ctx: Context) {
            if (!running) return
            running = false
            lastCount = 0
            ctx.stopService(Intent(ctx, KeepAliveService::class.java))
        }
    }
}
