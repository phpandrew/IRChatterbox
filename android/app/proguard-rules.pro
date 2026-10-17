# gomobile: the Go side calls these classes through JNI by name.
-keep class go.** { *; }
-keep class io.github.phpandrew.irchatterbox.core.** { *; }

# Methods the WebView calls by name.
-keepclassmembers class * {
    @android.webkit.JavascriptInterface <methods>;
}
