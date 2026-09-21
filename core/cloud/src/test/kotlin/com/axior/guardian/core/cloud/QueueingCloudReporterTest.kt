package com.axior.guardian.core.cloud

import com.axior.guardian.core.model.DetectorType
import com.axior.guardian.core.model.InferenceMetrics
import com.axior.guardian.core.model.SecurityEvent
import kotlinx.coroutines.Dispatchers
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

/**
 * Behavioral tests for the queueing reporter. A scriptable fake gateway
 * stands in for the network; the `sleep` hook replaces real backoff delays,
 * so the tests are fast and deterministic.
 */
class QueueingCloudReporterTest {

    private class FakeGateway(
        /** Results to return, in order; the last one repeats forever. */
        private val script: List<GatewayResult>,
    ) : CloudGateway {
        val calls = AtomicInteger(0)
        val payloads = ConcurrentLinkedQueue<String>()

        private fun next(json: String): GatewayResult {
            payloads.add(json)
            return script.getOrElse(calls.incrementAndGet() - 1) { script.last() }
        }

        override fun postEvent(json: String) = next(json)
        override fun postTelemetry(json: String) = next(json)
    }

    private val config = CloudConfig(
        enabled = true, baseUrl = "http://unused", apiToken = "t",
        deviceId = "test-device", appVersion = "1.0",
    )

    private val event = SecurityEvent(
        id = "evt-1", type = DetectorType.SMOKE, confidence = 0.9f,
        firstSeenMs = 1L, confirmedAtMs = 2L, boundingBox = null,
    )

    private fun reporter(gateway: FakeGateway, maxAttempts: Int = 5) = QueueingCloudReporter(
        config = config, gateway = gateway, dispatcher = Dispatchers.IO,
        maxAttempts = maxAttempts, sleep = { /* no real sleeping in tests */ },
    )

    /** Race-free wait: polls the call counter instead of racing a latch. */
    private fun awaitCalls(gateway: FakeGateway, n: Int) {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(5)
        while (gateway.calls.get() < n) {
            if (System.nanoTime() > deadline) {
                throw AssertionError("gateway never reached $n calls (got ${gateway.calls.get()})")
            }
            Thread.sleep(5)
        }
        // Small grace period so we also catch calls beyond n (over-retrying).
        Thread.sleep(50)
    }

    @Test
    fun `successful event is delivered exactly once with the contract payload`() {
        val gw = FakeGateway(listOf(GatewayResult.Accepted))
        val r = reporter(gw)
        try {
            r.reportEvent(event)
            awaitCalls(gw, 1)
            assertEquals(1, gw.calls.get())
            assertTrue(gw.payloads.first().contains("\"eventId\":\"evt-1\""))
            assertTrue(gw.payloads.first().contains("\"deviceId\":\"test-device\""))
        } finally {
            r.close()
        }
    }

    @Test
    fun `transient failures are retried until success`() {
        val gw = FakeGateway(
            listOf(
                GatewayResult.TransientError("500"),
                GatewayResult.TransientError("timeout"),
                GatewayResult.Accepted,
            ),
        )
        val r = reporter(gw)
        try {
            r.reportEvent(event)
            awaitCalls(gw, 3)
            assertEquals(3, gw.calls.get())
        } finally {
            r.close()
        }
    }

    @Test
    fun `permanent failure is not retried`() {
        val gw = FakeGateway(listOf(GatewayResult.PermanentError("HTTP 400"), GatewayResult.Accepted))
        val r = reporter(gw)
        try {
            r.reportEvent(event)
            // Deliver a second, healthy event; if the first were being
            // retried it would consume the Accepted slot instead.
            r.reportEvent(event.copy(id = "evt-2"))
            awaitCalls(gw, 2)
            assertEquals(2, gw.calls.get())
            assertTrue(gw.payloads.toList()[1].contains("evt-2"))
        } finally {
            r.close()
        }
    }

    @Test
    fun `gives up after maxAttempts transient failures and moves on`() {
        val gw = FakeGateway(listOf(GatewayResult.TransientError("down")))
        val r = reporter(gw, maxAttempts = 3)
        try {
            r.reportEvent(event)
            r.reportEvent(event.copy(id = "evt-2"))
            // 3 failed attempts for evt-1, then 3 for evt-2.
            awaitCalls(gw, 6)
            assertEquals(6, gw.calls.get())
        } finally {
            r.close()
        }
    }

    @Test
    fun `disabled config never touches the gateway`() {
        val gw = FakeGateway(listOf(GatewayResult.Accepted))
        val r = QueueingCloudReporter(
            config = config.copy(enabled = false), gateway = gw,
            dispatcher = Dispatchers.IO, sleep = { },
        )
        try {
            r.reportEvent(event)
            r.reportTelemetry(true, "READY", InferenceMetrics())
            Thread.sleep(100)
            assertEquals(0, gw.calls.get())
        } finally {
            r.close()
        }
    }

    @Test
    fun `reportEvent returns immediately even when delivery is stuck`() {
        val gw = FakeGateway(listOf(GatewayResult.TransientError("down")))
        val r = reporter(gw)
        try {
            val start = System.nanoTime()
            repeat(200) { r.reportEvent(event.copy(id = "evt-$it")) }
            val elapsedMs = (System.nanoTime() - start) / 1_000_000
            // 200 enqueues (queue capacity 64, oldest dropped) must be instant.
            assertTrue("enqueue took ${elapsedMs}ms", elapsedMs < 500)
        } finally {
            r.close()
        }
    }

    @Test
    fun `backoff stays within the configured ceiling`() {
        val r = reporter(FakeGateway(listOf(GatewayResult.Accepted)))
        try {
            for (attempt in 1..12) {
                repeat(20) {
                    val d = r.backoffMs(attempt)
                    assertTrue("backoff $d out of range on attempt $attempt", d in 0..30_000)
                }
            }
        } finally {
            r.close()
        }
    }
}
