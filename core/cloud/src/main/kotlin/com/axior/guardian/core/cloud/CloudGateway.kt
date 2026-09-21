package com.axior.guardian.core.cloud

import java.io.IOException
import java.net.HttpURLConnection
import java.net.URI

/** Outcome of one delivery attempt, classified for the retry loop. */
sealed interface GatewayResult {
    /** 2xx — accepted (a duplicate re-delivery is also success). */
    data object Accepted : GatewayResult

    /** Timeouts, connect failures, 5xx, 429: retrying can help. */
    data class TransientError(val detail: String) : GatewayResult

    /** 4xx: the payload or credentials are wrong; retrying cannot help. */
    data class PermanentError(val detail: String) : GatewayResult
}

/**
 * Transport seam for cloud delivery. [QueueingCloudReporter] talks to this;
 * tests substitute a fake, production uses [HttpCloudGateway].
 */
interface CloudGateway {
    fun postEvent(json: String): GatewayResult
    fun postTelemetry(json: String): GatewayResult
}

/**
 * HttpURLConnection-based gateway. HttpURLConnection is chosen because it
 * exists identically on Android and the JVM — this module stays pure-JVM and
 * testable, and the app gains no HTTP client dependency.
 *
 * Blocking by design: callers ([QueueingCloudReporter]) invoke it from a
 * dedicated background dispatcher, never from the frame path.
 */
class HttpCloudGateway(
    private val config: CloudConfig,
    private val connectTimeoutMs: Int = 5_000,
    private val readTimeoutMs: Int = 10_000,
) : CloudGateway {

    override fun postEvent(json: String): GatewayResult =
        post("${config.baseUrl}/v1/edge/events", json)

    override fun postTelemetry(json: String): GatewayResult =
        post("${config.baseUrl}/v1/edge/telemetry", json)

    private fun post(url: String, body: String): GatewayResult {
        return try {
            val conn = URI(url).toURL().openConnection() as HttpURLConnection
            try {
                conn.requestMethod = "POST"
                conn.connectTimeout = connectTimeoutMs
                conn.readTimeout = readTimeoutMs
                conn.doOutput = true
                conn.setRequestProperty("Content-Type", "application/json")
                if (config.apiToken.isNotEmpty()) {
                    conn.setRequestProperty("Authorization", "Bearer ${config.apiToken}")
                }
                conn.outputStream.use { it.write(body.toByteArray(Charsets.UTF_8)) }
                when (val code = conn.responseCode) {
                    in 200..299 -> GatewayResult.Accepted
                    429 -> GatewayResult.TransientError("HTTP 429")
                    in 400..499 -> GatewayResult.PermanentError("HTTP $code")
                    else -> GatewayResult.TransientError("HTTP $code")
                }
            } finally {
                conn.disconnect()
            }
        } catch (e: IOException) {
            GatewayResult.TransientError(e.message ?: e.javaClass.simpleName)
        } catch (e: Exception) {
            // Malformed URL and friends: configuration problems, not network ones.
            GatewayResult.PermanentError(e.message ?: e.javaClass.simpleName)
        }
    }
}
