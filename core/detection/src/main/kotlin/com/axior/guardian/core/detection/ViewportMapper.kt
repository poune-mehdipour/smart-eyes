package com.axior.guardian.core.detection

import com.axior.guardian.core.model.BoundingBox

/** A rectangle in view (pixel) coordinates. */
data class ViewRect(val left: Float, val top: Float, val right: Float, val bottom: Float) {
    val width: Float get() = (right - left).coerceAtLeast(0f)
    val height: Float get() = (bottom - top).coerceAtLeast(0f)
}

/**
 * Maps a normalised [BoundingBox] (in upright-frame [0,1] space) onto the
 * on-screen preview, reproducing CameraX `PreviewView` **FILL_CENTER**: the frame
 * is uniformly scaled to *cover* the view, then centre-cropped.
 *
 * Isolated as pure math (no Android) precisely so the box-alignment logic — the
 * thing most likely to be subtly wrong — is unit-tested. The overlay composable
 * is then a thin renderer over this.
 */
object ViewportMapper {

    /**
     * @param frameAspectRatio upright frame width / height (e.g. 480/640 for a
     *   portrait-upright 3:4 frame).
     * @param mirror true for a front-facing preview (horizontal flip).
     */
    fun mapBox(
        box: BoundingBox,
        frameAspectRatio: Float,
        viewWidth: Float,
        viewHeight: Float,
        mirror: Boolean = false,
    ): ViewRect {
        if (viewWidth <= 0f || viewHeight <= 0f || frameAspectRatio <= 0f) {
            return ViewRect(0f, 0f, 0f, 0f)
        }
        // Treat the frame as (frameAspectRatio x 1) units; FILL_CENTER covers the view.
        val fW = frameAspectRatio
        val fH = 1f
        val scale = maxOf(viewWidth / fW, viewHeight / fH)
        val scaledW = fW * scale
        val scaledH = fH * scale
        val offsetX = (scaledW - viewWidth) / 2f
        val offsetY = (scaledH - viewHeight) / 2f

        var left = box.left * scaledW - offsetX
        var right = box.right * scaledW - offsetX
        val top = box.top * scaledH - offsetY
        val bottom = box.bottom * scaledH - offsetY

        if (mirror) {
            val ml = viewWidth - right
            val mr = viewWidth - left
            left = ml
            right = mr
        }

        return ViewRect(
            left = left.coerceIn(0f, viewWidth),
            top = top.coerceIn(0f, viewHeight),
            right = right.coerceIn(0f, viewWidth),
            bottom = bottom.coerceIn(0f, viewHeight),
        )
    }
}
