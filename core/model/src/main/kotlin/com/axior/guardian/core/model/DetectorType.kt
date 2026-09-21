package com.axior.guardian.core.model

/**
 * The catalogue of hazards the platform can detect.
 *
 * Each value maps to exactly one [Detector] implementation. Adding a new
 * capability to the product means adding a value here and a detector module —
 * nothing in the engine, event pipeline or UI needs to change.
 */
enum class DetectorType {
    FIRE,
    SMOKE,
    FALL,
    INTRUSION,
    CAMERA_TAMPERING,
}
