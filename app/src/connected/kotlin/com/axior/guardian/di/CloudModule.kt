package com.axior.guardian.di

import android.content.Context
import com.axior.guardian.BuildConfig
import com.axior.guardian.core.cloud.CloudConfig
import com.axior.guardian.core.cloud.CloudReporter
import com.axior.guardian.core.cloud.HttpCloudGateway
import com.axior.guardian.core.cloud.NoOpCloudReporter
import com.axior.guardian.core.cloud.QueueingCloudReporter
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import java.util.UUID
import javax.inject.Singleton

/**
 * `connected` flavor: binds the real queueing reporter. Endpoint and token
 * come from BuildConfig (injected via Gradle properties at build time —
 * see app/build.gradle.kts); if they were not provided, the reporter
 * degrades to the same no-op as the offline flavor.
 */
@Module
@InstallIn(SingletonComponent::class)
object CloudModule {

    @Provides
    @Singleton
    fun provideCloudConfig(@ApplicationContext context: Context): CloudConfig {
        // Install-scoped random device id: stable across launches, carries no
        // personal or hardware identity, resets with app data.
        val prefs = context.getSharedPreferences("guardian_cloud", Context.MODE_PRIVATE)
        val deviceId = prefs.getString("device_id", null) ?: UUID.randomUUID().toString().also {
            prefs.edit().putString("device_id", it).apply()
        }
        return CloudConfig(
            enabled = BuildConfig.CLOUD_ENABLED,
            baseUrl = BuildConfig.CLOUD_BASE_URL,
            apiToken = BuildConfig.CLOUD_API_TOKEN,
            deviceId = deviceId,
            appVersion = BuildConfig.VERSION_NAME,
        )
    }

    @Provides
    @Singleton
    fun provideCloudReporter(config: CloudConfig): CloudReporter =
        if (config.enabled) {
            QueueingCloudReporter(config, HttpCloudGateway(config))
        } else {
            NoOpCloudReporter
        }
}
