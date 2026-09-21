package com.axior.guardian.core.model

/**
 * A single camera frame, normalised into a source-independent representation.
 *
 * Detectors consume [AnalysisFrame] and never touch CameraX (or, later, RTSP)
 * types directly. This is the seam that lets the AI engine be reused verbatim
 * when the frame source changes.
 *
 * Pixels are carried as NV21 (Y plane followed by interleaved V/U), the most
 * broadly accepted input layout for on-device vision runtimes.
 */
class AnalysisFrame(
    val width: Int,
    val height: Int,
    /** Clockwise rotation, in degrees, needed to view the frame upright: 0/90/180/270. */
    val rotationDegrees: Int,
    /** Monotonic capture timestamp in milliseconds. */
    val timestampMs: Long,
    val nv21: ByteArray,
) {
    override fun equals(other: Any?): Boolean {
        if (this === other) return true
        if (other !is AnalysisFrame) return false
        return width == other.width &&
            height == other.height &&
            rotationDegrees == other.rotationDegrees &&
            timestampMs == other.timestampMs &&
            nv21.contentEquals(other.nv21)
    }

    override fun hashCode(): Int {
        var result = width
        result = 31 * result + height
        result = 31 * result + rotationDegrees
        result = 31 * result + timestampMs.hashCode()
        result = 31 * result + nv21.contentHashCode()
        return result
    }
}
