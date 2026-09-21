plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
}

// Local alerting for confirmed events: vibration, a notification, and an
// optional tone. Android-only by nature; deliberately tiny and free of any
// network capability.
android {
    namespace = "com.axior.guardian.core.alerts"
    compileSdk = 35

    defaultConfig {
        minSdk = 26
        consumerProguardFiles("consumer-rules.pro")
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

    implementation(libs.androidx.core.ktx)
}
