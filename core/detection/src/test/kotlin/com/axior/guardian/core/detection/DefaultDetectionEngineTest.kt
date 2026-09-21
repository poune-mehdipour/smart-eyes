package com.axior.guardian.core.detection

import com.axior.guardian.core.model.AnalysisFrame
import com.axior.guardian.core.model.BoundingBox
import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.DetectorType
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DefaultDetectionEngineTest {

    /** A fake detector emitting one detection per declared type. */
    private class FakeDetector(
        override val type: DetectorType,
        override val types: Set<DetectorType> = setOf(type),
    ) : Detector {
        var initialized = false
        override suspend fun initialize() { initialized = true }
        override suspend fun detect(frame: AnalysisFrame): List<Detection> =
            types.map { Detection(it, 0.9f, BoundingBox(0f, 0f, 1f, 1f), frame.timestampMs) }
    }

    private fun frame() = AnalysisFrame(4, 4, 0, 0L, ByteArray(4 * 4 * 3 / 2))

    @Test fun `combined detector exposes all its types`() {
        val engine = DefaultDetectionEngine(setOf(FakeDetector(DetectorType.FIRE, setOf(DetectorType.FIRE, DetectorType.SMOKE))))
        assertEquals(setOf(DetectorType.FIRE, DetectorType.SMOKE), engine.availableTypes)
    }

    @Test fun `analyze returns detections from a combined detector`() = runBlocking {
        val engine = DefaultDetectionEngine(setOf(FakeDetector(DetectorType.FIRE, setOf(DetectorType.FIRE, DetectorType.SMOKE))))
        val out = engine.analyze(frame())
        assertEquals(setOf(DetectorType.FIRE, DetectorType.SMOKE), out.map { it.type }.toSet())
    }

    @Test fun `disabling a type filters it out of results`() = runBlocking {
        val engine = DefaultDetectionEngine(setOf(FakeDetector(DetectorType.FIRE, setOf(DetectorType.FIRE, DetectorType.SMOKE))))
        engine.setEnabled(DetectorType.SMOKE, false)
        val out = engine.analyze(frame())
        assertTrue(out.all { it.type == DetectorType.FIRE })
        assertFalse(DetectorType.SMOKE in engine.enabledTypes)
    }

    @Test fun `empty enabled set yields nothing`() = runBlocking {
        val engine = DefaultDetectionEngine(setOf(FakeDetector(DetectorType.FIRE)))
        engine.setEnabled(DetectorType.FIRE, false)
        assertTrue(engine.analyze(frame()).isEmpty())
    }

    @Test fun `initialize propagates to detectors`() = runBlocking {
        val d = FakeDetector(DetectorType.FIRE)
        val engine = DefaultDetectionEngine(setOf(d))
        engine.initialize()
        assertTrue(d.initialized)
    }
}
