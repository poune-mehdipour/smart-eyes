package com.axior.guardian.core.camera

import android.content.Context
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.view.CameraController
import androidx.camera.view.LifecycleCameraController
import com.axior.guardian.core.model.AnalysisFrame
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.asSharedFlow
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * CameraX-backed [CameraFrameSource].
 *
 * Uses [LifecycleCameraController], which bundles preview and image analysis
 * against a single lifecycle. The UI attaches a `PreviewView` to [controller];
 * this class owns the analysis analyzer and emits normalised [AnalysisFrame]s.
 */
class CameraXFrameSource(
    context: Context,
    private val config: FrameSourceConfig,
) : CameraFrameSource {

    private val analysisExecutor = Executors.newSingleThreadExecutor()

    private val _frames = MutableSharedFlow<AnalysisFrame>(
        replay = 0,
        extraBufferCapacity = 1,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )
    override val frames = _frames.asSharedFlow()

    private val running = AtomicBoolean(false)
    private val frameIntervalMs: Long = (1000L / config.targetFps.coerceAtLeast(1))
    @Volatile private var lastEmitMs = 0L
    private val dropped = java.util.concurrent.atomic.AtomicLong(0L)

    override val droppedFrames: Long get() = dropped.get()

    override val controller: LifecycleCameraController =
        LifecycleCameraController(context.applicationContext).apply {
            cameraSelector = CameraSelector.Builder()
                .requireLensFacing(config.lensFacing)
                .build()
            setEnabledUseCases(CameraController.IMAGE_ANALYSIS)
            imageAnalysisBackpressureStrategy = ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST
        }

    override fun start() {
        if (!running.compareAndSet(false, true)) return
        controller.setImageAnalysisAnalyzer(analysisExecutor) { imageProxy ->
            try {
                val now = System.currentTimeMillis()
                if (now - lastEmitMs < frameIntervalMs) {
                    dropped.incrementAndGet()
                    return@setImageAnalysisAnalyzer
                }
                lastEmitMs = now

                val frame = AnalysisFrame(
                    width = imageProxy.width,
                    height = imageProxy.height,
                    rotationDegrees = imageProxy.imageInfo.rotationDegrees,
                    timestampMs = now,
                    nv21 = imageProxy.toNv21(),
                )
                if (!_frames.tryEmit(frame)) dropped.incrementAndGet()
            } finally {
                imageProxy.close()
            }
        }
    }

    override fun stop() {
        if (!running.compareAndSet(true, false)) return
        controller.clearImageAnalysisAnalyzer()
    }
}
