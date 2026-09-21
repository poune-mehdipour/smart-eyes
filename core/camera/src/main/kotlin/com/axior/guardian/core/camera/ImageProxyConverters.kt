package com.axior.guardian.core.camera

import androidx.camera.core.ImageProxy

/**
 * Converts a YUV_420_888 [ImageProxy] to a tightly packed NV21 byte array
 * (full Y plane followed by interleaved V/U), honouring row and pixel strides.
 *
 * NV21 is the lingua franca accepted by the on-device runtimes the detectors
 * will use, which is why the conversion lives at the source rather than being
 * repeated in every detector.
 */
internal fun ImageProxy.toNv21(): ByteArray {
    val width = width
    val height = height
    val out = ByteArray(width * height * 3 / 2)

    val yPlane = planes[0]
    val uPlane = planes[1]
    val vPlane = planes[2]

    val yBuffer = yPlane.buffer
    val uBuffer = uPlane.buffer
    val vBuffer = vPlane.buffer

    var pos = 0

    // --- Luma (Y) ---
    val yRowStride = yPlane.rowStride
    val yPixelStride = yPlane.pixelStride
    for (row in 0 until height) {
        var rowStart = row * yRowStride
        if (yPixelStride == 1) {
            yBuffer.position(rowStart)
            yBuffer.get(out, pos, width)
            pos += width
        } else {
            for (col in 0 until width) {
                out[pos++] = yBuffer.get(rowStart)
                rowStart += yPixelStride
            }
        }
    }

    // --- Chroma (V/U interleaved => NV21) ---
    val uvRowStride = uPlane.rowStride
    val uvPixelStride = uPlane.pixelStride
    val chromaHeight = height / 2
    val chromaWidth = width / 2
    for (row in 0 until chromaHeight) {
        val rowStart = row * uvRowStride
        for (col in 0 until chromaWidth) {
            val uvIndex = rowStart + col * uvPixelStride
            out[pos++] = vBuffer.get(uvIndex)
            out[pos++] = uBuffer.get(uvIndex)
        }
    }

    return out
}
