plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.hilt)
    alias(libs.plugins.ksp)
}

android {
    namespace = "com.axior.guardian"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.axior.guardian"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    // Two product flavors express the offline guarantee in the build system:
    //
    //  * offline   — exactly the app as before this milestone: NO INTERNET
    //                permission (structurally incapable of networking) and a
    //                no-op cloud reporter. The default for development.
    //  * connected — adds the INTERNET permission (src/connected/AndroidManifest.xml)
    //                and binds the real cloud reporter. Cloud endpoint/token
    //                come from Gradle properties, never from source:
    //                  ./gradlew :app:assembleConnectedDebug \
    //                    -Pguardian.cloud.baseUrl=http://10.0.2.2:8080 \
    //                    -Pguardian.cloud.apiToken=dev-edge-token
    //
    // In BOTH flavors detection, confirmation and local alerting never wait
    // on the network; the connected flavor only *additionally* reports.
    flavorDimensions += "connectivity"
    productFlavors {
        create("offline") {
            dimension = "connectivity"
            buildConfigField("boolean", "CLOUD_ENABLED", "false")
            buildConfigField("String", "CLOUD_BASE_URL", "\"\"")
            buildConfigField("String", "CLOUD_API_TOKEN", "\"\"")
        }
        create("connected") {
            dimension = "connectivity"
            val baseUrl = (project.findProperty("guardian.cloud.baseUrl") as String?) ?: ""
            val apiToken = (project.findProperty("guardian.cloud.apiToken") as String?) ?: ""
            buildConfigField("boolean", "CLOUD_ENABLED", if (baseUrl.isEmpty()) "false" else "true")
            buildConfigField("String", "CLOUD_BASE_URL", "\"$baseUrl\"")
            buildConfigField("String", "CLOUD_API_TOKEN", "\"$apiToken\"")
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
        }
    }

    buildFeatures {
        compose = true
        buildConfig = true
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
    implementation(project(":core:model"))
    implementation(project(":core:detection"))
    implementation(project(":core:camera"))
    implementation(project(":core:alerts"))
    implementation(project(":core:cloud"))
    implementation(project(":detector:firesmoke"))
    implementation(project(":feature:monitoring"))

    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.activity.compose)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    debugImplementation(libs.androidx.compose.ui.tooling)

    implementation(libs.hilt.android)
    ksp(libs.hilt.compiler)
    implementation(libs.androidx.hilt.navigation.compose)
}
