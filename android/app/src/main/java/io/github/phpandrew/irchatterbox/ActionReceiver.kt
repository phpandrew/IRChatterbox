package io.github.phpandrew.irchatterbox

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.RemoteInput
import io.github.phpandrew.irchatterbox.core.mobile.Mobile
import org.json.JSONArray
import org.json.JSONObject

/** Notification buttons: Reply on a message, Disconnect on the keep-alive notification. */
class ActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        when (intent.action) {
            ACTION_QUIT -> {
                val done = goAsync()
                Thread {
                    try {
                        Mobile.shutdown()
                    } finally {
                        KeepAliveService.stop(context)
                        done.finish()
                    }
                }.start()
            }
            ACTION_REPLY -> {
                val server = intent.getStringExtra(Notifications.EXTRA_SERVER) ?: return
                val buffer = intent.getStringExtra(Notifications.EXTRA_BUFFER) ?: return
                val text = RemoteInput.getResultsFromIntent(intent)
                    ?.getCharSequence(Notifications.KEY_REPLY)?.toString()?.trim().orEmpty()
                if (text.isEmpty()) return
                val done = goAsync()
                // Only send when that server is connected: otherwise the line would be dropped
                // (the app may be disconnected in the background, or just restarted).
                CoreHost.call("GetState", "[]") { state ->
                    if (!isConnected(state, server)) {
                        Notifications.replyFailed(context, server, buffer, text)
                        done.finish()
                        return@call
                    }
                    CoreHost.call("SendInput", JSONArray(listOf(server, buffer, text)).toString()) {
                        Notifications.clear(context, server, buffer)
                        done.finish()
                    }
                }
            }
        }
    }

    private fun isConnected(stateJson: String, server: String): Boolean = try {
        val list = JSONObject(stateJson).optJSONArray("ok")
        (0 until (list?.length() ?: 0)).any {
            val st = list!!.getJSONObject(it)
            st.optString("id") == server && st.optString("status") == "connected"
        }
    } catch (_: Exception) {
        false
    }

    companion object {
        const val ACTION_QUIT = "io.github.phpandrew.irchatterbox.QUIT"
        const val ACTION_REPLY = "io.github.phpandrew.irchatterbox.REPLY"
    }
}
