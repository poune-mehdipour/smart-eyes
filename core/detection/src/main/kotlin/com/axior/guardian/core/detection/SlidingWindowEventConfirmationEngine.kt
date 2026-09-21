package com.axior.guardian.core.detection

import com.axior.guardian.core.model.BoundingBox
import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.DetectorType
import com.axior.guardian.core.model.SecurityEvent
import java.util.ArrayDeque
import java.util.UUID

/**
 * Confirms a hazard when at least [ConfirmationPolicy.requiredHits] qualifying
 * detections of the same type land within [ConfirmationPolicy.windowMs], then
 * holds a per-type cooldown so a single ongoing fire does not re-fire every
 * second.
 */
class SlidingWindowEventConfirmationEngine(
    private val policies: Map<DetectorType, ConfirmationPolicy>,
) : EventConfirmationEngine {

    private class Hit(val timestampMs: Long, val confidence: Float, val box: BoundingBox?)

    private val windows = HashMap<DetectorType, ArrayDeque<Hit>>()
    private val cooldownUntil = HashMap<DetectorType, Long>()

    override fun process(detections: List<Detection>, nowMs: Long): List<SecurityEvent> {
        if (detections.isEmpty()) return emptyList()

        var confirmed: MutableList<SecurityEvent>? = null

        for (detection in detections) {
            val policy = policies[detection.type] ?: continue
            if (detection.confidence < policy.minConfidence) continue

            val window = windows.getOrPut(detection.type) { ArrayDeque() }
            window.addLast(Hit(nowMs, detection.confidence, detection.boundingBox))
            evict(window, nowMs, policy.windowMs)

            if (nowMs < (cooldownUntil[detection.type] ?: 0L)) continue
            if (window.size < policy.requiredHits) continue

            val event = confirm(detection.type, window, nowMs)
            cooldownUntil[detection.type] = nowMs + policy.cooldownMs
            window.clear()
            (confirmed ?: mutableListOf<SecurityEvent>().also { confirmed = it }).add(event)
        }

        return confirmed ?: emptyList()
    }

    private fun confirm(type: DetectorType, window: ArrayDeque<Hit>, nowMs: Long): SecurityEvent {
        val strongest = window.maxByOrNull { it.confidence }
        return SecurityEvent(
            id = UUID.randomUUID().toString(),
            type = type,
            confidence = strongest?.confidence ?: 0f,
            firstSeenMs = window.peekFirst()?.timestampMs ?: nowMs,
            confirmedAtMs = nowMs,
            boundingBox = strongest?.box,
        )
    }

    private fun evict(window: ArrayDeque<Hit>, nowMs: Long, windowMs: Long) {
        val cutoff = nowMs - windowMs
        while (window.isNotEmpty() && window.peekFirst().timestampMs < cutoff) {
            window.removeFirst()
        }
    }

    override fun reset() {
        windows.clear()
        cooldownUntil.clear()
    }
}
