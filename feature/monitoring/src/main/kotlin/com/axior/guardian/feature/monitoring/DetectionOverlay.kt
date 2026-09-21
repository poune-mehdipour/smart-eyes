package com.axior.guardian.feature.monitoring

import androidx.compose.foundation.Canvas
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextMeasurer
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.runtime.Composable
import androidx.compose.ui.unit.dp
import com.axior.guardian.core.detection.ViewportMapper
import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.DetectorType

/**
 * Draws bounding boxes over the live preview. Coordinate mapping is delegated to
 * the pure, unit-tested [ViewportMapper] so alignment with the FILL_CENTER
 * preview is verified in tests, not eyeballed. Labels show the class and the raw
 * model score (an internal health signal, not a calibrated probability).
 */
@Composable
fun DetectionOverlay(
    detections: List<Detection>,
    frameAspectRatio: Float,
    modifier: Modifier = Modifier,
) {
    val textMeasurer = rememberTextMeasurer()
    Canvas(modifier = modifier) {
        if (frameAspectRatio <= 0f) return@Canvas
        for (d in detections) {
            val box = d.boundingBox ?: continue
            val rect = ViewportMapper.mapBox(
                box = box,
                frameAspectRatio = frameAspectRatio,
                viewWidth = size.width,
                viewHeight = size.height,
            )
            if (rect.width <= 0f || rect.height <= 0f) continue

            val color = colorFor(d.type)
            drawRect(
                color = color,
                topLeft = Offset(rect.left, rect.top),
                size = Size(rect.width, rect.height),
                style = Stroke(width = 4.dp.toPx()),
            )
            drawLabel(textMeasurer, d, color, rect.left, rect.top)
        }
    }
}

private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawLabel(
    measurer: TextMeasurer,
    detection: Detection,
    color: Color,
    x: Float,
    y: Float,
) {
    val pct = (detection.confidence * 100).toInt().coerceIn(0, 100)
    val label = "${labelFor(detection.type)} $pct%"
    val measured = measurer.measure(AnnotatedString(label))
    val padding = 6.dp.toPx()
    val bgHeight = measured.size.height + padding
    val bgTop = (y - bgHeight).coerceAtLeast(0f)
    drawRect(
        color = color.copy(alpha = 0.85f),
        topLeft = Offset(x, bgTop),
        size = Size(measured.size.width + padding * 2, bgHeight),
    )
    // The (TextMeasurer, String) drawText overload has no `color` parameter;
    // the label colour is routed through TextStyle instead.
    drawText(
        textMeasurer = measurer,
        text = label,
        topLeft = Offset(x + padding, bgTop + padding / 2),
        style = TextStyle(color = Color.White),
    )
}

private fun colorFor(type: DetectorType): Color = when (type) {
    DetectorType.FIRE -> Color(0xFFFF5722)
    DetectorType.SMOKE -> Color(0xFF9E9E9E)
    DetectorType.FALL -> Color(0xFFFFC107)
    DetectorType.INTRUSION -> Color(0xFF2196F3)
    DetectorType.CAMERA_TAMPERING -> Color(0xFFAB47BC)
}

private fun labelFor(type: DetectorType): String = when (type) {
    DetectorType.FIRE -> "Fire"
    DetectorType.SMOKE -> "Smoke"
    DetectorType.FALL -> "Fall"
    DetectorType.INTRUSION -> "Intrusion"
    DetectorType.CAMERA_TAMPERING -> "Tamper"
}
