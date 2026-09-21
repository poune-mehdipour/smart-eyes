package com.axior.guardian.di

import android.content.Context
import com.axior.guardian.core.detection.ConfirmationPolicy
import com.axior.guardian.core.detection.DefaultDetectionEngine
import com.axior.guardian.core.detection.DetectionConfig
import com.axior.guardian.core.detection.DetectionEngine
import com.axior.guardian.core.detection.Detector
import com.axior.guardian.core.detection.EventConfirmationEngine
import com.axior.guardian.core.detection.SlidingWindowEventConfirmationEngine
import com.axior.guardian.detector.firesmoke.FireSmokeDetector
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import dagger.multibindings.IntoSet
import dagger.multibindings.Multibinds
import javax.inject.Singleton

/**
 * Wires the detection pipeline.
 *
 * The detector set is a Hilt multibinding: each detector contributes itself via
 * `@IntoSet` with no change to the engine, event pipeline or UI. The MVP
 * contributes exactly one real detector — [FireSmokeDetector]. Adding a second
 * hazard later means adding one `@IntoSet` provider here (and its module).
 */
@Module
@InstallIn(SingletonComponent::class)
abstract class DetectorBindingsModule {
    @Multibinds
    abstract fun detectors(): Set<@JvmSuppressWildcards Detector>
}

@Module
@InstallIn(SingletonComponent::class)
object DetectionModule {

    @Provides
    @Singleton
    fun provideDetectionConfig(): DetectionConfig = DetectionConfig()

    /** The one real detector in the MVP. Loads its TFLite model from app assets. */
    @Provides
    @Singleton
    @IntoSet
    fun provideFireSmokeDetector(
        @ApplicationContext context: Context,
        config: DetectionConfig,
    ): Detector = FireSmokeDetector(context, config)

    @Provides
    @Singleton
    fun provideDetectionEngine(
        detectors: Set<@JvmSuppressWildcards Detector>,
    ): DetectionEngine = DefaultDetectionEngine(detectors)

    @Provides
    @Singleton
    fun provideEventConfirmationEngine(): EventConfirmationEngine =
        SlidingWindowEventConfirmationEngine(ConfirmationPolicy.defaults())
}
