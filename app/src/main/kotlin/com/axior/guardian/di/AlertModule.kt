package com.axior.guardian.di

import android.content.Context
import com.axior.guardian.core.alerts.AlertController
import com.axior.guardian.core.alerts.AndroidAlertController
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

/**
 * Provides the local alerting capability. Kept in its own module so the alert
 * mechanism can be swapped (e.g. for a headless/service build) without touching
 * detection wiring.
 */
@Module
@InstallIn(SingletonComponent::class)
object AlertModule {

    @Provides
    @Singleton
    fun provideAlertController(
        @ApplicationContext context: Context,
    ): AlertController = AndroidAlertController(context)
}
