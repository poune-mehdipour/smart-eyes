plugins {
    alias(libs.plugins.kotlin.jvm)
}

// Pure-Kotlin domain module. Deliberately has NO Android dependency so it can be
// shared unchanged between the phone app and the future RTSP/CCTV backend.
java {
    sourceCompatibility = JavaVersion.VERSION_17
    targetCompatibility = JavaVersion.VERSION_17
}

kotlin {
    jvmToolchain(17)
}

dependencies {
    testImplementation(libs.junit)
}
