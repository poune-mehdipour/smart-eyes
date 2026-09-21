package com.axior.guardian.core.detection

import com.axior.guardian.core.model.AnalysisFrame
import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.DetectorType
import com.axior.guardian.core.model.ModelMetadata
import com.axior.guardian.core.model.ModelStatus
import kotlinx.coroutines.flow.StateFlow

/**
 * A single, self-contained detection capability. Implementations live in their
 * own modules and are contributed to the engine via dependency injection.
 *
 * A detector reports only what it sees in [detect]; deciding whether that
 * constitutes an event is the confirmation pipeline's job, not the detector's.
 * This is the model-independent seam the whole product hangs off — a TFLite
 * detector, a classical-CV detector, or a future server-side one all satisfy it.
 */
interface Detector {
    /** Primary hazard type; also the default single member of [types]. */
    val type: DetectorType

    /**
     * All hazard types this detector can emit. A combined fire+smoke model
     * declares both here so one detector serves multiple classes without the
     * engine, pipeline or UI changing.
     */
    val types: Set<DetectorType> get() = setOf(type)

    /**
     * Load native resources (interpreter, weights, buffers). Called once before
     * the first [detect]. Must not throw for a *missing* model — surface that
     * through [modelStatus] instead, so the app degrades to "detection disabled"
     * rather than crashing.
     */
    suspend fun initialize() {}

    /**
     * Analyse a single frame. Called on a background dispatcher. Implementations
     * should be fast and allocation-light; the caller guarantees frames are
     * already throttled. A model-backed detector with no model loaded returns an
     * empty list — never a fabricated detection.
     */
    suspend fun detect(frame: AnalysisFrame): List<Detection>

    /** Runtime model facts once loaded, or null for a non-model detector. */
    fun metadata(): ModelMetadata? = null

    /** Live model lifecycle for the UI, or null if this detector has no model. */
    val modelStatus: StateFlow<ModelStatus>? get() = null

    /** Release native resources. */
    fun close() {}
}
