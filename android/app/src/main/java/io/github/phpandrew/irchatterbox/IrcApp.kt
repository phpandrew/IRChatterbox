package io.github.phpandrew.irchatterbox

import android.app.Application
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner
import androidx.lifecycle.ProcessLifecycleOwner
import io.github.phpandrew.irchatterbox.core.mobile.Mobile
import java.io.File

/**
 * Starts the Go core once per process. Connections live here, not in the Activity, so they
 * survive the Activity being closed while the keep-alive service runs.
 */
class IrcApp : Application() {
    override fun onCreate() {
        super.onCreate()
        Notifications.createChannels(this)
        Thread { SharedImages.clean(this) }.start() // photos left from the last run
        CoreHost.init(this)
        val dataDir = File(filesDir, "data").apply { mkdirs() }
        Mobile.setStore(BuildConfig.STORE)
        Mobile.start(dataDir.absolutePath, BuildConfig.VERSION_NAME, CoreHost)

        ProcessLifecycleOwner.get().lifecycle.addObserver(object : DefaultLifecycleObserver {
            override fun onStart(owner: LifecycleOwner) = CoreHost.setForeground(true)
            override fun onStop(owner: LifecycleOwner) = CoreHost.setForeground(false)
        })
    }
}
