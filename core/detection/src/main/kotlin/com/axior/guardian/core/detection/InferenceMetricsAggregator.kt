package com.axior.guardian.core.detection

import com.axior.guardian.core.model.InferenceMetrics

/**
 * Accumulates measured pipeline health into an [InferenceMetrics] snapshot.
 * Everything here is derived from real measurements fed in by the caller; there
 * are no target or placeholder numbers. Not thread-safe — drive from one coroutine.
 */
class InferenceMetricsAggregator(private val latencyWindow: Int = 30) {

    private val latencies = ArrayDeque<Long>()
    private var latencySum = 0L

    private var modelLoadMs = 0L
    private var lastInferenceMs = 0L
    private var droppedFrames = 0L
    private var totalDetections = 0L
    private var confirmedEvents = 0L

    private var fpsWindowStarted = false
    private var fpsWindowStartMs = 0L
    private var framesInWindow = 0
    private var processedFps = 0

    fun setModelLoadMs(ms: Long) { modelLoadMs = ms }

    fun recordDropped(count: Long = 1) { droppedFrames += count }

    /** Record one processed frame: its analysis latency, detection count, time. */
    fun recordFrame(latencyMs: Long, detectionCount: Int, nowMs: Long) {
        lastInferenceMs = latencyMs
        latencies.addLast(latencyMs)
        latencySum += latencyMs
        while (latencies.size > latencyWindow) {
            latencySum -= latencies.removeFirst()
        }
        totalDetections += detectionCount

        if (!fpsWindowStarted) { fpsWindowStartMs = nowMs; fpsWindowStarted = true }
        framesInWindow++
        if (nowMs - fpsWindowStartMs >= 1000L) {
            processedFps = framesInWindow
            framesInWindow = 0
            fpsWindowStartMs = nowMs
        }
    }

    fun recordConfirmedEvents(count: Int) { confirmedEvents += count }

    fun snapshot(): InferenceMetrics = InferenceMetrics(
        modelLoadMs = modelLoadMs,
        lastInferenceMs = lastInferenceMs,
        avgInferenceMs = if (latencies.isEmpty()) 0.0 else latencySum.toDouble() / latencies.size,
        processedFps = processedFps,
        droppedFrames = droppedFrames,
        totalDetections = totalDetections,
        confirmedEvents = confirmedEvents,
    )

    fun reset() {
        latencies.clear(); latencySum = 0
        lastInferenceMs = 0; droppedFrames = 0
        totalDetections = 0; confirmedEvents = 0
        fpsWindowStarted = false; fpsWindowStartMs = 0; framesInWindow = 0; processedFps = 0
        // modelLoadMs intentionally retained across monitoring sessions.
    }
}
