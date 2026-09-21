package com.axior.guardian.core.detection

import com.axior.guardian.core.model.AnalysisFrame
import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.DetectorType
import com.axior.guardian.core.model.ModelStatus
import kotlinx.coroutines.flow.StateFlow

/**
 * Fans a frame out across the enabled detectors and aggregates their raw
 * detections. Detector membership is fixed at construction; which types are
 * *active* is toggled at runtime.
 */
interface DetectionEngine {
    /** Hazard types currently active. */
    val enabledTypes: Set<DetectorType>

    /** The full set of types the engine could run, active or not. */
    val availableTypes: Set<DetectorType>

    /** Aggregate model lifecycle across model-backed detectors, for the UI. */
    val modelStatus: StateFlow<ModelStatus>

    /** Initialise all detectors (load models). Safe to call once at startup. */
    suspend fun initialize()

    fun setEnabled(type: DetectorType, enabled: Boolean)

    suspend fun analyze(frame: AnalysisFrame): List<Detection>

    fun close()
}
