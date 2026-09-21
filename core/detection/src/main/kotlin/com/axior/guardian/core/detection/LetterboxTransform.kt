package com.axior.guardian.core.detection

import com.axior.guardian.core.model.BoundingBox

/**
 * Describes the letterbox (aspect-preserving resize + centred padding) that was
 * applied to fit an upright frame into the square model input, and knows how to
 * map a decoded box in model-input pixel space back to normalised [0,1]
 * coordinates of the *upright* frame.
 *
 * Getting this right is what makes bounding boxes line up with the object on the
 * live preview; it is unit-tested for exactly that reason.
 */
data class LetterboxTransform(
    val inputWidth: Int,
    val inputHeight: Int,
    /** Uniform scale applied to the upright frame before padding. */
    val scale: Float,
    /** Left/top padding added to centre the scaled frame in the input square. */
    val padX: Float,
    val padY: Float,
    /** Upright frame dimensions (post-rotation, pre-scale). */
    val uprightWidth: Int,
    val uprightHeight: Int,
) {
    /** Map a point from model-input pixels to normalised upright-frame coords. */
    fun pointToNormalized(x: Float, y: Float): Pair<Float, Float> {
        val ux = ((x - padX) / scale) / uprightWidth
        val uy = ((y - padY) / scale) / uprightHeight
        return ux.coerceIn(0f, 1f) to uy.coerceIn(0f, 1f)
    }

    /** Map a decoded pixel-space box to a normalised, clipped [BoundingBox]. */
    fun boxToNormalized(box: PixelBox): BoundingBox {
        val (l, t) = pointToNormalized(box.left, box.top)
        val (r, b) = pointToNormalized(box.right, box.bottom)
        return BoundingBox(
            left = minOf(l, r),
            top = minOf(t, b),
            right = maxOf(l, r),
            bottom = maxOf(t, b),
        )
    }

    companion object {
        /** Compute the letterbox for fitting [uprightW]x[uprightH] into [inputW]x[inputH]. */
        fun compute(uprightW: Int, uprightH: Int, inputW: Int, inputH: Int): LetterboxTransform {
            val scale = minOf(inputW.toFloat() / uprightW, inputH.toFloat() / uprightH)
            val newW = uprightW * scale
            val newH = uprightH * scale
            return LetterboxTransform(
                inputWidth = inputW,
                inputHeight = inputH,
                scale = scale,
                padX = (inputW - newW) / 2f,
                padY = (inputH - newH) / 2f,
                uprightWidth = uprightW,
                uprightHeight = uprightH,
            )
        }
    }
}
