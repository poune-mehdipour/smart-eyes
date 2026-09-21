package com.axior.guardian.core.alerts

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.pm.PackageManager
import android.media.AudioManager
import android.media.ToneGenerator
import android.os.Build
import android.os.VibrationEffect
import android.os.Vibrator
import android.os.VibratorManager
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import com.axior.guardian.core.model.DetectorType
import com.axior.guardian.core.model.SecurityEvent

/**
 * Android implementation of [AlertController].
 *
 * Uses only local capabilities — vibration, a notification, and a synthesised
 * [ToneGenerator] beep (so no audio file has to ship). Every capability is
 * guarded: a missing vibrator, denied notification permission, or an
 * unavailable tone generator degrades silently rather than crashing monitoring.
 */
class AndroidAlertController(
    private val context: Context,
    override var soundEnabled: Boolean = true,
) : AlertController {

    private val appContext = context.applicationContext

    private val notificationManager =
        appContext.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    private val vibrator: Vibrator? by lazy { resolveVibrator() }

    // Lazily created; some emulators/devices throw if the stream is unavailable.
    private var toneGenerator: ToneGenerator? = null

    init {
        createChannel()
    }

    override fun onConfirmedEvent(event: SecurityEvent) {
        vibrate()
        notify(event)
        if (soundEnabled) beep()
    }

    private fun vibrate() {
        val v = vibrator ?: return
        if (!v.hasVibrator()) return
        runCatching {
            val effect = VibrationEffect.createWaveform(VIBRATION_PATTERN, -1)
            v.vibrate(effect)
        }
    }

    private fun notify(event: SecurityEvent) {
        // POST_NOTIFICATIONS is a runtime permission from API 33; respect it.
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            val granted = ContextCompat.checkSelfPermission(
                appContext, Manifest.permission.POST_NOTIFICATIONS,
            ) == PackageManager.PERMISSION_GRANTED
            if (!granted) return
        }

        val title = "${label(event.type)} detected"
        val pct = (event.confidence * 100).toInt().coerceIn(0, 100)
        val text = "Confirmed on-device • signal strength $pct%"

        val notification = NotificationCompat.Builder(appContext, CHANNEL_ID)
            .setContentTitle(title)
            .setContentText(text)
            .setSmallIcon(android.R.drawable.stat_notify_error)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_ALARM)
            .setAutoCancel(true)
            .build()

        runCatching {
            NotificationManagerCompat.from(appContext)
                .notify(event.id.hashCode(), notification)
        }
    }

    private fun beep() {
        runCatching {
            val gen = toneGenerator ?: ToneGenerator(
                AudioManager.STREAM_ALARM, TONE_VOLUME,
            ).also { toneGenerator = it }
            gen.startTone(ToneGenerator.TONE_CDMA_ALERT_CALL_GUARD, TONE_DURATION_MS)
        }
    }

    private fun createChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            "Hazard alerts",
            NotificationManager.IMPORTANCE_HIGH,
        ).apply {
            description = "Local alerts for confirmed on-device hazard detections."
            enableVibration(false) // vibration is driven explicitly above
        }
        notificationManager.createNotificationChannel(channel)
    }

    private fun resolveVibrator(): Vibrator? = runCatching {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            val manager = appContext.getSystemService(Context.VIBRATOR_MANAGER_SERVICE)
                as? VibratorManager
            manager?.defaultVibrator
        } else {
            @Suppress("DEPRECATION")
            appContext.getSystemService(Context.VIBRATOR_SERVICE) as? Vibrator
        }
    }.getOrNull()

    private fun label(type: DetectorType): String = when (type) {
        DetectorType.FIRE -> "Fire"
        DetectorType.SMOKE -> "Smoke"
        DetectorType.FALL -> "Fall"
        DetectorType.INTRUSION -> "Intrusion"
        DetectorType.CAMERA_TAMPERING -> "Camera tampering"
    }

    override fun release() {
        runCatching { toneGenerator?.release() }
        toneGenerator = null
    }

    private companion object {
        const val CHANNEL_ID = "guardian_hazard_alerts"
        const val TONE_VOLUME = 90
        const val TONE_DURATION_MS = 800
        val VIBRATION_PATTERN = longArrayOf(0, 400, 200, 400)
    }
}
