package com.axior.guardian.core.detection

import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.ModelSpec

/**
 * Composes the full YOLOv8 post-processing chain into domain [Detection]s:
 * decode -> confidence threshold (in the decoder) -> NMS -> map boxes back to
 * normalised upright-frame coordinates. Kept Android-free and unit-tested.
 */
class YoloV8PostProcessor(
    private val spec: ModelSpec,
    private val config: DetectionConfig,
) {
    fun process(
        raw: FloatArray,
        numChannels: Int,
        numAnchors: Int,
        transform: LetterboxTransform,
        frameTimestampMs: Long,
    ): List<Detection> {
        val decoded = YoloV8Decoder.decode(raw, numChannels, numAnchors, spec, config)
        val kept = Nms.suppress(decoded, config.iouThreshold, config.maxDetections)
        if (kept.isEmpty()) return emptyList()

        val out = ArrayList<Detection>(kept.size)
        for (d in kept) {
            val type = spec.detectorForClassIndex(d.classIndex) ?: continue
            out.add(
                Detection(
                    type = type,
                    confidence = d.score,
                    boundingBox = transform.boxToNormalized(d.box),
                    frameTimestampMs = frameTimestampMs,
                ),
            )
        }
        return out
    }
}
