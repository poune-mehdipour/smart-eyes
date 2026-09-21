package com.axior.guardian.core.detection

import com.axior.guardian.core.model.BoundingBox
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ViewportMapperTest {

    @Test
    fun `full-frame box fills view when aspect ratios match`() {
        // 3:4 frame into a 300x400 view (same aspect): no crop, direct mapping.
        val rect = ViewportMapper.mapBox(
            box = BoundingBox(0f, 0f, 1f, 1f),
            frameAspectRatio = 300f / 400f,
            viewWidth = 300f,
            viewHeight = 400f,
        )
        assertEquals(0f, rect.left, 0.5f)
        assertEquals(0f, rect.top, 0.5f)
        assertEquals(300f, rect.right, 0.5f)
        assertEquals(400f, rect.bottom, 0.5f)
    }

    @Test
    fun `centered box stays centered`() {
        val rect = ViewportMapper.mapBox(
            box = BoundingBox(0.25f, 0.25f, 0.75f, 0.75f),
            frameAspectRatio = 300f / 400f,
            viewWidth = 300f,
            viewHeight = 400f,
        )
        assertEquals(75f, rect.left, 0.5f)
        assertEquals(100f, rect.top, 0.5f)
        assertEquals(225f, rect.right, 0.5f)
        assertEquals(300f, rect.bottom, 0.5f)
    }

    @Test
    fun `wider frame than view is cropped horizontally, not squashed`() {
        // 16:9 frame (aspect 1.777) into a 400x400 square view.
        // FILL_CENTER covers -> vertical fills exactly, horizontal overflows and is cropped.
        val frameAspect = 16f / 9f
        val full = ViewportMapper.mapBox(
            box = BoundingBox(0f, 0f, 1f, 1f),
            frameAspectRatio = frameAspect,
            viewWidth = 400f,
            viewHeight = 400f,
        )
        // Top/bottom reach the view edges (vertical is the fill axis).
        assertEquals(0f, full.top, 0.5f)
        assertEquals(400f, full.bottom, 0.5f)
        // Left/right are clipped to the view (the frame is wider than the view).
        assertEquals(0f, full.left, 0.5f)
        assertEquals(400f, full.right, 0.5f)

        // A horizontally centered half-width box should sit inside the view.
        val mid = ViewportMapper.mapBox(
            box = BoundingBox(0.25f, 0.4f, 0.75f, 0.6f),
            frameAspectRatio = frameAspect,
            viewWidth = 400f,
            viewHeight = 400f,
        )
        assertTrue(mid.left > 0f)
        assertTrue(mid.right < 400f)
        assertTrue(mid.left < mid.right)
    }

    @Test
    fun `mirror flips horizontally`() {
        val normal = ViewportMapper.mapBox(
            box = BoundingBox(0.0f, 0.4f, 0.2f, 0.6f),
            frameAspectRatio = 300f / 400f,
            viewWidth = 300f,
            viewHeight = 400f,
            mirror = false,
        )
        val mirrored = ViewportMapper.mapBox(
            box = BoundingBox(0.0f, 0.4f, 0.2f, 0.6f),
            frameAspectRatio = 300f / 400f,
            viewWidth = 300f,
            viewHeight = 400f,
            mirror = true,
        )
        // A left-edge box becomes a right-edge box under mirroring.
        assertEquals(300f - normal.right, mirrored.left, 0.5f)
        assertEquals(300f - normal.left, mirrored.right, 0.5f)
    }

    @Test
    fun `degenerate inputs yield empty rect`() {
        val rect = ViewportMapper.mapBox(
            box = BoundingBox(0f, 0f, 1f, 1f),
            frameAspectRatio = 0f,
            viewWidth = 0f,
            viewHeight = 0f,
        )
        assertEquals(0f, rect.width, 0f)
        assertEquals(0f, rect.height, 0f)
    }
}
