package com.axior.guardian.di

import android.content.Context
import com.axior.guardian.core.camera.CameraFrameSource
import com.axior.guardian.core.camera.CameraXFrameSource
import com.axior.guardian.core.camera.FrameSourceConfig
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

/**
 * Binds the phone camera as the frame source. Swapping to an RTSP source later
 * means changing only this module — nothing downstream is aware of CameraX.
 */
@Module
@InstallIn(SingletonComponent::class)
object CameraModule {

    @Provides
    @Singleton
    fun provideFrameSourceConfig(): FrameSourceConfig = FrameSourceConfig()

    @Provides
    @Singleton
    fun provideCameraFrameSource(
        @ApplicationContext context: Context,
        config: FrameSourceConfig,
    ): CameraFrameSource = CameraXFrameSource(context, config)
}
