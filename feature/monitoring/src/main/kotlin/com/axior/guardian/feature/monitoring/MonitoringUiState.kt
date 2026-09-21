package com.axior.guardian.feature.monitoring

import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.InferenceMetrics
import com.axior.guardian.core.model.ModelStatus
import com.axior.guardian.core.model.SecurityEvent

enum class MonitoringStatus { Idle, Monitoring }

/**
 * Everything the monitoring screen needs to render, as a single immutable
 * snapshot. The ViewModel is the only writer.
 */
data class MonitoringUiState(
    val status: MonitoringStatus = MonitoringStatus.Idle,
    val hasCameraPermission: Boolean = false,
    /** Live model lifecycle, so the UI shows an honest loading/disabled/error state. */
    val modelStatus: ModelStatus = ModelStatus.Loading,
    /** Measured pipeline health — every field is real, none is a target. */
    val metrics: InferenceMetrics = InferenceMetrics(),
    /** Raw detections for the current frame, drawn as boxes. */
    val activeDetections: List<Detection> = emptyList(),
    /** Upright frame aspect ratio (w/h) for correct overlay mapping. */
    val frameAspectRatio: Float = 0f,
    val lastEvent: SecurityEvent? = null,
    /** Optional audible alarm on confirmed events. */
    val soundEnabled: Boolean = true,
    /** Non-null when something needs the operator's attention. */
    val errorMessage: String? = null,
) {
    val isMonitoring: Boolean get() = status == MonitoringStatus.Monitoring

    /** True only when a real model is loaded and running. */
    val isModelReady: Boolean get() = modelStatus is ModelStatus.Ready

    /** True when the running model is cleared for commercial release. */
    val isCommercialApproved: Boolean
        get() = (modelStatus as? ModelStatus.Ready)?.metadata?.commercialApproved == true
}
