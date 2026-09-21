package com.axior.guardian.core.cloud

/**
 * Cloud reporting configuration. [enabled] false (the default, and the only
 * option in the `offline` product flavor) turns the whole cloud path into a
 * no-op: detection, confirmation and local alerting run exactly as before.
 *
 * Values come from BuildConfig / Gradle properties, never from source:
 * see app/build.gradle.kts (`connected` flavor).
 */
data class CloudConfig(
    val enabled: Boolean = false,
    /** Base URL of the Cloud Connector, e.g. https://connector.example.com */
    val baseUrl: String = "",
    /** Bearer token for the edge ingest API. */
    val apiToken: String = "",
    /**
     * Stable, non-personal device identifier the connector keys this device
     * by. An install-scoped random UUID; deliberately not a hardware ID.
     */
    val deviceId: String = "",
    val appVersion: String = "",
)
