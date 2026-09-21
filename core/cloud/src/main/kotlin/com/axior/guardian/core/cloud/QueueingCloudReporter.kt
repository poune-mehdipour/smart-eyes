package com.axior.guardian.core.cloud

import com.axior.guardian.core.model.InferenceMetrics
import com.axior.guardian.core.model.SecurityEvent
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlin.random.Random

/**
 * The production [CloudReporter]: a bounded in-memory queue drained by one
 * background coroutine, with exponential backoff + jitter per item.
 *
 * Reliability model (deliberately modest, and honest about it):
 *  - `report*` never blocks and never throws. When the queue is full the
 *    OLDEST entry is dropped: under a long outage the freshest data wins.
 *  - Events are retried up to [maxAttempts]; telemetry is retried once at
 *    most — it is periodic, so the next snapshot supersedes a lost one.
 *  - Permanent (4xx) failures are dropped immediately: the payload will
 *    never get better by resending it.
 *  - The queue is in-memory only. If the process dies before delivery the
 *    events are lost — the connector's own idempotency makes redelivery
 *    safe, but this MVP does not persist an outbox. Documented limitation,
 *    not an accident. Local alerting has already happened by the time
 *    anything reaches this class, so no safety function is at risk.
 */
class QueueingCloudReporter(
    private val config: CloudConfig,
    private val gateway: CloudGateway,
    dispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val maxAttempts: Int = 5,
    private val baseBackoffMs: Long = 500,
    private val maxBackoffMs: Long = 30_000,
    queueCapacity: Int = 64,
    /** Test hook: no real sleeping inside unit tests. */
    private val sleep: suspend (Long) -> Unit = { delay(it) },
) : CloudReporter {

    private sealed interface Item {
        val json: String
        data class Event(override val json: String) : Item
        data class Telemetry(override val json: String) : Item
    }

    private val scope = CoroutineScope(SupervisorJob() + dispatcher)
    private val queue = Channel<Item>(capacity = queueCapacity, onBufferOverflow = BufferOverflow.DROP_OLDEST)
    private val worker: Job = scope.launch { drain() }

    override fun reportEvent(event: SecurityEvent) {
        if (!config.enabled) return
        queue.trySend(Item.Event(CloudContract.eventJson(config.deviceId, config.appVersion, event)))
    }

    override fun reportTelemetry(monitoring: Boolean, modelStatus: String, metrics: InferenceMetrics) {
        if (!config.enabled) return
        val json = CloudContract.telemetryJson(
            deviceId = config.deviceId,
            appVersion = config.appVersion,
            reportedAtMs = System.currentTimeMillis(),
            monitoring = monitoring,
            modelStatus = modelStatus,
            metrics = metrics,
        )
        queue.trySend(Item.Telemetry(json))
    }

    private suspend fun drain() {
        for (item in queue) {
            val attempts = if (item is Item.Event) maxAttempts else 2
            deliver(item, attempts)
        }
    }

    private suspend fun deliver(item: Item, maxAttempts: Int) {
        var attempt = 0
        while (true) {
            val result = when (item) {
                is Item.Event -> gateway.postEvent(item.json)
                is Item.Telemetry -> gateway.postTelemetry(item.json)
            }
            when (result) {
                is GatewayResult.Accepted -> return
                is GatewayResult.PermanentError -> return // resending cannot fix a 4xx
                is GatewayResult.TransientError -> {
                    attempt++
                    if (attempt >= maxAttempts) return // give up; connector idempotency covers any earlier partial success
                    sleep(backoffMs(attempt))
                }
            }
        }
    }

    /** Full-jitter exponential backoff: uniform in [0, min(base·2^attempt, max)]. */
    internal fun backoffMs(attempt: Int): Long {
        val ceiling = (baseBackoffMs shl (attempt - 1).coerceAtMost(20)).coerceAtMost(maxBackoffMs)
        return Random.nextLong(0, ceiling + 1)
    }

    override fun close() {
        queue.close()
        worker.cancel()
        scope.cancel()
    }
}
