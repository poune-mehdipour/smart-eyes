package com.axior.guardian.core.cloud

import com.axior.guardian.core.model.InferenceMetrics
import com.axior.guardian.core.model.SecurityEvent

/**
 * The edge -> cloud wire contract. These JSON shapes mirror, field for field,
 * what the Go connector's ingest API expects (cloud-connector/internal/ingest):
 * timestamps are unix milliseconds because that is what the on-device pipeline
 * already produces ([SecurityEvent.confirmedAtMs] etc.).
 *
 * Serialization is a small hand-rolled writer instead of a JSON library:
 * the two payloads total ~15 flat fields, adding a serialization dependency
 * (and its ProGuard/KSP surface) for that buys nothing. The writer escapes
 * strings correctly and is unit-tested (CloudContractTest).
 */
object CloudContract {

    /** Wire form of one confirmed hazard event (POST /v1/edge/events). */
    fun eventJson(deviceId: String, appVersion: String, event: SecurityEvent): String =
        jsonObject(
            "deviceId" to deviceId,
            "eventId" to event.id,
            "type" to event.type.name,
            "confidence" to event.confidence,
            "firstSeenMs" to event.firstSeenMs,
            "confirmedAtMs" to event.confirmedAtMs,
            "appVersion" to appVersion,
        )

    /** Wire form of one telemetry snapshot (POST /v1/edge/telemetry). */
    fun telemetryJson(
        deviceId: String,
        appVersion: String,
        reportedAtMs: Long,
        monitoring: Boolean,
        modelStatus: String,
        metrics: InferenceMetrics,
    ): String = jsonObject(
        "deviceId" to deviceId,
        "reportedAtMs" to reportedAtMs,
        "monitoringStatus" to if (monitoring) "MONITORING" else "IDLE",
        "modelStatus" to modelStatus,
        "inferenceLatencyMs" to metrics.lastInferenceMs,
        "processedFps" to metrics.processedFps,
        "droppedFrames" to metrics.droppedFrames,
        "confirmedEvents" to metrics.confirmedEvents,
        "appVersion" to appVersion,
    )

    // ---- Minimal JSON writer ------------------------------------------------

    private fun jsonObject(vararg fields: Pair<String, Any?>): String =
        fields.joinToString(prefix = "{", postfix = "}", separator = ",") { (k, v) ->
            "${quote(k)}:${encodeValue(v)}"
        }

    private fun encodeValue(v: Any?): String = when (v) {
        null -> "null"
        is String -> quote(v)
        is Boolean, is Int, is Long -> v.toString()
        // Note: a Float is stringified as a Float ("0.83"), never widened to
        // Double first ("0.8299999833106995").
        is Float -> if (v.isFinite()) v.toString() else "0"
        is Double -> if (v.isFinite()) v.toString() else "0"
        else -> error("unsupported JSON value type: ${v::class}")
    }

    internal fun quote(s: String): String {
        val sb = StringBuilder(s.length + 2)
        sb.append('"')
        for (c in s) {
            when (c) {
                '"' -> sb.append("\\\"")
                '\\' -> sb.append("\\\\")
                '\n' -> sb.append("\\n")
                '\r' -> sb.append("\\r")
                '\t' -> sb.append("\\t")
                else -> if (c < ' ') sb.append("\\u%04x".format(c.code)) else sb.append(c)
            }
        }
        sb.append('"')
        return sb.toString()
    }
}
