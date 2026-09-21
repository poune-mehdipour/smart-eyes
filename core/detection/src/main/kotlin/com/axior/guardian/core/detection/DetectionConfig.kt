package com.axior.guardian.core.detection

import com.axior.guardian.core.model.DetectorType

/**
 * The single, central place for the tunable numbers that govern *raw* detection
 * (as opposed to [ConfirmationPolicy], which governs event promotion). Kept in
 * one object so there are no unexplained magic constants scattered across the
 * codebase.
 *
 * Every value here is a starting point for tuning against real footage, not a
 * validated production threshold — see docs/research/benchmarking.md.
 */
data class DetectionConfig(
    /** Global minimum class score for a raw box to be kept before NMS. */
    val confidenceThreshold: Float = 0.35f,
    /** IoU above which two same-class boxes are considered duplicates in NMS. */
    val iouThreshold: Float = 0.45f,
    /** Hard cap on detections kept per frame after NMS (newest-strongest first). */
    val maxDetections: Int = 50,
    /** Optional per-type overrides of [confidenceThreshold]. */
    val perTypeConfidence: Map<DetectorType, Float> = emptyMap(),
    /** Rolling window size (samples) for the average-latency readout. */
    val latencyWindow: Int = 30,
) {
    fun confidenceFor(type: DetectorType?): Float =
        type?.let { perTypeConfidence[it] } ?: confidenceThreshold
}
