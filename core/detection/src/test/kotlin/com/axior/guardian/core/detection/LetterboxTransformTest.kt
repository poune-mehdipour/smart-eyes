package com.axior.guardian.core.detection

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LetterboxTransformTest {

    @Test fun `square frame maps with no padding`() {
        val t = LetterboxTransform.compute(640, 640, 640, 640)
        assertEquals(1f, t.scale, 1e-5f)
        assertEquals(0f, t.padX, 1e-5f)
        assertEquals(0f, t.padY, 1e-5f)
        val (nx, ny) = t.pointToNormalized(320f, 320f)
        assertEquals(0.5f, nx, 1e-4f)
        assertEquals(0.5f, ny, 1e-4f)
    }

    @Test fun `landscape frame is letterboxed with vertical padding`() {
        // 1280x720 into 640x640 -> scale 0.5, newH=360, padY=(640-360)/2=140
        val t = LetterboxTransform.compute(1280, 720, 640, 640)
        assertEquals(0.5f, t.scale, 1e-5f)
        assertEquals(0f, t.padX, 1e-5f)
        assertEquals(140f, t.padY, 1e-3f)
        // centre of the padded content maps to centre of the frame
        val (nx, ny) = t.pointToNormalized(320f, 320f)
        assertEquals(0.5f, nx, 1e-4f)
        assertEquals(0.5f, ny, 1e-4f)
    }

    @Test fun `box round-trips through the letterbox`() {
        val t = LetterboxTransform.compute(1280, 720, 640, 640)
        // a box at upright-frame normalised [0.25..0.75] x, [0.25..0.75] y
        // upright px: x 320..960, y 180..540 ; scaled*0.5: 160..480, 90..270 ; +padY140: 230..410
        val box = PixelBox(left = 160f, top = 230f, right = 480f, bottom = 410f)
        val n = t.boxToNormalized(box)
        assertEquals(0.25f, n.left, 1e-3f)
        assertEquals(0.75f, n.right, 1e-3f)
        assertEquals(0.25f, n.top, 1e-3f)
        assertEquals(0.75f, n.bottom, 1e-3f)
    }

    @Test fun `coordinates are clipped to the unit square`() {
        val t = LetterboxTransform.compute(640, 640, 640, 640)
        val (nx, ny) = t.pointToNormalized(-50f, 900f)
        assertEquals(0f, nx, 1e-5f)
        assertEquals(1f, ny, 1e-5f)
    }

    @Test fun `normalized box is well-ordered`() {
        val t = LetterboxTransform.compute(720, 1280, 640, 640)
        val n = t.boxToNormalized(PixelBox(400f, 100f, 200f, 500f)) // deliberately inverted x
        assertTrue(n.left <= n.right)
        assertTrue(n.top <= n.bottom)
    }
}
