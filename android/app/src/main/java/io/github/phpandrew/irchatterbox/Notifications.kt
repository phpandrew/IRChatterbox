package io.github.phpandrew.irchatterbox

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.RemoteInput
import androidx.core.content.ContextCompat

/**
 * Mention and private-message notifications, one per buffer. Lines stack up until you
 * open the buffer; each has a Reply action that sends straight to the channel.
 */
object Notifications {
    const val CH_MENTIONS = "mentions"
    const val CH_MESSAGES = "messages"
    const val CH_SERVICE = "service"
    const val CH_UPDATES = "updates"
    const val EXTRA_SERVER = "server"
    const val EXTRA_BUFFER = "buffer"
    const val KEY_REPLY = "reply"
    private const val GROUP = "irc"
    private const val SUMMARY_ID = 1

    private val lines = mutableMapOf<String, MutableList<CharSequence>>()

    fun createChannels(ctx: Context) {
        val nm = ctx.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannels(
            listOf(
                NotificationChannel(CH_MENTIONS, ctx.getString(R.string.channel_mentions), NotificationManager.IMPORTANCE_HIGH)
                    .apply { description = ctx.getString(R.string.channel_mentions_desc) },
                NotificationChannel(CH_MESSAGES, ctx.getString(R.string.channel_messages), NotificationManager.IMPORTANCE_HIGH)
                    .apply { description = ctx.getString(R.string.channel_messages_desc) },
                NotificationChannel(CH_SERVICE, ctx.getString(R.string.channel_service), NotificationManager.IMPORTANCE_MIN)
                    .apply { description = ctx.getString(R.string.channel_service_desc); setShowBadge(false) },
                NotificationChannel(CH_UPDATES, ctx.getString(R.string.channel_updates), NotificationManager.IMPORTANCE_DEFAULT)
                    .apply { description = ctx.getString(R.string.channel_updates_desc); setShowBadge(false) },
            )
        )
    }

    fun allowed(ctx: Context): Boolean =
        NotificationManagerCompat.from(ctx).areNotificationsEnabled() &&
            (Build.VERSION.SDK_INT < 33 ||
                ContextCompat.checkSelfPermission(ctx, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED)

    private fun key(server: String, buffer: String) = server + "\u0000" + buffer.lowercase()
    private fun idFor(server: String, buffer: String) = key(server, buffer).hashCode() or 0x1000

    private fun isChannel(buffer: String) = buffer.isNotEmpty() && buffer[0] in "#&+!"

    fun openIntent(ctx: Context, server: String, buffer: String, draft: String? = null): PendingIntent {
        val i = Intent(ctx, MainActivity::class.java)
            .setAction("open:$server:$buffer")
            .putExtra(EXTRA_SERVER, server)
            .putExtra(EXTRA_BUFFER, buffer)
            .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP)
        if (draft != null) i.putExtra(MainActivity.EXTRA_DRAFT, draft)
        return PendingIntent.getActivity(ctx, idFor(server, buffer), i, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    fun message(ctx: Context, title: String, body: String, server: String, buffer: String, silent: Boolean) {
        if (!allowed(ctx)) return
        val k = key(server, buffer)
        val list = synchronized(lines) {
            lines.getOrPut(k) { mutableListOf() }.apply {
                add(body)
                while (size > 7) removeAt(0)
            }.toList()
        }
        val style = NotificationCompat.InboxStyle().setBigContentTitle(title)
        list.forEach { style.addLine(it) }
        if (list.size > 1) style.setSummaryText("${list.size} new")

        val n = NotificationCompat.Builder(ctx, if (isChannel(buffer)) CH_MENTIONS else CH_MESSAGES)
            .setSmallIcon(R.drawable.ic_stat_chat)
            .setColor(ContextCompat.getColor(ctx, R.color.brand))
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(style)
            .setNumber(list.size)
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setAutoCancel(true)
            .setSilent(silent)
            .setOnlyAlertOnce(false)
            .setGroup(GROUP)
            .setContentIntent(openIntent(ctx, server, buffer))
            .addAction(replyAction(ctx, server, buffer))
            .build()
        val summary = NotificationCompat.Builder(ctx, CH_MENTIONS)
            .setSmallIcon(R.drawable.ic_stat_chat)
            .setColor(ContextCompat.getColor(ctx, R.color.brand))
            .setGroup(GROUP)
            .setGroupSummary(true)
            .setSilent(true)
            .setAutoCancel(true)
            .build()
        try {
            val nm = NotificationManagerCompat.from(ctx)
            nm.notify(idFor(server, buffer), n)
            nm.notify(SUMMARY_ID, summary)
        } catch (_: SecurityException) {
        }
    }

    private fun replyAction(ctx: Context, server: String, buffer: String): NotificationCompat.Action {
        val input = RemoteInput.Builder(KEY_REPLY).setLabel("Reply to $buffer").build()
        val i = Intent(ctx, ActionReceiver::class.java)
            .setAction(ActionReceiver.ACTION_REPLY)
            .putExtra(EXTRA_SERVER, server)
            .putExtra(EXTRA_BUFFER, buffer)
        val pi = PendingIntent.getBroadcast(ctx, idFor(server, buffer), i, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE)
        return NotificationCompat.Action.Builder(R.drawable.ic_stat_chat, "Reply", pi)
            .addRemoteInput(input)
            .setAllowGeneratedReplies(false)
            .setSemanticAction(NotificationCompat.Action.SEMANTIC_ACTION_REPLY)
            .build()
    }

    /**
     * A reply from the notification couldn't be sent because that server isn't connected.
     * The notification says so, and tapping it opens the buffer with the text ready to send.
     */
    fun replyFailed(ctx: Context, server: String, buffer: String, text: String) {
        val n = NotificationCompat.Builder(ctx, if (isChannel(buffer)) CH_MENTIONS else CH_MESSAGES)
            .setSmallIcon(R.drawable.ic_stat_chat)
            .setColor(ContextCompat.getColor(ctx, R.color.brand))
            .setContentTitle("Not sent to $buffer")
            .setContentText("Not connected. Tap to open; your message is waiting in the box.")
            .setStyle(NotificationCompat.BigTextStyle().bigText("Not connected. Tap to open; your message is waiting in the box:\n$text"))
            .setSilent(true)
            .setAutoCancel(true)
            .setGroup(GROUP)
            .setContentIntent(openIntent(ctx, server, buffer, text))
            .build()
        try {
            NotificationManagerCompat.from(ctx).notify(idFor(server, buffer), n)
        } catch (_: SecurityException) {
        }
    }

    /** The buffer is on screen now: drop its notification. */
    fun clear(ctx: Context, server: String, buffer: String) {
        val had = synchronized(lines) { lines.remove(key(server, buffer)) != null }
        if (!had) return
        val nm = NotificationManagerCompat.from(ctx)
        nm.cancel(idFor(server, buffer))
        if (synchronized(lines) { lines.isEmpty() }) nm.cancel(SUMMARY_ID)
    }

    /** Opening the app clears the stacked lines, like the desktop's taskbar flash. */
    fun clearAll(ctx: Context) {
        val ids = synchronized(lines) {
            val keys = lines.keys.toList()
            lines.clear()
            keys
        }
        if (ids.isEmpty()) return
        val nm = NotificationManagerCompat.from(ctx)
        ids.forEach { nm.cancel(it.hashCode() or 0x1000) }
        nm.cancel(SUMMARY_ID)
    }
}
