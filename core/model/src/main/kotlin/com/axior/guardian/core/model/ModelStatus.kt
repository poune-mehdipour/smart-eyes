package com.axior.guardian.core.model

/**
 * Lifecycle of the detection model, so the UI can show an honest state instead
 * of pretending to detect. There is deliberately no "mock ready" state.
 */
sealed interface ModelStatus {
    /** No model asset present; detection is disabled until one is provisioned. */
    data class NotProvisioned(val expectedAssetPath: String) : ModelStatus

    /** Interpreter is being constructed. */
    data object Loading : ModelStatus

    /** Model loaded and running. */
    data class Ready(val metadata: ModelMetadata) : ModelStatus

    /** Model present but failed to load or run. Carries an actionable message. */
    data class Error(val message: String) : ModelStatus
}
