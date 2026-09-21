package com.axior.guardian.feature.monitoring

import androidx.camera.view.LifecycleCameraController
import androidx.camera.view.PreviewView
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.compose.LocalLifecycleOwner

/**
 * Hosts a CameraX [PreviewView] and binds the shared controller to the current
 * lifecycle. The controller is owned by the frame source, so preview and
 * analysis are always the same camera session.
 */
@Composable
fun CameraPreview(
    controller: LifecycleCameraController,
    modifier: Modifier = Modifier,
) {
    val lifecycleOwner = LocalLifecycleOwner.current
    AndroidView(
        modifier = modifier,
        factory = { context ->
            PreviewView(context).apply {
                this.controller = controller
                controller.bindToLifecycle(lifecycleOwner)
                scaleType = PreviewView.ScaleType.FILL_CENTER
            }
        },
    )
}
