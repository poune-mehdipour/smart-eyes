plugins {
    alias(libs.plugins.kotlin.jvm)
}

// The AI engine contracts and the frame -> event pipeline. Pure Kotlin +
// coroutines, no Android: the same engine runs on the phone and, later, server
// side against RTSP frames.
java {
    sourceCompatibility = JavaVersion.VERSION_17
    targetCompatibility = JavaVersion.VERSION_17
}

kotlin {
    jvmToolchain(17)
}

dependencies {
    api(project(":core:model"))
    api(libs.kotlinx.coroutines.core)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
}
