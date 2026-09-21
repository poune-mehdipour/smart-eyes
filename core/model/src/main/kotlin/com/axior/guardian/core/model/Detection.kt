package com.axior.guardian.core.model

/**
 * A single raw observation from a detector for one frame.
 *
 * A [Detection] is NOT an alert. Raw detections are noisy by nature; only the
 * event-confirmation stage may promote a sustained pattern of detections into a
 * [SecurityEvent].
 */
data class Detection(
    val type: DetectorType,
    /** Model score in [0f, 1f]. Not assumed to be a calibrated probability. */
    val confidence: Float,
    val boundingBox: BoundingBox?,
    val frameTimestampMs: Long,
)
