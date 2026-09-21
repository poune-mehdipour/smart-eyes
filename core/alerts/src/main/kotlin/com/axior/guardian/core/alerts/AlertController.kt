package com.axior.guardian.core.alerts

import com.axior.guardian.core.model.SecurityEvent

/**
 * Fires a local, on-device alert for a *confirmed* [SecurityEvent].
 *
 * The contract is deliberately narrow: it acts only on confirmed events (never
 * on raw per-frame detections), and it never touches the network. Implementations
 * own their own Android resources and must be released via [release].
 */
interface AlertController {

    /** Whether the optional audible tone plays on an alert. Toggleable at runtime. */
    var soundEnabled: Boolean

    /**
     * Raise the local alert for one confirmed event: vibrate, post a
     * notification, and (if [soundEnabled]) play a short tone. Must be safe to
     * call repeatedly and must not throw on a device missing a given capability.
     */
    fun onConfirmedEvent(event: SecurityEvent)

    /** Release any held Android resources (tone generator, etc.). */
    fun release()
}
