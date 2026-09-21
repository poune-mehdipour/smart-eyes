package com.axior.guardian.core.detection

import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.SecurityEvent

/**
 * Turns a stream of raw [Detection]s into a stream of confirmed [SecurityEvent]s.
 * Stateful across frames; not thread-safe — drive it from a single coroutine.
 */
interface EventConfirmationEngine {
    /**
     * Feed the detections for one frame. Returns any events that became
     * confirmed on this frame (usually empty).
     */
    fun process(detections: List<Detection>, nowMs: Long): List<SecurityEvent>

    /** Clear all accumulated state (e.g. when monitoring stops). */
    fun reset()
}
