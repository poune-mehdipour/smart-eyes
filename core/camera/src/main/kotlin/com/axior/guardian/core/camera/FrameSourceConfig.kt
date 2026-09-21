package com.axior.guardian.core.camera

import androidx.camera.core.CameraSelector

/**
 * Tuning knobs for a [CameraFrameSource].
 *
 * [targetFps] deliberately defaults low: continuous full-rate inference is the
 * fastest way to flatten a battery and thermally throttle a phone. A few frames
 * per second is plenty for the hazards in scope.
 */
data class FrameSourceConfig(
    val targetFps: Int = 4,
    val lensFacing: Int = CameraSelector.LENS_FACING_BACK,
)
