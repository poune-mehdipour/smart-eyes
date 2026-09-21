package com.axior.guardian.core.detection

import com.axior.guardian.core.model.AnalysisFrame
import com.axior.guardian.core.model.InputLayout
import com.axior.guardian.core.model.ModelSpec
import com.axior.guardian.core.model.Normalization

/** Result of preprocessing: the input tensor data plus the geometry to invert it. */
class PreprocessedFrame(
    /** Model input as a flat float array in the spec's layout, ready to feed. */
    val data: FloatArray,
    val transform: LetterboxTransform,
)

/**
 * Converts an [AnalysisFrame] (NV21 + rotation) into a model-ready float tensor,
 * folding YUV->RGB, rotation-to-upright, aspect-preserving letterbox and
 * normalisation into a single pass with no intermediate Bitmap allocation.
 *
 * Pure Kotlin and Android-free on purpose: it is unit-testable and runs
 * unchanged on the future server/RTSP tier. The output float layout follows
 * [ModelSpec.inputLayout] and [ModelSpec.normalization].
 *
 * A reusable output buffer can be supplied to avoid per-frame allocation.
 */
class FramePreprocessor(private val spec: ModelSpec) {

    private val inputW = spec.inputWidth
    private val inputH = spec.inputHeight
    private val padValue = 114 // Ultralytics letterbox grey

    fun process(frame: AnalysisFrame, reuse: FloatArray? = null): PreprocessedFrame {
        val srcW = frame.width
        val srcH = frame.height
        val rot = ((frame.rotationDegrees % 360) + 360) % 360

        val uprightW: Int
        val uprightH: Int
        if (rot == 90 || rot == 270) {
            uprightW = srcH; uprightH = srcW
        } else {
            uprightW = srcW; uprightH = srcH
        }

        val t = LetterboxTransform.compute(uprightW, uprightH, inputW, inputH)
        val nv21 = frame.nv21
        val out = reuse?.takeIf { it.size == inputW * inputH * spec.inputChannels }
            ?: FloatArray(inputW * inputH * spec.inputChannels)

        for (ty in 0 until inputH) {
            for (tx in 0 until inputW) {
                var r = padValue; var g = padValue; var b = padValue
                val insideX = tx >= t.padX && tx < t.padX + uprightW * t.scale
                val insideY = ty >= t.padY && ty < t.padY + uprightH * t.scale
                if (insideX && insideY) {
                    val ux = ((tx - t.padX) / t.scale).toInt().coerceIn(0, uprightW - 1)
                    val uy = ((ty - t.padY) / t.scale).toInt().coerceIn(0, uprightH - 1)
                    val (sx, sy) = uprightToSource(ux, uy, rot, srcW, srcH)
                    val rgb = yuvToRgb(nv21, sx, sy, srcW, srcH)
                    r = rgb[0]; g = rgb[1]; b = rgb[2]
                }
                writePixel(out, tx, ty, r, g, b)
            }
        }
        return PreprocessedFrame(out, t)
    }

    private fun writePixel(out: FloatArray, x: Int, y: Int, r: Int, g: Int, b: Int) {
        val c0: Float; val c1: Float; val c2: Float
        if (spec.inputRgb) { c0 = norm(r); c1 = norm(g); c2 = norm(b) }
        else { c0 = norm(b); c1 = norm(g); c2 = norm(r) }
        when (spec.inputLayout) {
            InputLayout.NHWC -> {
                val base = (y * inputW + x) * 3
                out[base] = c0; out[base + 1] = c1; out[base + 2] = c2
            }
            InputLayout.NCHW -> {
                val plane = inputW * inputH
                val idx = y * inputW + x
                out[idx] = c0; out[plane + idx] = c1; out[2 * plane + idx] = c2
            }
        }
    }

    private fun norm(v: Int): Float = when (spec.normalization) {
        Normalization.SCALE_0_1 -> v / 255f
        Normalization.SCALE_MINUS1_1 -> v / 127.5f - 1f
        Normalization.NONE -> v.toFloat()
    }

    /** Map an upright-frame pixel back to the source (pre-rotation) pixel. */
    private fun uprightToSource(ux: Int, uy: Int, rot: Int, srcW: Int, srcH: Int): IntArray =
        when (rot) {
            90 -> intArrayOf(uy, srcH - 1 - ux)
            180 -> intArrayOf(srcW - 1 - ux, srcH - 1 - uy)
            270 -> intArrayOf(srcW - 1 - uy, ux)
            else -> intArrayOf(ux, uy)
        }.also {
            it[0] = it[0].coerceIn(0, srcW - 1)
            it[1] = it[1].coerceIn(0, srcH - 1)
        }

    /** Standard NV21 (Y plane, then interleaved V/U) -> RGB for one pixel. */
    private fun yuvToRgb(nv21: ByteArray, x: Int, y: Int, w: Int, h: Int): IntArray {
        val yIndex = y * w + x
        val uvStart = w * h
        val uvIndex = uvStart + (y / 2) * w + (x / 2) * 2
        val yy = (nv21[yIndex].toInt() and 0xFF)
        val v = (nv21[uvIndex].toInt() and 0xFF) - 128
        val u = (nv21[uvIndex + 1].toInt() and 0xFF) - 128
        val r = (yy + 1.370705f * v).toInt()
        val g = (yy - 0.337633f * u - 0.698001f * v).toInt()
        val b = (yy + 1.732446f * u).toInt()
        return intArrayOf(r.coerceIn(0, 255), g.coerceIn(0, 255), b.coerceIn(0, 255))
    }
}
