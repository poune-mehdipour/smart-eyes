package com.axior.guardian.feature.monitoring

import androidx.camera.view.LifecycleCameraController
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.axior.guardian.core.alerts.AlertController
import com.axior.guardian.core.camera.CameraFrameSource
import com.axior.guardian.core.cloud.CloudReporter
import com.axior.guardian.core.detection.DetectionConfig
import com.axior.guardian.core.detection.DetectionEngine
import com.axior.guardian.core.detection.EventConfirmationEngine
import com.axior.guardian.core.detection.InferenceMetricsAggregator
import com.axior.guardian.core.model.AnalysisFrame
import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.ModelStatus
import com.axior.guardian.core.model.SecurityEvent
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Owns the monitoring loop: initialise the model, start/stop the frame source,
 * push each frame through detection -> event confirmation -> alert, and expose a
 * single UI snapshot with real measured diagnostics.
 *
 * Design points that matter:
 *  - The model is initialised once, off the main thread; the UI reflects its
 *    real [ModelStatus] (including "not provisioned"), never a faked "ready".
 *  - Each frame is timed end-to-end and fed to [InferenceMetricsAggregator];
 *    every number shown to the operator is measured on-device.
 *  - Each frame is wrapped in try/catch so a single failure can never terminate
 *    monitoring — it is counted and skipped.
 *  - Alerts fire only from confirmed [SecurityEvent]s, never raw detections.
 */
@HiltViewModel
class MonitoringViewModel @Inject constructor(
    private val frameSource: CameraFrameSource,
    private val detectionEngine: DetectionEngine,
    private val eventEngine: EventConfirmationEngine,
    private val alertController: AlertController,
    private val cloudReporter: CloudReporter,
    config: DetectionConfig,
) : ViewModel() {

    val cameraController: LifecycleCameraController get() = frameSource.controller

    private val _state = MutableStateFlow(MonitoringUiState(soundEnabled = alertController.soundEnabled))
    val state = _state.asStateFlow()

    private val metrics = InferenceMetricsAggregator(config.latencyWindow)
    private var monitoringJob: Job? = null
    private var telemetryJob: Job? = null

    init {
        // Reflect the real model lifecycle in the UI as it loads.
        viewModelScope.launch {
            detectionEngine.modelStatus.collect { status ->
                if (status is ModelStatus.Ready) metrics.setModelLoadMs(status.metadata.loadTimeMs)
                _state.update {
                    it.copy(
                        modelStatus = status,
                        errorMessage = (status as? ModelStatus.Error)?.message,
                        metrics = metrics.snapshot().copy(droppedFrames = frameSource.droppedFrames),
                    )
                }
            }
        }
        // Load the model once, off the main thread. Missing model -> NotProvisioned.
        viewModelScope.launch(Dispatchers.Default) {
            runCatching { detectionEngine.initialize() }
        }
    }

    fun onCameraPermissionResult(granted: Boolean) {
        _state.update { it.copy(hasCameraPermission = granted) }
    }

    fun toggleSound() {
        val next = !alertController.soundEnabled
        alertController.soundEnabled = next
        _state.update { it.copy(soundEnabled = next) }
    }

    fun startMonitoring() {
        if (_state.value.isMonitoring) return
        if (!_state.value.hasCameraPermission) return

        eventEngine.reset()
        metrics.reset()
        frameSource.start()

        monitoringJob = viewModelScope.launch(Dispatchers.Default) {
            frameSource.frames.collect { frame -> processFrame(frame) }
        }

        // Periodic health snapshot to the cloud while monitoring. reportTelemetry
        // is fire-and-forget and a no-op in the offline flavor; a network outage
        // can only ever cost telemetry, never detection.
        telemetryJob = viewModelScope.launch(Dispatchers.Default) {
            while (isActive) {
                val s = _state.value
                cloudReporter.reportTelemetry(
                    monitoring = s.isMonitoring,
                    modelStatus = modelStatusLabel(s.modelStatus),
                    metrics = s.metrics,
                )
                delay(TELEMETRY_INTERVAL_MS)
            }
        }

        _state.update { it.copy(status = MonitoringStatus.Monitoring) }
    }

    fun stopMonitoring() {
        if (!_state.value.isMonitoring) return
        monitoringJob?.cancel()
        monitoringJob = null
        telemetryJob?.cancel()
        telemetryJob = null
        frameSource.stop()
        eventEngine.reset()
        _state.update {
            it.copy(
                status = MonitoringStatus.Idle,
                activeDetections = emptyList(),
            )
        }
    }

    private suspend fun processFrame(frame: AnalysisFrame) {
        val start = System.nanoTime()
        val detections: List<Detection>
        val newEvent: SecurityEvent?
        try {
            detections = detectionEngine.analyze(frame)
            val events = eventEngine.process(detections, frame.timestampMs)
            if (events.isNotEmpty()) {
                metrics.recordConfirmedEvents(events.size)
                // Local alert first — it must never wait on anything cloud-shaped.
                events.forEach { alertController.onConfirmedEvent(it) }
                // Then queue for cloud delivery: non-blocking, non-throwing,
                // no-op when cloud reporting is disabled or unconfigured.
                events.forEach { cloudReporter.reportEvent(it) }
            }
            newEvent = events.lastOrNull()
        } catch (e: Exception) {
            // One failed frame must not kill monitoring: count it, keep going.
            metrics.recordDropped()
            _state.update {
                it.copy(metrics = metrics.snapshot().copy(droppedFrames = frameSource.droppedFrames))
            }
            return
        }

        val latencyMs = (System.nanoTime() - start) / 1_000_000L
        val now = System.currentTimeMillis()
        metrics.recordFrame(latencyMs, detections.size, now)

        _state.update { current ->
            current.copy(
                activeDetections = detections,
                frameAspectRatio = uprightAspect(frame),
                lastEvent = newEvent ?: current.lastEvent,
                metrics = metrics.snapshot().copy(droppedFrames = frameSource.droppedFrames),
            )
        }
    }

    private fun modelStatusLabel(status: ModelStatus): String = when (status) {
        is ModelStatus.Ready -> "READY"
        is ModelStatus.Loading -> "LOADING"
        is ModelStatus.NotProvisioned -> "NOT_PROVISIONED"
        is ModelStatus.Error -> "ERROR"
    }

    private fun uprightAspect(frame: AnalysisFrame): Float {
        val rot = ((frame.rotationDegrees % 360) + 360) % 360
        val (w, h) = if (rot == 90 || rot == 270) frame.height to frame.width
        else frame.width to frame.height
        return if (h == 0) 0f else w.toFloat() / h.toFloat()
    }

    override fun onCleared() {
        stopMonitoring()
        detectionEngine.close()
        alertController.release()
    }

    private companion object {
        const val TELEMETRY_INTERVAL_MS = 30_000L
    }
}
