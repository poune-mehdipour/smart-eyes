package com.axior.guardian.detector.firesmoke

import android.content.Context
import com.axior.guardian.core.detection.DetectionConfig
import com.axior.guardian.core.detection.FramePreprocessor
import com.axior.guardian.core.detection.YoloV8PostProcessor
import com.axior.guardian.core.model.AnalysisFrame
import com.axior.guardian.core.model.Detection
import com.axior.guardian.core.model.DetectorType
import com.axior.guardian.core.model.ModelMetadata
import com.axior.guardian.core.model.ModelSpec
import com.axior.guardian.core.model.ModelStatus
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.io.FileInputStream
import java.nio.channels.FileChannel

/**
 * The MVP's one real detector: offline fire/smoke detection via a TensorFlow
 * Lite YOLOv8 model loaded from APK assets.
 *
 * Honesty guarantees baked in:
 *  - If the weights asset is absent, [modelStatus] becomes
 *    [ModelStatus.NotProvisioned] and [detect] returns an EMPTY list forever —
 *    there is no mock path that fabricates detections.
 *  - If the model is present but incompatible, [modelStatus] becomes
 *    [ModelStatus.Error] with an actionable message.
 *  - A per-frame inference failure is swallowed for that frame only, so one bad
 *    frame never terminates monitoring.
 *
 * All heavy objects (interpreter, preprocessor, buffers) are created once in
 * [initialize] and reused; [detect] allocates almost nothing per frame.
 */
class FireSmokeDetector(
    private val context: Context,
    private val config: DetectionConfig,
) : com.axior.guardian.core.detection.Detector {

    override val type: DetectorType = DetectorType.FIRE

    /** A single combined model emits both classes, so it serves two hazard types. */
    override val types: Set<DetectorType> = setOf(DetectorType.FIRE, DetectorType.SMOKE)

    private val _modelStatus =
        MutableStateFlow<ModelStatus>(ModelStatus.NotProvisioned(ModelAssets.MODEL_PATH))
    override val modelStatus: StateFlow<ModelStatus> = _modelStatus.asStateFlow()

    private var model: TfLiteFireSmokeModel? = null
    private var preprocessor: FramePreprocessor? = null
    private var postProcessor: YoloV8PostProcessor? = null
    private var metadata: ModelMetadata? = null
    private var inputScratch: FloatArray? = null

    override suspend fun initialize() {
        if (!assetExists(ModelAssets.MODEL_PATH)) {
            _modelStatus.value = ModelStatus.NotProvisioned(ModelAssets.MODEL_PATH)
            return
        }
        _modelStatus.value = ModelStatus.Loading
        try {
            val specJson = readAssetText(ModelAssets.SPEC_PATH)
                ?: throw ModelLoadException(
                    "Weights present but spec sidecar '${ModelAssets.SPEC_PATH}' is missing. " +
                        "Add it (see PROVISIONING.md) so the pipeline knows how to decode the model.",
                )
            val spec: ModelSpec = ModelSpecJson.parse(specJson)

            val start = System.currentTimeMillis()
            val buffer = mapAsset(ModelAssets.MODEL_PATH)
            val loaded = TfLiteFireSmokeModel.load(buffer, spec)
            val loadMs = System.currentTimeMillis() - start

            val meta = ModelMetadata(
                name = spec.name,
                version = spec.version,
                license = spec.license,
                commercialUse = spec.commercialUse,
                classNames = spec.classNames,
                inputShape = loaded.inputShape.toList(),
                outputShape = loaded.outputShape.toList(),
                loadTimeMs = loadMs,
                delegate = loaded.boundDelegate,
            )

            model = loaded
            preprocessor = FramePreprocessor(spec)
            postProcessor = YoloV8PostProcessor(spec, config)
            inputScratch = FloatArray(spec.inputWidth * spec.inputHeight * spec.inputChannels)
            metadata = meta
            _modelStatus.value = ModelStatus.Ready(meta)
        } catch (e: ModelLoadException) {
            _modelStatus.value = ModelStatus.Error(e.message ?: "Model failed to load")
        } catch (e: Exception) {
            _modelStatus.value = ModelStatus.Error("Model failed to load: ${e.message}")
        }
    }

    override suspend fun detect(frame: AnalysisFrame): List<Detection> {
        val m = model ?: return emptyList()
        val pre = preprocessor ?: return emptyList()
        val post = postProcessor ?: return emptyList()
        return try {
            val processed = pre.process(frame, inputScratch)
            val raw = m.run(processed.data)
            post.process(
                raw = raw.data,
                numChannels = raw.numChannels,
                numAnchors = raw.numAnchors,
                transform = processed.transform,
                frameTimestampMs = frame.timestampMs,
            )
        } catch (e: Exception) {
            // One failed frame must never terminate monitoring. Drop it and keep going.
            emptyList()
        }
    }

    override fun metadata(): ModelMetadata? = metadata

    override fun close() {
        runCatching { model?.close() }
        model = null
    }

    // --- asset helpers ---

    private fun assetExists(path: String): Boolean = runCatching {
        val dir = path.substringBeforeLast('/', "")
        val name = path.substringAfterLast('/')
        context.assets.list(dir)?.contains(name) == true
    }.getOrDefault(false)

    private fun readAssetText(path: String): String? = runCatching {
        context.assets.open(path).bufferedReader().use { it.readText() }
    }.getOrNull()

    private fun mapAsset(path: String): java.nio.MappedByteBuffer {
        val afd = context.assets.openFd(path)
        FileInputStream(afd.fileDescriptor).use { fis ->
            return fis.channel.map(
                FileChannel.MapMode.READ_ONLY,
                afd.startOffset,
                afd.declaredLength,
            )
        }
    }
}
