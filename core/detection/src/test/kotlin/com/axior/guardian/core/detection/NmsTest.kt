package com.axior.guardian.core.detection

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class NmsTest {

    private fun box(l: Float, t: Float, r: Float, b: Float) = PixelBox(l, t, r, b)

    @Test fun `iou of identical boxes is one`() {
        val a = box(0f, 0f, 10f, 10f)
        assertEquals(1f, Nms.iou(a, a), 1e-5f)
    }

    @Test fun `iou of disjoint boxes is zero`() {
        assertEquals(0f, Nms.iou(box(0f, 0f, 10f, 10f), box(20f, 20f, 30f, 30f)), 1e-5f)
    }

    @Test fun `iou half overlap`() {
        // A=[0,0,10,10] area100, B=[5,0,15,10] area100, inter=[5,0,10,10]=50, union=150
        assertEquals(50f / 150f, Nms.iou(box(0f, 0f, 10f, 10f), box(5f, 0f, 15f, 10f)), 1e-5f)
    }

    @Test fun `overlapping same-class boxes are suppressed keeping the strongest`() {
        val strong = RawDetection(0, 0.9f, box(0f, 0f, 10f, 10f))
        val weakDup = RawDetection(0, 0.6f, box(1f, 1f, 11f, 11f)) // high IoU with strong
        val kept = Nms.suppress(listOf(weakDup, strong), iouThreshold = 0.45f)
        assertEquals(1, kept.size)
        assertEquals(0.9f, kept.first().score, 1e-4f)
    }

    @Test fun `boxes of different classes never suppress each other`() {
        val fire = RawDetection(0, 0.9f, box(0f, 0f, 10f, 10f))
        val smoke = RawDetection(1, 0.8f, box(0f, 0f, 10f, 10f)) // identical box, other class
        val kept = Nms.suppress(listOf(fire, smoke), iouThreshold = 0.45f)
        assertEquals(2, kept.size)
    }

    @Test fun `low-overlap same-class boxes are both kept`() {
        val a = RawDetection(0, 0.9f, box(0f, 0f, 10f, 10f))
        val b = RawDetection(0, 0.8f, box(8f, 8f, 18f, 18f)) // small overlap
        assertEquals(2, Nms.suppress(listOf(a, b), iouThreshold = 0.45f).size)
    }

    @Test fun `maxDetections truncates by score`() {
        val d = (1..10).map { RawDetection(0, it / 10f, box(it * 100f, 0f, it * 100f + 10f, 10f)) }
        val kept = Nms.suppress(d, iouThreshold = 0.45f, maxDetections = 3)
        assertEquals(3, kept.size)
        assertTrue(kept.first().score >= kept.last().score)
        assertEquals(1.0f, kept.first().score, 1e-4f)
    }

    @Test fun `empty input yields empty output`() {
        assertTrue(Nms.suppress(emptyList(), 0.45f).isEmpty())
    }
}
