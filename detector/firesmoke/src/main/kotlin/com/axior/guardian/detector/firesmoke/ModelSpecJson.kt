package com.axior.guardian.detector.firesmoke

import com.axior.guardian.core.model.CommercialUse
import com.axior.guardian.core.model.DetectorType
import com.axior.guardian.core.model.InputLayout
import com.axior.guardian.core.model.ModelSpec
import com.axior.guardian.core.model.Normalization
import com.axior.guardian.core.model.OutputFormat
import org.json.JSONObject

/**
 * Where the provisioned model and its spec sidecar live inside the APK assets.
 * The sidecar (a tiny JSON file) is committed to the repo; the weights file is
 * provisioned separately — see assets/models/PROVISIONING.md.
 */
object ModelAssets {
    const val MODEL_PATH = "models/fire_smoke.tflite"
    const val SPEC_PATH = "models/fire_smoke.json"
}

/**
 * Parses the model spec sidecar JSON into a [ModelSpec]. Pure and
 * Android-independent apart from [org.json], so the exact same fields that drive
 * preprocessing and decoding are declared in data, not code — a different model
 * is swapped in by editing this JSON, never the Kotlin.
 *
 * Unknown or missing optional fields fall back to the documented YOLOv8 TFLite
 * defaults; the required fields (name, classes) throw if absent so a malformed
 * sidecar fails loudly rather than mis-decoding silently.
 */
object ModelSpecJson {

    fun parse(json: String): ModelSpec {
        val o = JSONObject(json)

        val classNames = o.getJSONArray("classNames").let { arr ->
            List(arr.length()) { arr.getString(it) }
        }
        require(classNames.isNotEmpty()) { "classNames must not be empty" }

        val mapObj = o.getJSONObject("classToDetector")
        val classToDetector = mapObj.keys().asSequence().associateWith { key ->
            DetectorType.valueOf(mapObj.getString(key).uppercase())
        }.mapKeys { it.key.lowercase() }

        return ModelSpec(
            name = o.getString("name"),
            version = o.optString("version", "unknown"),
            source = o.optString("source", "unknown"),
            license = o.optString("license", "unknown"),
            commercialUse = o.optString("commercialUse", "UNRESOLVED")
                .let { runCatching { CommercialUse.valueOf(it.uppercase()) }
                    .getOrDefault(CommercialUse.UNRESOLVED) },
            inputWidth = o.optInt("inputWidth", 640),
            inputHeight = o.optInt("inputHeight", 640),
            inputChannels = o.optInt("inputChannels", 3),
            inputLayout = o.optString("inputLayout", "NHWC")
                .let { InputLayout.valueOf(it.uppercase()) },
            normalization = o.optString("normalization", "SCALE_0_1")
                .let { Normalization.valueOf(it.uppercase()) },
            inputRgb = o.optBoolean("inputRgb", true),
            inputQuantized = o.optBoolean("inputQuantized", false),
            inputQuantScale = o.optDouble("inputQuantScale", 1.0).toFloat(),
            inputQuantZeroPoint = o.optInt("inputQuantZeroPoint", 0),
            outputFormat = o.optString("outputFormat", "YOLOV8_DETECT")
                .let { OutputFormat.valueOf(it.uppercase()) },
            classNames = classNames,
            classToDetector = classToDetector,
            coordinatesNormalized = o.optBoolean("coordinatesNormalized", true),
        )
    }
}
