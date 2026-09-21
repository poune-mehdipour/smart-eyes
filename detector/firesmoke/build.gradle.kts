plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

// The one real detector in the MVP: an offline TensorFlow Lite fire/smoke model
// wired to the shared pipeline. Isolated in its own module so adding another
// detector later means adding a sibling module, not editing this one.
android {
    namespace = "com.axior.guardian.detector.firesmoke"
    compileSdk = 35

    defaultConfig {
        minSdk = 26
        consumerProguardFiles("consumer-rules.pro")
    }

    // The .tflite asset must NOT be compressed, so it can be memory-mapped
    // directly from the APK for zero-copy loading.
    androidResources {
        noCompress += "tflite"
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

kotlin {
    jvmToolchain(17)
}

dependencies {
    api(project(":core:model"))
    api(project(":core:detection"))

    implementation(libs.tensorflow.lite)
    implementation(libs.tensorflow.lite.gpu)

    implementation(libs.kotlinx.coroutines.core)
}
