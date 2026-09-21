package com.axior.guardian.core.detection

import com.axior.guardian.core.model.DetectorType

/**
 * How many corroborating frames are needed before a hazard is treated as real,
 * and how long to stay quiet afterwards.
 *
 * This is a debounce, not a forecast: it exists solely to stop a single noisy
 * frame (a reflection, a puff of steam, a flicker) from firing an emergency
 * workflow.
 */
data class ConfirmationPolicy(
    /** Minimum per-frame confidence for a detection to count toward confirmation. */
    val minConfidence: Float,
    /** Corroborating detections required inside [windowMs] to confirm. */
    val requiredHits: Int,
    /** Sliding window over which [requiredHits] must accrue. */
    val windowMs: Long,
    /** After confirming, suppress re-confirmation of the same type for this long. */
    val cooldownMs: Long,
) {
    companion object {
        /**
         * Conservative starting points. These are placeholders for *tuning*, not
         * guesses to ship — every threshold must be validated against real
         * footage before this app is allowed to place a call.
         */
        fun defaults(): Map<DetectorType, ConfirmationPolicy> = mapOf(
            DetectorType.FIRE to ConfirmationPolicy(0.60f, requiredHits = 4, windowMs = 2_000, cooldownMs = 30_000),
            DetectorType.SMOKE to ConfirmationPolicy(0.60f, requiredHits = 6, windowMs = 3_000, cooldownMs = 30_000),
            DetectorType.FALL to ConfirmationPolicy(0.70f, requiredHits = 3, windowMs = 2_500, cooldownMs = 20_000),
            DetectorType.INTRUSION to ConfirmationPolicy(0.55f, requiredHits = 3, windowMs = 1_500, cooldownMs = 15_000),
            DetectorType.CAMERA_TAMPERING to ConfirmationPolicy(0.50f, requiredHits = 5, windowMs = 2_000, cooldownMs = 15_000),
        )
    }
}
