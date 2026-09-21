package com.axior.guardian.detector.firesmoke

import com.axior.guardian.core.model.Delegate
import com.axior.guardian.core.model.ModelSpec
import org.tensorflow.lite.DataType
import org.tensorflow.lite.Interpreter
import org.tensorflow.lite.gpu.CompatibilityList
import org.tensorflow.lite.gpu.GpuDelegate
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.MappedByteBuffer

/** The decoded output of one inference, ready for [com.axior.guardian.core.detection.YoloV8PostProcessor]. */
class RawOutput(
    /** Flattened channels-first [4+C, N] scores, regardless of the model's native layout. */
    val data: FloatArray,
    val numChannels: Int,
    val numAnchors: Int,
)

/**
 * Thin, honest wrapper around a TensorFlow Lite [Interpreter] for a YOLOv8
 * detect model.
 *
 * Responsibilities: build the interpreter once (preferring a GPU delegate,
 * falling back to multi-threaded CPU), verify the real tensor shapes against the
 * declared [ModelSpec], run inference on a reused input buffer, and normalise
 * the output into channels-first order for the pure decoder. Nothing here is
 * fabricated: shapes and the bound delegate are read back from the interpreter.
 */
class TfLiteFireSmokeModel private constructor(
    private val interpreter: Interpreter,
    private val gpuDelegate: GpuDelegate?,
    val boundDelegate: Delegate,
    private val spec: ModelSpec,
    val inputShape: IntArray,
    val outputShape: IntArray,
    private val outputChannelsFirst: Boolean,
    private val numChannels: Int,
    private val numAnchors: Int,
) {
    private val inputBuffer: ByteBuffer =
        ByteBuffer.allocateDirect(4 * spec.inputWidth * spec.inputHeight * spec.inputChannels)
            .order(ByteOrder.nativeOrder())

    private val outputBuffer: ByteBuffer =
        ByteBuffer.allocateDirect(4 * numChannels * numAnchors).order(ByteOrder.nativeOrder())

    private val flatOutput = FloatArray(numChannels * numAnchors)

    @Volatile var lastInferenceMs: Long = 0L
        private set

    /**
     * Run one inference. [input] is the preprocessed tensor in the spec's layout
     * (NHWC float32 in the MVP). Returns channels-first raw scores.
     */
    fun run(input: FloatArray): RawOutput {
        inputBuffer.rewind()
        val fb = inputBuffer.asFloatBuffer()
        fb.put(input)
        inputBuffer.rewind()
        outputBuffer.rewind()

        val start = System.nanoTime()
        interpreter.run(inputBuffer, outputBuffer)
        lastInferenceMs = (System.nanoTime() - start) / 1_000_000L

        outputBuffer.rewind()
        outputBuffer.asFloatBuffer().get(flatOutput)

        val data = if (outputChannelsFirst) {
            flatOutput
        } else {
            transposeToChannelsFirst(flatOutput, numAnchors, numChannels)
        }
        return RawOutput(data, numChannels, numAnchors)
    }

    fun close() {
        runCatching { interpreter.close() }
        runCatching { gpuDelegate?.close() }
    }

    companion object {

        /**
         * Build the interpreter and validate it against [spec].
         *
         * @throws ModelLoadException with an actionable message if the tensor
         *   shapes or datatypes do not match the declared spec — never silently
         *   proceeds with an incompatible model.
         */
        fun load(modelBuffer: MappedByteBuffer, spec: ModelSpec): TfLiteFireSmokeModel {
            var gpuDelegate: GpuDelegate? = null
            var delegate = Delegate.CPU

            val options = Interpreter.Options()
            val compat = runCatching { CompatibilityList() }.getOrNull()
            if (compat?.isDelegateSupportedOnThisDevice == true) {
                runCatching {
                    val d = GpuDelegate(compat.bestOptionsForThisDevice)
                    options.addDelegate(d)
                    gpuDelegate = d
                    delegate = Delegate.GPU
                }.onFailure {
                    gpuDelegate = null
                    delegate = Delegate.CPU
                }
            }
            if (delegate == Delegate.CPU) {
                options.numThreads = Runtime.getRuntime().availableProcessors().coerceIn(1, 4)
            }

            val interpreter = runCatching { Interpreter(modelBuffer, options) }
                .getOrElse { e ->
                    gpuDelegate?.close()
                    // A GPU delegate can fail at construction for this specific
                    // graph; retry once on plain CPU before giving up.
                    if (delegate == Delegate.GPU) {
                        gpuDelegate = null
                        delegate = Delegate.CPU
                        val cpuOptions = Interpreter.Options().apply {
                            numThreads = Runtime.getRuntime().availableProcessors().coerceIn(1, 4)
                        }
                        runCatching { Interpreter(modelBuffer, cpuOptions) }.getOrElse { inner ->
                            throw ModelLoadException("Failed to construct interpreter: ${inner.message}")
                        }
                    } else {
                        throw ModelLoadException("Failed to construct interpreter: ${e.message}")
                    }
                }

            val inputTensor = interpreter.getInputTensor(0)
            val outputTensor = interpreter.getOutputTensor(0)
            val inShape = inputTensor.shape()
            val outShape = outputTensor.shape()

            // --- validate input ---
            if (!spec.inputQuantized && inputTensor.dataType() != DataType.FLOAT32) {
                interpreter.close(); gpuDelegate?.close()
                throw ModelLoadException(
                    "Model input is ${inputTensor.dataType()} but spec declares float32. " +
                        "Set inputQuantized in the spec sidecar, or export a float model.",
                )
            }
            val expectedInputElems = spec.inputWidth * spec.inputHeight * spec.inputChannels
            val actualInputElems = inShape.fold(1) { a, b -> a * b }
            if (actualInputElems != expectedInputElems) {
                interpreter.close(); gpuDelegate?.close()
                throw ModelLoadException(
                    "Model input shape ${inShape.toList()} has $actualInputElems elements, " +
                        "spec expects $expectedInputElems (${spec.inputWidth}x${spec.inputHeight}x${spec.inputChannels}).",
                )
            }

            // --- validate + orient output ---
            if (outputTensor.dataType() != DataType.FLOAT32) {
                interpreter.close(); gpuDelegate?.close()
                throw ModelLoadException(
                    "Model output is ${outputTensor.dataType()}, not float32. This MVP decodes " +
                        "float32 YOLOv8 output; re-export without full int8 output quantisation.",
                )
            }
            if (outShape.size != 3 || outShape[0] != 1) {
                interpreter.close(); gpuDelegate?.close()
                throw ModelLoadException(
                    "Unexpected output rank ${outShape.toList()}; expected [1, C, N] or [1, N, C].",
                )
            }
            val expectedChannels = 4 + spec.classCount
            val channelsFirst: Boolean
            val channels: Int
            val anchors: Int
            when (expectedChannels) {
                outShape[1] -> { channelsFirst = true; channels = outShape[1]; anchors = outShape[2] }
                outShape[2] -> { channelsFirst = false; channels = outShape[2]; anchors = outShape[1] }
                else -> {
                    interpreter.close(); gpuDelegate?.close()
                    throw ModelLoadException(
                        "Output ${outShape.toList()} has neither dim == $expectedChannels " +
                            "(4 box + ${spec.classCount} classes). Wrong model or wrong class count.",
                    )
                }
            }

            return TfLiteFireSmokeModel(
                interpreter = interpreter,
                gpuDelegate = gpuDelegate,
                boundDelegate = delegate,
                spec = spec,
                inputShape = inShape,
                outputShape = outShape,
                outputChannelsFirst = channelsFirst,
                numChannels = channels,
                numAnchors = anchors,
            )
        }

        /** [n, c] row-major -> [c, n] row-major. */
        private fun transposeToChannelsFirst(src: FloatArray, n: Int, c: Int): FloatArray {
            val dst = FloatArray(src.size)
            for (a in 0 until n) {
                val base = a * c
                for (ch in 0 until c) {
                    dst[ch * n + a] = src[base + ch]
                }
            }
            return dst
        }
    }
}

/** Model present but unusable; carries an operator-actionable message. */
class ModelLoadException(message: String) : Exception(message)
