import java.util.Properties

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// source/VERSION is shared with the Windows app. CI passes the full release version
// (-PappVersion=0.4.2); local builds are "<VERSION>-dev".
val baseVersion = rootProject.file("../source/VERSION").readText().trim()
val appVersion = (findProperty("appVersion") as String?)?.takeIf { it.isNotBlank() } ?: "$baseVersion-dev"

// 0.4.2 → 40020: major·1,000,000 + minor·10,000 + patch·10
fun versionCodeOf(v: String): Int {
    val nums = v.substringBefore('-').substringBefore('+').split('.').map { it.toIntOrNull() ?: 0 }
    val (maj, min, pat) = (nums + listOf(0, 0, 0)).take(3)
    return maj * 1_000_000 + min * 10_000 + pat * 10
}

// Release signing comes from keystore.properties (local) or environment variables (CI).
// Without either, release builds are signed with the debug key so they still install.
val signing = Properties().apply {
    rootProject.file("keystore.properties").takeIf { it.exists() }?.inputStream()?.use { load(it) }
}
fun signingValue(key: String, env: String): String? =
    signing.getProperty(key) ?: System.getenv(env)?.takeIf { it.isNotBlank() }

android {
    namespace = "io.github.phpandrew.irchatterbox"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.github.phpandrew.irchatterbox"
        minSdk = 26
        targetSdk = 36
        versionCode = versionCodeOf(appVersion)
        versionName = appVersion
    }

    val storeFile = signingValue("storeFile", "ANDROID_KEYSTORE_FILE")
    signingConfigs {
        if (storeFile != null) {
            create("release") {
                this.storeFile = rootProject.file(storeFile)
                storePassword = signingValue("storePassword", "ANDROID_KEYSTORE_PASSWORD")
                keyAlias = signingValue("keyAlias", "ANDROID_KEY_ALIAS")
                keyPassword = signingValue("keyPassword", "ANDROID_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
        }
        debug {
            applicationIdSuffix = ".debug"
            versionNameSuffix = "-debug"
        }
    }

    // github: the APK on GitHub Releases, with the built-in update check.
    // play:   the Google Play bundle (.aab). Play delivers updates, so the update check is off,
    //         and the battery-optimisation prompt is replaced by a link to the app's settings
    //         (REQUEST_IGNORE_BATTERY_OPTIMIZATIONS is restricted on Play).
    // Same package name; the two installs can't update each other (different signing keys).
    flavorDimensions += "store"
    productFlavors {
        create("github") {
            dimension = "store"
            buildConfigField("String", "STORE", "\"\"")
        }
        create("play") {
            dimension = "store"
            buildConfigField("String", "STORE", "\"play\"")
        }
    }

    buildFeatures {
        buildConfig = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    sourceSets["main"].assets.srcDir(layout.buildDirectory.dir("generated/web"))

    packaging {
        // compressed in the APK: a much smaller download for the Go library
        jniLibs.useLegacyPackaging = true
    }
}

kotlin {
    compilerOptions {
        jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
    }
}

// The UI is the same Vite build as the Windows app (source/frontend/dist), served from
// assets/web/ inside the APK.
val frontendDist = rootProject.file("../source/frontend/dist")
val copyWeb by tasks.registering(Copy::class) {
    from(frontendDist) { exclude("gitkeep", ".gitkeep") }
    into(layout.buildDirectory.dir("generated/web/web"))
    doFirst {
        check(frontendDist.resolve("index.html").exists()) {
            "Build the frontend first: cd source/frontend && npm ci && npm run build"
        }
    }
}
tasks.named("preBuild") { dependsOn(copyWeb) }

dependencies {
    implementation(fileTree("libs") { include("*.aar") })
    implementation("androidx.core:core-ktx:1.17.0")
    implementation("androidx.activity:activity-ktx:1.11.0")
    implementation("androidx.webkit:webkit:1.14.0")
    implementation("androidx.lifecycle:lifecycle-process:2.9.4")
}
