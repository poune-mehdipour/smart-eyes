package com.axior.guardian.core.camera

import androidx.camera.view.LifecycleCameraController

/**
 * A [FrameSource] backed by an on-device camera. Exposes the underlying
 * [LifecycleCameraController] so the UI layer can attach a preview and bind the
 * camera to a lifecycle, while keeping the analysis pipeline owned here.
 */
interface CameraFrameSource : FrameSource {
    val controller: LifecycleCameraController
}
