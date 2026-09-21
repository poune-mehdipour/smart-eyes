package com.axior.guardian.core.cloud

import com.axior.guardian.core.model.BoundingBox
import com.axior.guardian.core.model.DetectorType
import com.axior.guardian.core.model.InferenceMetrics
import com.axior.guardian.core.model.SecurityEvent
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class CloudContractTest {

    private val event = SecurityEvent(
        id = "evt-123",
        type = DetectorType.FIRE,
        confidence = 0.83f,
        firstSeenMs = 1_758_441_590_000,
        confirmedAtMs = 1_758_441_600_000,
        boundingBox = BoundingBox(0.1f, 0.2f, 0.3f, 0.4f),
    )

    @Test
    fun `event json matches the connector's ingest contract field for field`() {
        val json = CloudContract.eventJson("pixel-7-abc", "1.0", event)
        // Field names must match cloud-connector/internal/ingest eventRequest.
        assertTrue(json.contains("\"deviceId\":\"pixel-7-abc\""))
        assertTrue(json.contains("\"eventId\":\"evt-123\""))
        assertTrue(json.contains("\"type\":\"FIRE\""))
        assertTrue(json.contains("\"firstSeenMs\":1758441590000"))
        assertTrue(json.contains("\"confirmedAtMs\":1758441600000"))
        assertTrue(json.contains("\"appVersion\":\"1.0\""))
        assertTrue(json.contains("\"confidence\":0.83"))
        assertTrue(json.startsWith("{") && json.endsWith("}"))
    }

    @Test
    fun `telemetry json matches the connector's ingest contract`() {
        val metrics = InferenceMetrics(
            lastInferenceMs = 42,
            processedFps = 4,
            droppedFrames = 7,
            confirmedEvents = 2,
        )
        val json = CloudContract.telemetryJson(
            deviceId = "pixel-7-abc", appVersion = "1.0", reportedAtMs = 123L,
            monitoring = true, modelStatus = "READY", metrics = metrics,
        )
        assertTrue(json.contains("\"deviceId\":\"pixel-7-abc\""))
        assertTrue(json.contains("\"reportedAtMs\":123"))
        assertTrue(json.contains("\"monitoringStatus\":\"MONITORING\""))
        assertTrue(json.contains("\"modelStatus\":\"READY\""))
        assertTrue(json.contains("\"inferenceLatencyMs\":42"))
        assertTrue(json.contains("\"processedFps\":4"))
        assertTrue(json.contains("\"droppedFrames\":7"))
        assertTrue(json.contains("\"confirmedEvents\":2"))
    }

    @Test
    fun `idle monitoring status is IDLE`() {
        val json = CloudContract.telemetryJson("d", "1", 1, false, "READY", InferenceMetrics())
        assertTrue(json.contains("\"monitoringStatus\":\"IDLE\""))
    }

    @Test
    fun `strings are escaped so hostile input cannot break the payload`() {
        assertEquals("\"a\\\"b\"", CloudContract.quote("a\"b"))
        assertEquals("\"a\\\\b\"", CloudContract.quote("a\\b"))
        assertEquals("\"a\\nb\"", CloudContract.quote("a\nb"))
        assertEquals("\"a\\u0001b\"", CloudContract.quote("a\u0001b"))
    }

    @Test
    fun `non-finite confidence is written as 0 not NaN`() {
        val bad = event.copy(confidence = Float.NaN)
        val json = CloudContract.eventJson("d", "1", bad)
        assertTrue(json.contains("\"confidence\":0"))
        assertTrue(!json.contains("NaN"))
    }
}
