package com.axior.guardian.di

import com.axior.guardian.core.cloud.CloudReporter
import com.axior.guardian.core.cloud.NoOpCloudReporter
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

/**
 * `offline` flavor: the cloud does not exist. The reporter is a no-op, the
 * manifest has no INTERNET permission, and the rest of the app is identical
 * to the connected build — which is exactly the point of the seam.
 */
@Module
@InstallIn(SingletonComponent::class)
object CloudModule {

    @Provides
    @Singleton
    fun provideCloudReporter(): CloudReporter = NoOpCloudReporter
}
