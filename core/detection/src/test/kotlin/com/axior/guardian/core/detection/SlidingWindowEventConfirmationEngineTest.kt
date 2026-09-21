package com.axior.guardian.core.detection

import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.DetectorType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SlidingWindowEventConfirmationEngineTest {

    private val policy = ConfirmationPolicy(
        minConfidence = 0.6f, requiredHits = 3, windowMs = 2_000, cooldownMs = 5_000,
    )
    private fun engine() = SlidingWindowEventConfirmationEngine(mapOf(DetectorType.FIRE to policy))
    private fun fire(conf: Float, ts: Long) = Detection(DetectorType.FIRE, conf, null, ts)

    @Test fun `confirms only after required hits within the window`() {
        val e = engine()
        assertTrue(e.process(listOf(fire(0.9f, 0)), 0).isEmpty())
        assertTrue(e.process(listOf(fire(0.9f, 500)), 500).isEmpty())
        val confirmed = e.process(listOf(fire(0.9f, 1000)), 1000)
        assertEquals(1, confirmed.size)
        assertEquals(DetectorType.FIRE, confirmed.first().type)
    }

    @Test fun `below-threshold detections do not count`() {
        val e = engine()
        repeat(5) { i -> e.process(listOf(fire(0.5f, i * 100L)), i * 100L) } // all < 0.6
        assertTrue(e.process(listOf(fire(0.5f, 600)), 600).isEmpty())
    }

    @Test fun `stale hits fall out of the window`() {
        val e = engine()
        e.process(listOf(fire(0.9f, 0)), 0)
        e.process(listOf(fire(0.9f, 500)), 500)
        // third hit arrives after the first has aged out (>2000ms window from now)
        val confirmed = e.process(listOf(fire(0.9f, 2600)), 2600)
        assertTrue(confirmed.isEmpty()) // only 2 hits remain in window
    }

    @Test fun `cooldown suppresses immediate re-confirmation`() {
        val e = engine()
        // confirm at t=1000
        e.process(listOf(fire(0.9f, 0)), 0)
        e.process(listOf(fire(0.9f, 500)), 500)
        assertEquals(1, e.process(listOf(fire(0.9f, 1000)), 1000).size)
        // three more hits during cooldown (<6000) -> no new event
        e.process(listOf(fire(0.9f, 1100)), 1100)
        e.process(listOf(fire(0.9f, 1200)), 1200)
        assertTrue(e.process(listOf(fire(0.9f, 1300)), 1300).isEmpty())
    }

    @Test fun `re-confirms after cooldown expires`() {
        val e = engine()
        e.process(listOf(fire(0.9f, 0)), 0)
        e.process(listOf(fire(0.9f, 500)), 500)
        assertEquals(1, e.process(listOf(fire(0.9f, 1000)), 1000).size) // cooldown until 6000
        val t = 7000L
        e.process(listOf(fire(0.9f, t)), t)
        e.process(listOf(fire(0.9f, t + 300)), t + 300)
        assertEquals(1, e.process(listOf(fire(0.9f, t + 600)), t + 600).size)
    }

    @Test fun `confirmed event carries the strongest confidence`() {
        val e = engine()
        e.process(listOf(fire(0.7f, 0)), 0)
        e.process(listOf(fire(0.95f, 400)), 400)
        val confirmed = e.process(listOf(fire(0.8f, 800)), 800)
        assertEquals(0.95f, confirmed.first().confidence, 1e-4f)
    }

    @Test fun `reset clears window and cooldown`() {
        val e = engine()
        e.process(listOf(fire(0.9f, 0)), 0)
        e.process(listOf(fire(0.9f, 400)), 400)
        e.reset()
        // after reset, two fresh hits are not enough (need 3 again)
        assertTrue(e.process(listOf(fire(0.9f, 800)), 800).isEmpty())
        assertTrue(e.process(listOf(fire(0.9f, 1000)), 1000).isEmpty())
        assertEquals(1, e.process(listOf(fire(0.9f, 1200)), 1200).size)
    }

    @Test fun `unmapped type is ignored`() {
        val e = engine() // only FIRE policy configured
        val smoke = Detection(DetectorType.SMOKE, 0.99f, null, 0)
        assertTrue(e.process(listOf(smoke), 0).isEmpty())
    }
}
