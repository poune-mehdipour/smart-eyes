package com.axior.guardian.core.model

/** A point in normalised [0f, 1f] frame coordinates. */
data class NormalizedPoint(val x: Float, val y: Float)

/**
 * A user-defined polygon within the camera view. Intrusion detection only
 * reports subjects whose position falls inside an active zone.
 */
data class MonitoringZone(
    val id: String,
    val name: String,
    val polygon: List<NormalizedPoint>,
    val enabled: Boolean = true,
)
