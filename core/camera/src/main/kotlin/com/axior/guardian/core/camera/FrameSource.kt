package com.axior.guardian.core.camera

import com.axior.guardian.core.model.AnalysisFrame
import kotlinx.coroutines.flow.Flow

/**
 * A source of [AnalysisFrame]s. This is the single seam between "where frames
 * come from" and "what we do with them".
 *
 * The phone MVP is backed by CameraX; the enterprise tier will add an RTSP-backed
 * implementation. Nothing downstream of this interface — engine, event pipeline,
 * alerts, storage — changes when the source does.
 */
interface FrameSource {
    /**
     * A hot stream of analysis frames, already throttled to the configured rate
     * and delivered with keep-only-latest semantics (slow consumers drop frames
     * rather than build a backlog).
     */
    val frames: Flow<AnalysisFrame>

    /**
     * Count of frames delivered by the camera but intentionally skipped by the
     * source's own rate throttle. A real measured number (not the frames CameraX
     * drops upstream via keep-only-latest, which are not observable here).
     */
    val droppedFrames: Long get() = 0L

    /** Begin producing frames. Idempotent. */
    fun start()

    /** Stop producing frames. Idempotent. */
    fun stop()
}
