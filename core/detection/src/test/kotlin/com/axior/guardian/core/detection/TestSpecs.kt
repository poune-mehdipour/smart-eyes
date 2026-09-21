package com.axior.guardian.core.detection

import com.axior.guardian.core.model.CommercialUse
import com.axior.guardian.core.model.DetectorType
import com.axior.guardian.core.model.ModelSpec

object TestSpecs {
    /** 2-class fire/smoke YOLOv8 spec, 640 input, coords normalised to input. */
    fun fireSmoke(coordinatesNormalized: Boolean = false) = ModelSpec(
        name = "test-firesmoke",
        version = "0",
        source = "test",
        license = "test",
        commercialUse = CommercialUse.EVALUATION_ONLY,
        inputWidth = 640,
        inputHeight = 640,
        classNames = listOf("fire", "smoke"),
        classToDetector = mapOf("fire" to DetectorType.FIRE, "smoke" to DetectorType.SMOKE),
        coordinatesNormalized = coordinatesNormalized,
    )
}

/** Build a flat YOLOv8 output [1, 4+C, N] from per-anchor (cx,cy,w,h,scores...). */
fun buildYoloOutput(classCount: Int, anchors: List<FloatArray>): Triple<FloatArray, Int, Int> {
    val channels = 4 + classCount
    val n = anchors.size
    val raw = FloatArray(channels * n)
    anchors.forEachIndexed { i, a ->
        require(a.size == channels) { "anchor $i has ${a.size} values, expected $channels" }
        for (c in 0 until channels) raw[c * n + i] = a[c]
    }
    return Triple(raw, channels, n)
}
