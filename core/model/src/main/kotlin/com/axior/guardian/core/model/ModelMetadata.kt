package com.axior.guardian.core.model

/**
 * Runtime facts about the model that is actually loaded, surfaced to the UI so
 * the operator can see what is running (and whether it is commercially cleared).
 * Contains only measured / read values — nothing fabricated.
 */
data class ModelMetadata(
    val name: String,
    val version: String,
    val license: String,
    val commercialUse: CommercialUse,
    val classNames: List<String>,
    val inputShape: List<Int>,
    val outputShape: List<Int>,
    /** Wall-clock time to construct the interpreter + warm up, in ms. */
    val loadTimeMs: Long,
    /** Which compute path the interpreter actually bound to. */
    val delegate: Delegate,
) {
    val commercialApproved: Boolean get() = commercialUse == CommercialUse.APPROVED
}

enum class Delegate { CPU, GPU, NNAPI }
