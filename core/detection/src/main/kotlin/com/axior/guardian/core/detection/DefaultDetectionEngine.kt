package com.axior.guardian.core.detection

import com.axior.guardian.core.model.AnalysisFrame
import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.DetectorType
import com.axior.guardian.core.model.ModelMetadata
import com.axior.guardian.core.model.ModelStatus
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import java.util.concurrent.ConcurrentHashMap

/**
 * Default engine. Runs every enabled detector sequentially on the calling
 * (background) coroutine and concatenates their results, filtering each
 * detector's output to the types currently enabled.
 *
 * Sequential is intentional for the phone MVP: on-device inference is already
 * memory- and thermally-bound, so running interpreters in parallel tends to
 * hurt. The parallel fan-out belongs to the server tier, behind this interface.
 */
class DefaultDetectionEngine(
    private val detectors: Set<Detector>,
) : DetectionEngine {

    private val enabled: MutableSet<DetectorType> =
        ConcurrentHashMap.newKeySet<DetectorType>().apply {
            detectors.forEach { addAll(it.types) }
        }

    override val enabledTypes: Set<DetectorType> get() = enabled.toSet()

    override val availableTypes: Set<DetectorType> =
        detectors.flatMap { it.types }.toSet()

    override val modelStatus: StateFlow<ModelStatus> =
        detectors.firstNotNullOfOrNull { it.modelStatus }
            ?: MutableStateFlow(ModelStatus.Ready(NO_MODEL_METADATA))

    override suspend fun initialize() {
        detectors.forEach { it.initialize() }
    }

    override fun setEnabled(type: DetectorType, enabled: Boolean) {
        if (type !in availableTypes) return
        if (enabled) this.enabled.add(type) else this.enabled.remove(type)
    }

    override suspend fun analyze(frame: AnalysisFrame): List<Detection> {
        if (enabled.isEmpty()) return emptyList()
        val out = ArrayList<Detection>()
        for (detector in detectors) {
            if (detector.types.none { it in enabled }) continue
            val detections = detector.detect(frame)
            for (d in detections) if (d.type in enabled) out.add(d)
        }
        return out
    }

    override fun close() {
        detectors.forEach { runCatching { it.close() } }
    }

    private companion object {
        val NO_MODEL_METADATA = ModelMetadata(
            name = "(no model-backed detector)",
            version = "-",
            license = "-",
            commercialUse = com.axior.guardian.core.model.CommercialUse.APPROVED,
            classNames = emptyList(),
            inputShape = emptyList(),
            outputShape = emptyList(),
            loadTimeMs = 0L,
            delegate = com.axior.guardian.core.model.Delegate.CPU,
        )
    }
}
