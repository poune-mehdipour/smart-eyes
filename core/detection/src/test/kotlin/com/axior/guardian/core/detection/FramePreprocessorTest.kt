package com.axior.guardian.core.detection

import com.axior.guardian.core.model.AnalysisFrame
import com.axior.guardian.core.model.Normalization
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class FramePreprocessorTest {

    /** Build a solid-colour NV21 frame of the given size. */
    private fun solidNv21(w: Int, h: Int, y: Int, u: Int, v: Int): ByteArray {
        val out = ByteArray(w * h * 3 / 2)
        for (i in 0 until w * h) out[i] = y.toByte()
        var p = w * h
        repeat((w / 2) * (h / 2)) {
            out[p++] = v.toByte()
            out[p++] = u.toByte()
        }
        return out
    }

    @Test fun `output tensor has the expected NHWC size`() {
        val spec = TestSpecs.fireSmoke()
        val pre = FramePreprocessor(spec)
        val frame = AnalysisFrame(640, 640, 0, 0L, solidNv21(640, 640, 128, 128, 128))
        val result = pre.process(frame)
        assertEquals(640 * 640 * 3, result.data.size)
    }

    @Test fun `grey frame normalises into 0-1 range`() {
        val spec = TestSpecs.fireSmoke().copy(normalization = Normalization.SCALE_0_1)
        val pre = FramePreprocessor(spec)
        val frame = AnalysisFrame(64, 64, 0, 0L, solidNv21(64, 64, 128, 128, 128))
        val result = pre.process(frame)
        // centre pixel should be inside the content region and within [0,1]
        val centre = ((320 * 640) + 320) * 3
        assertTrue(result.data.all { it in 0f..1f })
        assertTrue(result.data[centre] in 0f..1f)
    }

    @Test fun `rotation swaps upright dimensions in the transform`() {
        val spec = TestSpecs.fireSmoke()
        val pre = FramePreprocessor(spec)
        val frame = AnalysisFrame(480, 640, 90, 0L, solidNv21(480, 640, 100, 128, 128))
        val result = pre.process(frame)
        assertEquals(640, result.transform.uprightWidth)  // rotated: src H -> upright W
        assertEquals(480, result.transform.uprightHeight)
    }

    @Test fun `reused buffer of the right size is reused`() {
        val spec = TestSpecs.fireSmoke()
        val pre = FramePreprocessor(spec)
        val buf = FloatArray(640 * 640 * 3)
        val frame = AnalysisFrame(320, 320, 0, 0L, solidNv21(320, 320, 50, 128, 128))
        val result = pre.process(frame, reuse = buf)
        assertTrue(result.data === buf)
    }

    @Test fun `red frame decodes to a high red channel`() {
        // Y=76,U=84,V=255 is approximately pure red in YUV
        val spec = TestSpecs.fireSmoke()
        val pre = FramePreprocessor(spec)
        val frame = AnalysisFrame(64, 64, 0, 0L, solidNv21(64, 64, 76, 84, 255))
        val result = pre.process(frame)
        val centre = ((320 * 640) + 320) * 3
        val r = result.data[centre]; val g = result.data[centre + 1]; val b = result.data[centre + 2]
        assertTrue("expected red dominant but was r=$r g=$g b=$b", r > g && r > b)
    }
}
