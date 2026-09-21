package com.axior.guardian.core.model

/**
 * A snapshot of live pipeline health. Every value here is measured on-device;
 * none is a target or a claim. Displayed on the monitoring screen.
 */
data class InferenceMetrics(
    val modelLoadMs: Long = 0L,
    val lastInferenceMs: Long = 0L,
    val avgInferenceMs: Double = 0.0,
    val processedFps: Int = 0,
    val droppedFrames: Long = 0L,
    val totalDetections: Long = 0L,
    val confirmedEvents: Long = 0L,
)
