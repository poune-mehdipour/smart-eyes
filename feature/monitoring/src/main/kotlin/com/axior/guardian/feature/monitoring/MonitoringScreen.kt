package com.axior.guardian.feature.monitoring

import android.Manifest
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.axior.guardian.core.model.ModelStatus

/**
 * The single screen of the MVP: live preview with a detection overlay, an honest
 * status/diagnostics panel, and Start/Stop + sound controls. Everything shown is
 * driven by real state from [MonitoringViewModel] — there is no decorative or
 * fabricated readout.
 */
@Composable
fun MonitoringScreen(
    modifier: Modifier = Modifier,
    viewModel: MonitoringViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()

    val permissionLauncher = rememberLauncherForActivityResult(
        contract = ActivityResultContracts.RequestPermission(),
        onResult = viewModel::onCameraPermissionResult,
    )

    // Local notifications need a runtime grant on Android 13+. Best-effort:
    // alerts still vibrate and show in-app if this is denied.
    val notificationsLauncher = rememberLauncherForActivityResult(
        contract = ActivityResultContracts.RequestPermission(),
        onResult = { },
    )
    LaunchedEffect(state.hasCameraPermission) {
        if (state.hasCameraPermission && android.os.Build.VERSION.SDK_INT >= 33) {
            notificationsLauncher.launch("android.permission.POST_NOTIFICATIONS")
        }
    }

    Box(modifier = modifier.fillMaxSize().background(Color.Black)) {
        if (state.hasCameraPermission) {
            CameraPreview(
                controller = viewModel.cameraController,
                modifier = Modifier.fillMaxSize(),
            )
            if (state.isMonitoring) {
                DetectionOverlay(
                    detections = state.activeDetections,
                    frameAspectRatio = state.frameAspectRatio,
                    modifier = Modifier.fillMaxSize(),
                )
            }
        } else {
            PermissionRequest(
                onRequest = { permissionLauncher.launch(Manifest.permission.CAMERA) },
                modifier = Modifier.align(Alignment.Center),
            )
        }

        StatusPanel(
            state = state,
            modifier = Modifier
                .align(Alignment.TopCenter)
                .fillMaxWidth()
                .padding(16.dp),
        )

        if (state.hasCameraPermission) {
            Controls(
                state = state,
                onStart = viewModel::startMonitoring,
                onStop = viewModel::stopMonitoring,
                onToggleSound = viewModel::toggleSound,
                modifier = Modifier
                    .align(Alignment.BottomCenter)
                    .fillMaxWidth()
                    .padding(24.dp),
            )
        }
    }
}

@Composable
private fun StatusPanel(state: MonitoringUiState, modifier: Modifier = Modifier) {
    Surface(
        modifier = modifier,
        color = Color.Black.copy(alpha = 0.6f),
        shape = RoundedCornerShape(12.dp),
    ) {
        Column(
            modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    StatusDot(active = state.isMonitoring)
                    Text(
                        text = if (state.isMonitoring) "  Monitoring" else "  Idle",
                        color = if (state.isMonitoring) Color(0xFF4CAF50) else Color.White,
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
                OfflineChip()
            }

            Text(
                text = modelStatusText(state.modelStatus),
                color = modelStatusColor(state.modelStatus),
                style = MaterialTheme.typography.bodyMedium,
            )

            if (state.isModelReady && !state.isCommercialApproved) {
                CommercialBanner()
            }

            if (state.isMonitoring) {
                Text(
                    text = metricsLine(state),
                    color = Color.White.copy(alpha = 0.85f),
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                )
            }

            state.lastEvent?.let { event ->
                val pct = (event.confidence * 100).toInt().coerceIn(0, 100)
                Text(
                    text = "Last event: ${event.type.name.lowercase()} • signal $pct%",
                    color = Color(0xFFFFB74D),
                    style = MaterialTheme.typography.bodySmall,
                )
            }

            state.errorMessage?.let { msg ->
                Text(
                    text = msg,
                    color = Color(0xFFEF5350),
                    style = MaterialTheme.typography.bodySmall,
                )
            }
        }
    }
}

@Composable
private fun StatusDot(active: Boolean) {
    Box(
        modifier = Modifier
            .size(10.dp)
            .clip(CircleShape)
            .background(if (active) Color(0xFF4CAF50) else Color.Gray),
    )
}

@Composable
private fun OfflineChip() {
    Surface(color = Color(0xFF37474F), shape = RoundedCornerShape(8.dp)) {
        Text(
            text = "OFFLINE · on-device",
            color = Color(0xFFB0BEC5),
            style = MaterialTheme.typography.labelSmall,
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp),
        )
    }
}

@Composable
private fun CommercialBanner() {
    Surface(color = Color(0xFF8D6E00), shape = RoundedCornerShape(8.dp)) {
        Text(
            text = "EVALUATION ONLY — model not cleared for commercial release",
            color = Color.White,
            style = MaterialTheme.typography.labelSmall,
            fontWeight = FontWeight.SemiBold,
            modifier = Modifier.fillMaxWidth().padding(horizontal = 10.dp, vertical = 6.dp),
        )
    }
}

private fun metricsLine(state: MonitoringUiState): String {
    val m = state.metrics
    val avg = if (m.avgInferenceMs > 0) m.avgInferenceMs.toInt() else 0
    return "${m.lastInferenceMs}ms  avg ${avg}ms  ${m.processedFps}fps  " +
        "drop ${m.droppedFrames}  det ${state.activeDetections.size}"
}

private fun modelStatusText(status: ModelStatus): String = when (status) {
    is ModelStatus.NotProvisioned ->
        "Model not provisioned — detection disabled (add ${status.expectedAssetPath})"
    ModelStatus.Loading -> "Loading model…"
    is ModelStatus.Ready -> {
        val md = status.metadata
        "Model: ${md.name} • ${md.delegate} • load ${md.loadTimeMs}ms"
    }
    is ModelStatus.Error -> "Model error: ${status.message}"
}

private fun modelStatusColor(status: ModelStatus): Color = when (status) {
    is ModelStatus.Ready -> Color(0xFF81C784)
    is ModelStatus.Error -> Color(0xFFEF5350)
    is ModelStatus.NotProvisioned -> Color(0xFFFFB74D)
    ModelStatus.Loading -> Color.White.copy(alpha = 0.8f)
}

@Composable
private fun Controls(
    state: MonitoringUiState,
    onStart: () -> Unit,
    onStop: () -> Unit,
    onToggleSound: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier,
        verticalArrangement = Arrangement.spacedBy(12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Surface(color = Color.Black.copy(alpha = 0.5f), shape = RoundedCornerShape(12.dp)) {
            Row(
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text("Alarm sound", color = Color.White, style = MaterialTheme.typography.bodyMedium)
                Switch(checked = state.soundEnabled, onCheckedChange = { onToggleSound() })
            }
        }
        Button(
            onClick = if (state.isMonitoring) onStop else onStart,
            modifier = Modifier.fillMaxWidth(),
            colors = ButtonDefaults.buttonColors(
                containerColor = if (state.isMonitoring) Color(0xFFD32F2F) else Color(0xFF2E7D32),
            ),
            shape = RoundedCornerShape(16.dp),
        ) {
            // Icons.Filled.Stop is not in material-icons-core; a drawn square
            // avoids pulling in material-icons-extended for one glyph.
            if (state.isMonitoring) {
                Box(
                    modifier = Modifier
                        .size(18.dp)
                        .background(Color.White, RoundedCornerShape(3.dp)),
                )
            } else {
                Icon(imageVector = Icons.Filled.PlayArrow, contentDescription = null)
            }
            Text(
                text = if (state.isMonitoring) "  Stop Monitoring" else "  Start Monitoring",
                style = MaterialTheme.typography.titleLarge,
                modifier = Modifier.padding(vertical = 8.dp),
            )
        }
    }
}

@Composable
private fun PermissionRequest(onRequest: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier.padding(24.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = "Camera access is required to monitor the environment.",
            color = Color.White,
            style = MaterialTheme.typography.bodyLarge,
        )
        Button(onClick = onRequest) {
            Text("Grant camera access")
        }
    }
}
