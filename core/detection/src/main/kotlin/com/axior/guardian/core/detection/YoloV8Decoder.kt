package com.axior.guardian.core.detection

import com.axior.guardian.core.model.ModelSpec

/** Raised when a model's output tensor does not match its declared spec. */
class DecodeException(message: String) : IllegalArgumentException(message)

/**
 * Decodes the Ultralytics YOLOv8 detect head.
 *
 * Output tensor is [1, 4 + C, N] (channels-first, transposed vs YOLOv5): there
 * is NO objectness channel — the C class scores are already sigmoid-activated
 * and carry confidence directly. The first four channels are cx, cy, w, h.
 *
 * Verified against Ultralytics' documented format (see MODEL_INTEGRATION_RECORD.md).
 * Coordinates are converted to model-input *pixel* space here; mapping back to
 * the frame happens later in [LetterboxTransform].
 */
object YoloV8Decoder {

    /**
     * @param raw flattened output, length must be [numChannels] * [numAnchors].
     * @param numChannels 4 + classCount.
     * @param numAnchors number of candidate boxes (e.g. 8400 for 640 input).
     */
    fun decode(
        raw: FloatArray,
        numChannels: Int,
        numAnchors: Int,
        spec: ModelSpec,
        config: DetectionConfig,
    ): List<RawDetection> {
        val expectedChannels = 4 + spec.classCount
        if (numChannels != expectedChannels) {
            throw DecodeException(
                "Output channel count $numChannels != expected $expectedChannels " +
                    "(4 box + ${spec.classCount} classes). Check the model spec.",
            )
        }
        if (raw.size < numChannels.toLong() * numAnchors) {
            throw DecodeException(
                "Output buffer ${raw.size} too small for [$numChannels x $numAnchors].",
            )
        }

        val scaleX = if (spec.coordinatesNormalized) spec.inputWidth.toFloat() else 1f
        val scaleY = if (spec.coordinatesNormalized) spec.inputHeight.toFloat() else 1f

        val out = ArrayList<RawDetection>()
        for (i in 0 until numAnchors) {
            // argmax over the class channels
            var bestClass = -1
            var bestScore = 0f
            for (k in 0 until spec.classCount) {
                val s = raw[(4 + k) * numAnchors + i]
                if (s > bestScore) {
                    bestScore = s
                    bestClass = k
                }
            }
            if (bestClass < 0) continue

            val type = spec.detectorForClassIndex(bestClass)
            if (bestScore < config.confidenceFor(type)) continue

            val cx = raw[i] * scaleX
            val cy = raw[numAnchors + i] * scaleY
            val w = raw[2 * numAnchors + i] * scaleX
            val h = raw[3 * numAnchors + i] * scaleY

            out.add(
                RawDetection(
                    classIndex = bestClass,
                    score = bestScore,
                    box = PixelBox(
                        left = cx - w / 2f,
                        top = cy - h / 2f,
                        right = cx + w / 2f,
                        bottom = cy + h / 2f,
                    ),
                ),
            )
        }
        return out
    }
}
