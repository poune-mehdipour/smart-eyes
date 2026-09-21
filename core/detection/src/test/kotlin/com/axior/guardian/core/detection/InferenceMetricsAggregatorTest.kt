package com.axior.guardian.core.detection

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class InferenceMetricsAggregatorTest {

    @Test fun `averages latency over the window`() {
        val agg = InferenceMetricsAggregator(latencyWindow = 3)
        agg.recordFrame(10, 0, 0)
        agg.recordFrame(20, 0, 10)
        agg.recordFrame(30, 0, 20)
        assertEquals(20.0, agg.snapshot().avgInferenceMs, 1e-6)
        assertEquals(30L, agg.snapshot().lastInferenceMs)
    }

    @Test fun `rolling window drops the oldest sample`() {
        val agg = InferenceMetricsAggregator(latencyWindow = 2)
        agg.recordFrame(10, 0, 0)
        agg.recordFrame(20, 0, 10)
        agg.recordFrame(60, 0, 20) // window now [20,60]
        assertEquals(40.0, agg.snapshot().avgInferenceMs, 1e-6)
    }

    @Test fun `counts detections and confirmed events`() {
        val agg = InferenceMetricsAggregator()
        agg.recordFrame(10, 2, 0)
        agg.recordFrame(10, 3, 10)
        agg.recordConfirmedEvents(1)
        val s = agg.snapshot()
        assertEquals(5L, s.totalDetections)
        assertEquals(1L, s.confirmedEvents)
    }

    @Test fun `computes fps once a second has elapsed`() {
        val agg = InferenceMetricsAggregator()
        // 4 frames within the first second, then one crossing the boundary
        agg.recordFrame(10, 0, 0)
        agg.recordFrame(10, 0, 250)
        agg.recordFrame(10, 0, 500)
        agg.recordFrame(10, 0, 750)
        agg.recordFrame(10, 0, 1000) // >= 1000ms since window start -> fps latches
        assertEquals(5, agg.snapshot().processedFps)
    }

    @Test fun `tracks dropped frames and model load time`() {
        val agg = InferenceMetricsAggregator()
        agg.setModelLoadMs(123)
        agg.recordDropped(4)
        val s = agg.snapshot()
        assertEquals(123L, s.modelLoadMs)
        assertEquals(4L, s.droppedFrames)
    }

    @Test fun `reset clears counters but keeps model load time`() {
        val agg = InferenceMetricsAggregator()
        agg.setModelLoadMs(99)
        agg.recordFrame(10, 5, 0)
        agg.recordDropped(2)
        agg.reset()
        val s = agg.snapshot()
        assertEquals(0L, s.totalDetections)
        assertEquals(0L, s.droppedFrames)
        assertTrue(s.avgInferenceMs == 0.0)
        assertEquals(99L, s.modelLoadMs) // retained
    }
}
