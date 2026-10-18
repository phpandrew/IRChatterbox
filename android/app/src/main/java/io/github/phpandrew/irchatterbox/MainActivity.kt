package io.github.phpandrew.irchatterbox

import android.Manifest
import android.annotation.SuppressLint
import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.view.View
import android.view.ViewGroup
import android.webkit.ConsoleMessage
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import androidx.activity.ComponentActivity
import androidx.activity.OnBackPressedCallback
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.core.view.updatePadding
import androidx.webkit.WebViewAssetLoader
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import org.json.JSONObject
import java.io.IOException
import java.net.URLConnection

/**
 * Hosts the shared IRChatterbox UI (source/frontend) in a WebView. bridge.js gives it the
 * same window.go / window.runtime objects Wails provides on Windows.
 */
class MainActivity : ComponentActivity() {
    private lateinit var web: WebView
    private lateinit var root: FrameLayout
    private val main = Handler(Looper.getMainLooper())
    private var pageReady = false
    private var uiIsReady = false // main.js finished booting and listens for events
    private val pendingEvents = StringBuilder()
    private var flushScheduled = false
    private var pendingFocus: Pair<String, String>? = null
    private var pendingShare: String? = null
    private var fullscreenView: View? = null

    private val askNotifications = registerForActivityResult(ActivityResultContracts.RequestPermission()) {}

    // The message box's attach button (<input type="file">): "Choose from gallery" opens the
    // system photo picker, "Take photo" (an input with capture) opens the phone's camera app.
    // Either way the image goes back to the page, which uploads it. Neither needs a permission.
    private var fileCallback: ValueCallback<Array<Uri>>? = null
    private var cameraUri: Uri? = null
    private val pickImage = registerForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        answerFileChooser(if (uri != null) arrayOf(uri) else null)
    }
    private val pickImages = registerForActivityResult(ActivityResultContracts.PickMultipleVisualMedia(10)) { uris ->
        answerFileChooser(uris.takeIf { it.isNotEmpty() }?.toTypedArray())
    }
    private val takePhoto = registerForActivityResult(ActivityResultContracts.TakePicture()) { saved ->
        answerFileChooser(cameraUri?.takeIf { saved }?.let { arrayOf(it) })
    }
    private var pendingImages: List<SharedImages.Item> = emptyList()

    // Settings backup (About → Back up settings): Android's "save as" and "open" screens.
    // The result goes back to the page as an androidBackup event.
    private var pendingBackup: String? = null
    private val saveBackup = registerForActivityResult(ActivityResultContracts.CreateDocument("application/json")) { uri ->
        val data = pendingBackup
        pendingBackup = null
        if (uri == null || data == null) return@registerForActivityResult backupResult("saved", canceled = true)
        Thread {
            try {
                contentResolver.openOutputStream(uri, "wt")!!.use { it.write(data.toByteArray()) }
                main.post { backupResult("saved") }
            } catch (e: Exception) {
                Log.w(CoreHost.TAG, "couldn't save backup", e)
                main.post { backupResult("saved", error = "Couldn't save the file.") }
            }
        }.start()
    }
    private val openBackup = registerForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri == null) return@registerForActivityResult backupResult("opened", canceled = true)
        Thread {
            try {
                val bytes = contentResolver.openInputStream(uri)!!.use { input ->
                    // read at most MAX_BACKUP + 1 bytes (readNBytes needs Android 13)
                    val out = java.io.ByteArrayOutputStream()
                    val buf = ByteArray(16 * 1024)
                    while (out.size() <= MAX_BACKUP) {
                        val n = input.read(buf)
                        if (n < 0) break
                        out.write(buf, 0, n)
                    }
                    out.toByteArray()
                }
                main.post {
                    if (bytes.size > MAX_BACKUP) backupResult("opened", error = "That file is too big to be an IRChatterbox backup.")
                    else backupResult("opened", data = String(bytes))
                }
            } catch (e: Exception) {
                Log.w(CoreHost.TAG, "couldn't open backup", e)
                main.post { backupResult("opened", error = "Couldn't read the file.") }
            }
        }.start()
    }

    fun saveBackupFile(name: String, data: String) {
        pendingBackup = data
        try {
            saveBackup.launch(name)
        } catch (e: Exception) {
            pendingBackup = null
            backupResult("saved", error = "No app to save files with.")
        }
    }

    fun openBackupFile() {
        try {
            openBackup.launch(arrayOf("application/json", "text/plain", "application/octet-stream"))
        } catch (e: Exception) {
            backupResult("opened", error = "No app to open files with.")
        }
    }

    private fun backupResult(kind: String, data: String? = null, error: String? = null, canceled: Boolean = false) {
        val o = JSONObject().put("kind", kind).put("canceled", canceled)
        if (data != null) o.put("data", data)
        if (error != null) o.put("error", error)
        queueEvent("androidBackup", o.toString())
    }

    private fun answerFileChooser(uris: Array<Uri>?) {
        fileCallback?.onReceiveValue(uris)
        fileCallback = null
    }

    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        current = this

        root = FrameLayout(this)
        web = WebView(this)
        root.addView(web, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        setContentView(root)

        // keep the UI clear of the status bar, navigation bar, display cutout and keyboard
        ViewCompat.setOnApplyWindowInsetsListener(root) { v, insets ->
            val bars = insets.getInsets(
                WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout() or WindowInsetsCompat.Type.ime()
            )
            v.updatePadding(left = bars.left, top = bars.top, right = bars.right, bottom = bars.bottom)
            WindowInsetsCompat.CONSUMED
        }

        val assets = WebViewAssetLoader.Builder()
            .addPathHandler("/", WebPathHandler())
            .build()

        web.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            mediaPlaybackRequiresUserGesture = false
            allowFileAccess = false
            allowContentAccess = false
            setSupportMultipleWindows(false)
            textZoom = 100 // the app has its own font size setting
            cacheMode = WebSettings.LOAD_DEFAULT
        }
        web.isVerticalScrollBarEnabled = false
        web.overScrollMode = View.OVER_SCROLL_NEVER
        // The bridge and its message channel are limited to the app's own origin; embedded
        // players in other frames get neither. Both need a WebView from 2020 or later.
        if (!WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT) ||
            !WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)
        ) {
            web.loadData(
                "<p style='font:16px sans-serif;padding:24px'>IRChatterbox needs a newer Android System WebView. " +
                    "Update it from the Play Store, then reopen the app.</p>", "text/html", "utf-8"
            )
            return
        }
        val origin = setOf("https://$HOST")
        WebViewCompat.addWebMessageListener(web, "IcbNative", origin, JsBridge(this))
        WebViewCompat.addDocumentStartJavaScript(web, assets("bridge.js"), origin)

        web.webViewClient = object : WebViewClient() {
            override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? =
                assets.shouldInterceptRequest(request.url)

            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                if (request.url.host == HOST) return false
                CoreHost.openURL(request.url.toString())
                return true
            }

            override fun onPageFinished(view: WebView, url: String?) {
                pageReady = true
                flushEvents()
            }

            // The WebView's renderer was killed (low memory while in the background) or crashed.
            // Android's default is to kill the whole app, which would drop every connection; only
            // the page needs rebuilding — the Go core lives on in the process.
            override fun onRenderProcessGone(view: WebView, detail: android.webkit.RenderProcessGoneDetail): Boolean {
                Log.w(CoreHost.TAG, "WebView renderer gone (crashed: ${detail.didCrash()}); rebuilding the page")
                pageReady = false
                uiIsReady = false
                root.removeView(view)
                view.destroy()
                if (!isFinishing) recreate()
                return true
            }
        }
        web.webChromeClient = object : WebChromeClient() {
            override fun onConsoleMessage(m: ConsoleMessage): Boolean {
                Log.d(CoreHost.TAG, "js: ${m.message()} (${m.sourceId()}:${m.lineNumber()})")
                return true
            }

            override fun onShowFileChooser(
                view: WebView,
                callback: ValueCallback<Array<Uri>>,
                params: FileChooserParams,
            ): Boolean {
                fileCallback?.onReceiveValue(null) // a picker that never answered: let the page move on
                fileCallback = callback
                if (params.isCaptureEnabled) {
                    try {
                        val uri = SharedImages.newCameraFile(this@MainActivity)
                        cameraUri = uri
                        takePhoto.launch(uri)
                        return true
                    } catch (e: Exception) {
                        // no camera app (or it can't be opened): fall back to the gallery
                        Log.w(CoreHost.TAG, "couldn't open the camera", e)
                    }
                }
                return try {
                    val images = PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)
                    if (params.mode == FileChooserParams.MODE_OPEN_MULTIPLE) pickImages.launch(images) else pickImage.launch(images)
                    true
                } catch (e: Exception) {
                    Log.w(CoreHost.TAG, "couldn't open the image picker", e)
                    fileCallback = null
                    false
                }
            }

            // full-screen video from YouTube / Vimeo embeds
            override fun onShowCustomView(view: View, callback: CustomViewCallback) {
                fullscreenView = view
                root.addView(view, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
                WindowCompat.getInsetsController(window, root).apply {
                    hide(WindowInsetsCompat.Type.systemBars())
                    systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
                }
            }

            override fun onHideCustomView() {
                fullscreenView?.let { root.removeView(it) }
                fullscreenView = null
                WindowCompat.getInsetsController(window, root).show(WindowInsetsCompat.Type.systemBars())
            }
        }

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (fullscreenView != null) {
                    web.webChromeClient?.onHideCustomView()
                    return
                }
                web.evaluateJavascript("window.__icb && window.__icb.back()") { handled ->
                    if (handled != "true") moveTaskToBack(true)
                }
            }
        })

        readIntent(intent)
        web.loadUrl("https://$HOST/index.html")

        if (Build.VERSION.SDK_INT >= 33 && !Notifications.allowed(this)) {
            askNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        readIntent(intent)
        if (uiIsReady) deliverIntentExtras()
    }

    override fun onResume() {
        super.onResume()
        pushStatus() // back from Android's battery or notification settings
    }

    override fun onDestroy() {
        if (current === this) current = null
        runCatching { web.destroy() }
        super.onDestroy()
    }

    /** main.js has registered its event listeners: deliver what arrived with the launch. */
    fun uiReady() {
        uiIsReady = true
        pushStatus()
        deliverIntentExtras()
    }

    /** Battery and notification state for the settings page (window.icbAndroid). */
    private fun pushStatus() {
        if (!pageReady || isDestroyed) return
        val battery = getSystemService(android.os.PowerManager::class.java).isIgnoringBatteryOptimizations(packageName)
        val s = JSONObject().put("battery", battery).put("notifications", Notifications.allowed(this))
        web.evaluateJavascript("window.__icb&&window.__icb.status($s)", null)
    }

    // ---- intents: notification taps and "Share to IRChatterbox" ----

    private fun readIntent(i: Intent?) {
        i ?: return
        val server = i.getStringExtra(Notifications.EXTRA_SERVER)
        val buffer = i.getStringExtra(Notifications.EXTRA_BUFFER)
        if (server != null && buffer != null) {
            pendingFocus = server to buffer
            Notifications.clear(this, server, buffer)
        }
        if (i.action == Intent.ACTION_SEND || i.action == Intent.ACTION_SEND_MULTIPLE) {
            if (i.type?.startsWith("image/") == true) {
                // photos shared from the gallery or another app: copied in the background, then
                // handed to the page (which uploads them) once it's ready
                val uris = SharedImages.streams(i)
                Thread {
                    val items = SharedImages.copyIn(this, uris)
                    runOnUiThread {
                        pendingImages = pendingImages + items
                        if (uiIsReady) deliverIntentExtras()
                    }
                }.start()
            } else {
                pendingShare = i.getStringExtra(Intent.EXTRA_TEXT)
            }
        }
        // a notification reply that couldn't be sent: put it back in the message box
        i.getStringExtra(EXTRA_DRAFT)?.let { pendingShare = it }
    }

    private fun deliverIntentExtras() {
        pendingFocus?.let { (server, buffer) ->
            pendingFocus = null
            queueEvent("focusBuffer", JSONObject().put("server", server).put("buffer", buffer).toString())
        }
        pendingShare?.let {
            pendingShare = null
            queueEvent("shareText", JSONObject().put("text", it).toString())
        }
        if (pendingImages.isNotEmpty()) {
            val files = org.json.JSONArray()
            pendingImages.forEach {
                files.put(JSONObject().put("url", "https://$HOST/${SharedImages.PATH}/${it.name}").put("type", it.type).put("name", it.name))
            }
            pendingImages = emptyList()
            queueEvent("shareImages", JSONObject().put("files", files).toString())
        }
    }

    // ---- backend → UI events, batched once per frame ----

    fun queueEvent(event: String, dataJson: String) {
        synchronized(pendingEvents) {
            if (pendingEvents.isNotEmpty()) pendingEvents.append(',')
            pendingEvents.append('[').append(JSONObject.quote(event)).append(',').append(dataJson).append(']')
            if (flushScheduled) return
            flushScheduled = true
        }
        main.postDelayed({ flushEvents() }, 16)
    }

    private fun flushEvents() {
        if (!pageReady || isDestroyed) return
        val batch = synchronized(pendingEvents) {
            flushScheduled = false
            if (pendingEvents.isEmpty()) return
            val s = pendingEvents.toString()
            pendingEvents.setLength(0)
            s
        }
        web.evaluateJavascript("window.__icb&&window.__icb.emit([$batch])", null)
    }

    fun resolve(id: Int, replyJson: String) {
        if (isDestroyed) return
        web.evaluateJavascript("window.__icb&&window.__icb.resolve($id,$replyJson)", null)
    }

    /** Status/navigation bar colour follows the current theme. */
    fun setSystemBars(color: String, light: Boolean) {
        val c = try { Color.parseColor(color) } catch (_: IllegalArgumentException) { return }
        root.setBackgroundColor(c)
        window.decorView.setBackgroundColor(c)
        WindowCompat.getInsetsController(window, root).apply {
            isAppearanceLightStatusBars = light
            isAppearanceLightNavigationBars = light
        }
    }

    private fun assets(name: String) = assets.open(name).bufferedReader().use { it.readText() }

    /** Serves the built frontend from assets/web/ at https://appassets.androidplatform.net/. */
    private inner class WebPathHandler : WebViewAssetLoader.PathHandler {
        override fun handle(path: String): WebResourceResponse? {
            val clean = path.trimStart('/').ifEmpty { "index.html" }
            if (clean.contains("..")) return null
            if (clean.startsWith(SharedImages.PATH + "/")) return SharedImages.serve(this@MainActivity, clean.substringAfter('/'))
            return try {
                val stream = assets.open("web/$clean")
                val mime = when {
                    clean.endsWith(".js") || clean.endsWith(".mjs") -> "text/javascript"
                    clean.endsWith(".css") -> "text/css"
                    clean.endsWith(".html") -> "text/html"
                    clean.endsWith(".svg") -> "image/svg+xml"
                    clean.endsWith(".wav") -> "audio/wav"
                    clean.endsWith(".woff2") -> "font/woff2"
                    clean.endsWith(".json") -> "application/json"
                    else -> URLConnection.guessContentTypeFromName(clean) ?: "application/octet-stream"
                }
                WebResourceResponse(mime, if (mime.startsWith("text/") || mime.endsWith("json")) "utf-8" else null, stream)
            } catch (_: IOException) {
                null
            }
        }
    }

    fun openNotificationSettings() {
        startActivity(
            Intent(android.provider.Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                .putExtra(android.provider.Settings.EXTRA_APP_PACKAGE, packageName)
        )
    }

    fun requestNotifications() {
        if (Build.VERSION.SDK_INT >= 33) askNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        else openNotificationSettings()
    }

    @SuppressLint("BatteryLife")
    fun requestUnrestricted() {
        if (BuildConfig.STORE == "play") {
            // Play build: no direct prompt. The app's settings page has Battery → Unrestricted.
            try {
                startActivity(Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$packageName")))
            } catch (_: Exception) {
                startActivity(Intent(android.provider.Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
            }
            return
        }
        try {
            startActivity(
                Intent(android.provider.Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName"))
            )
        } catch (_: Exception) {
            startActivity(Intent(android.provider.Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
        }
    }

    companion object {
        const val HOST = "appassets.androidplatform.net"
        private const val MAX_BACKUP = 4 shl 20
        const val EXTRA_DRAFT = "draft"
        @Volatile var current: MainActivity? = null
            private set
    }
}
