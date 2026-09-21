package com.axior.guardian.core.detection

/**
 * An axis-aligned box in *model-input pixel* coordinates, used only inside the
 * decode/NMS stage. Kept separate from the domain [com.axior.guardian.core.model.BoundingBox]
 * (which is normalised [0,1]) so the two coordinate spaces never get confused.
 */
data class PixelBox(val left: Float, val top: Float, val right: Float, val bottom: Float) {
    val width: Float get() = (right - left).coerceAtLeast(0f)
    val height: Float get() = (bottom - top).coerceAtLeast(0f)
    val area: Float get() = width * height
}

/** A single decoded box before it is mapped back to frame coordinates. */
data class RawDetection(
    val classIndex: Int,
    val score: Float,
    val box: PixelBox,
)
