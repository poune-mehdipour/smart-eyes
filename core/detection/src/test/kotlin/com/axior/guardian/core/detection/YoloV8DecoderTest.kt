package com.axior.guardian.core.detection

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class YoloV8DecoderTest {

    private val spec = TestSpecs.fireSmoke(coordinatesNormalized = false) // pixel coords
    private val config = DetectionConfig(confidenceThreshold = 0.5f)

    @Test fun `decodes a single fire box in pixel space`() {
        // anchor: cx=320 cy=320 w=100 h=200, fire=0.9 smoke=0.1
        val (raw, ch, n) = buildYoloOutput(2, listOf(floatArrayOf(320f, 320f, 100f, 200f, 0.9f, 0.1f)))
        val out = YoloV8Decoder.decode(raw, ch, n, spec, config)
        assertEquals(1, out.size)
        val d = out.first()
        assertEquals(0, d.classIndex)
        assertEquals(0.9f, d.score, 1e-4f)
        assertEquals(270f, d.box.left, 1e-3f)   // 320 - 100/2
        assertEquals(220f, d.box.top, 1e-3f)    // 320 - 200/2
        assertEquals(370f, d.box.right, 1e-3f)
        assertEquals(420f, d.box.bottom, 1e-3f)
    }

    @Test fun `confidence threshold filters weak boxes`() {
        val (raw, ch, n) = buildYoloOutput(2, listOf(
            floatArrayOf(100f, 100f, 20f, 20f, 0.9f, 0.1f), // keep
            floatArrayOf(200f, 200f, 20f, 20f, 0.30f, 0.2f), // drop (< 0.5)
        ))
        val out = YoloV8Decoder.decode(raw, ch, n, spec, config)
        assertEquals(1, out.size)
        assertEquals(0.9f, out.first().score, 1e-4f)
    }

    @Test fun `argmax picks the stronger class`() {
        val (raw, ch, n) = buildYoloOutput(2, listOf(floatArrayOf(100f, 100f, 20f, 20f, 0.2f, 0.8f)))
        val out = YoloV8Decoder.decode(raw, ch, n, spec, config)
        assertEquals(1, out.first().classIndex) // smoke
    }

    @Test fun `normalized coordinates are scaled to input pixels`() {
        val nspec = TestSpecs.fireSmoke(coordinatesNormalized = true)
        // cx=0.5 cy=0.5 w=0.1 h=0.1 -> 320,320,64,64 at 640 input
        val (raw, ch, n) = buildYoloOutput(2, listOf(floatArrayOf(0.5f, 0.5f, 0.1f, 0.1f, 0.9f, 0.0f)))
        val out = YoloV8Decoder.decode(raw, ch, n, nspec, config)
        val d = out.first()
        assertEquals(288f, d.box.left, 1e-2f)  // 320 - 64/2
        assertEquals(352f, d.box.right, 1e-2f)
    }

    @Test fun `empty output yields no detections`() {
        val (raw, ch, n) = buildYoloOutput(2, emptyList())
        // n=0 -> zero-length; use an explicit empty buffer of correct channel count
        val out = YoloV8Decoder.decode(FloatArray(6 * 0), 6, 0, spec, config)
        assertTrue(out.isEmpty())
        assertEquals(0, raw.size)
        assertEquals(6, ch); assertEquals(0, n)
    }

    @Test fun `wrong channel count is rejected`() {
        // spec has 2 classes -> expects 6 channels; give 7
        val raw = FloatArray(7 * 3)
        val ex = assertThrows(DecodeException::class.java) {
            YoloV8Decoder.decode(raw, 7, 3, spec, config)
        }
        assertTrue(ex.message!!.contains("channel"))
    }

    @Test fun `undersized output buffer is rejected`() {
        val raw = FloatArray(6 * 3 - 1) // one short for [6 x 3]
        assertThrows(DecodeException::class.java) {
            YoloV8Decoder.decode(raw, 6, 3, spec, config)
        }
    }
}
