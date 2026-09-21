package com.axior.guardian.core.model

/**
 * Everything the pipeline must know about a specific model file to feed it and
 * decode it *correctly*. These are not guesses: every field must match the
 * actual provisioned model, and is documented in
 * docs/implementation/MODEL_INTEGRATION_RECORD.md.
 *
 * The spec is loaded from a JSON sidecar next to the model asset, so a model can
 * be swapped without touching code (see the firesmoke detector's model loader).
 */
data class ModelSpec(
    val name: String,
    val version: String,
    val source: String,
    val license: String,
    /** Whether this model may ship in a commercial product / redistributed APK. */
    val commercialUse: CommercialUse,
    // --- input ---
    val inputWidth: Int,
    val inputHeight: Int,
    val inputChannels: Int = 3,
    val inputLayout: InputLayout = InputLayout.NHWC,
    val normalization: Normalization = Normalization.SCALE_0_1,
    val inputRgb: Boolean = true,
    /** true => uint8 quantized input (with [inputQuantScale]/[inputQuantZeroPoint]); false => float32. */
    val inputQuantized: Boolean = false,
    val inputQuantScale: Float = 1f,
    val inputQuantZeroPoint: Int = 0,
    // --- output ---
    val outputFormat: OutputFormat = OutputFormat.YOLOV8_DETECT,
    /**
     * Ordered class names as trained. Index in this list is the model's class
     * channel index. Mapped to [DetectorType] via [classToDetector].
     */
    val classNames: List<String>,
    /** Maps each class name (lowercased) to the hazard type it represents. */
    val classToDetector: Map<String, DetectorType>,
    /**
     * true  => decoded box coords are already normalised to [0,1] of the input
     *          (the usual YOLOv8 *TFLite* export behaviour), or
     * false => coords are in input-pixel units and must be divided by input size.
     * MUST be verified against the actual model — see the inspection script.
     */
    val coordinatesNormalized: Boolean = true,
) {
    val classCount: Int get() = classNames.size

    /** Resolve a model class index to a hazard type, or null if unmapped. */
    fun detectorForClassIndex(index: Int): DetectorType? {
        val name = classNames.getOrNull(index)?.lowercase() ?: return null
        return classToDetector[name]
    }
}

enum class InputLayout { NHWC, NCHW }

enum class Normalization {
    /** pixel / 255f -> [0,1] */
    SCALE_0_1,

    /** (pixel / 127.5) - 1 -> [-1,1] */
    SCALE_MINUS1_1,

    /** raw pixel values 0..255 (e.g. for a uint8 quantised model) */
    NONE,
}

enum class OutputFormat {
    /**
     * Ultralytics YOLOv8 detect head: a single tensor [1, 4+C, N] (channels
     * first), no objectness score, class scores already sigmoid-activated,
     * boxes as cx,cy,w,h. Requires NMS.
     */
    YOLOV8_DETECT,

    /**
     * A model whose head already emits one box per object with no NMS needed
     * (e.g. YOLOv10 / YOLO26 / DETR-family). Decoder differs; provided as an
     * extension point, not implemented in the MVP.
     */
    NMS_FREE_DETECT,
}

enum class CommercialUse {
    /** Licence permits commercial use and APK redistribution. */
    APPROVED,

    /** Usable for evaluation only; NOT cleared for a commercial release. */
    EVALUATION_ONLY,

    /** Licence status not yet resolved — treat as evaluation-only. */
    UNRESOLVED,
}
