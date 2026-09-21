package com.axior.guardian.core.cloud

import com.axior.guardian.core.model.InferenceMetrics
import com.axior.guardian.core.model.SecurityEvent

/**
 * What the monitoring pipeline sees of the cloud. Both calls are fire-and-
 * forget and MUST be non-blocking and non-throwing: the safety invariant of
 * the whole integration is that local detection and local alerting never
 * wait on, and can never be broken by, the network.
 */
interface CloudReporter {
    /** Queue one confirmed hazard event for delivery. Returns immediately. */
    fun reportEvent(event: SecurityEvent)

    /** Queue one telemetry snapshot for delivery. Returns immediately. */
    fun reportTelemetry(monitoring: Boolean, modelStatus: String, metrics: InferenceMetrics)

    /** Release background resources. */
    fun close() {}
}

/**
 * The `offline` flavor's reporter (and the safe default): does nothing at
 * all. Its existence is what lets the rest of the app depend on
 * [CloudReporter] unconditionally while the offline build stays offline by
 * construction — no INTERNET permission, no network code path.
 */
object NoOpCloudReporter : CloudReporter {
    override fun reportEvent(event: SecurityEvent) = Unit
    override fun reportTelemetry(monitoring: Boolean, modelStatus: String, metrics: InferenceMetrics) = Unit
}
