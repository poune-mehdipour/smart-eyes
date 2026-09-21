package com.axior.guardian.core.model

/**
 * A confirmed hazard, produced by the event-confirmation stage after a pattern
 * of raw [Detection]s has satisfied that hazard's confirmation policy. This is
 * the unit the alert pipeline acts on and the unit persisted to history.
 */
data class SecurityEvent(
    val id: String,
    val type: DetectorType,
    /** Representative confidence at the moment of confirmation. */
    val confidence: Float,
    val firstSeenMs: Long,
    val confirmedAtMs: Long,
    val boundingBox: BoundingBox?,
)
