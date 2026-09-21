#!/usr/bin/env python3
"""
Inspect a TensorFlow Lite model and print the exact facts the Guardian pipeline
needs, so detector/firesmoke/src/main/assets/models/fire_smoke.json can be
verified against the ACTUAL provisioned file rather than assumed.

Usage:
    python scripts/inspect_tflite_model.py path/to/fire_smoke.tflite

Requires one of: tflite-runtime, tensorflow, or ai-edge-litert.
    pip install tflite-runtime      # smallest
    # or: pip install tensorflow
"""
import sys
import json


def load_interpreter(model_path):
    try:
        from tflite_runtime.interpreter import Interpreter
        return Interpreter(model_path=model_path)
    except ImportError:
        pass
    try:
        import tensorflow as tf
        return tf.lite.Interpreter(model_path=model_path)
    except ImportError:
        pass
    try:
        from ai_edge_litert.interpreter import Interpreter
        return Interpreter(model_path=model_path)
    except ImportError:
        sys.exit("No TFLite runtime found. pip install tflite-runtime (or tensorflow).")


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    model_path = sys.argv[1]

    interp = load_interpreter(model_path)
    interp.allocate_tensors()
    inp = interp.get_input_details()[0]
    out = interp.get_output_details()[0]

    in_shape = list(map(int, inp["shape"]))
    out_shape = list(map(int, out["shape"]))
    in_dtype = str(inp["dtype"].__name__)
    out_dtype = str(out["dtype"].__name__)

    print("=" * 60)
    print("INPUT")
    print(f"  shape : {in_shape}")
    print(f"  dtype : {in_dtype}")
    print(f"  quant : {inp['quantization']}")
    print("OUTPUT")
    print(f"  shape : {out_shape}")
    print(f"  dtype : {out_dtype}")
    print(f"  quant : {out['quantization']}")
    print("=" * 60)

    # --- interpret for the sidecar ---
    layout = "NHWC" if len(in_shape) == 4 and in_shape[-1] in (1, 3) else "NCHW"
    if layout == "NHWC":
        h, w = in_shape[1], in_shape[2]
    else:
        h, w = in_shape[2], in_shape[3]

    notes = []
    if in_dtype != "float32":
        notes.append("Input is NOT float32 -> set inputQuantized:true (this MVP expects float32).")
    if out_dtype != "float32":
        notes.append("Output is NOT float32 -> the app will refuse it; re-export a float32 model.")

    # channels-first [1, 4+C, N] vs anchors-first [1, N, 4+C]
    orientation = "unknown"
    if len(out_shape) == 3:
        d1, d2 = out_shape[1], out_shape[2]
        if d1 < d2:
            orientation = f"channels-first [1, {d1}, {d2}]  (C={d1}, N={d2})"
            classes = d1 - 4
        else:
            orientation = f"anchors-first [1, {d1}, {d2}]  (N={d1}, C={d2})"
            classes = d2 - 4
        notes.append(f"Detected {orientation}; implies classCount = {classes}. "
                     "Confirm classNames has exactly that many entries.")

    suggested = {
        "inputWidth": w,
        "inputHeight": h,
        "inputLayout": layout,
        "inputChannels": in_shape[-1] if layout == "NHWC" else in_shape[1],
        "inputQuantized": in_dtype != "float32",
        "note_coordinatesNormalized": (
            "Cannot be read from tensors. Run one real image; if decoded boxes look "
            "~1000x too large, coords are pixel-space -> set coordinatesNormalized:false."
        ),
    }
    print("SUGGESTED sidecar fields (verify the rest by eye):")
    print(json.dumps(suggested, indent=2))
    print("-" * 60)
    for n in notes:
        print("NOTE:", n)


if __name__ == "__main__":
    main()
