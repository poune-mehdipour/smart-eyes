plugins {
    alias(libs.plugins.kotlin.jvm)
}

// Edge -> cloud reporting. Pure Kotlin + coroutines, NO Android dependency,
// exactly like :core:model and :core:detection: the wire contract, queueing
// and retry logic are unit-tested on the JVM, and the same code would serve
// the future RTSP/server-side frame source. The only I/O primitive used is
// HttpURLConnection, which exists identically on Android and the JVM.
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
