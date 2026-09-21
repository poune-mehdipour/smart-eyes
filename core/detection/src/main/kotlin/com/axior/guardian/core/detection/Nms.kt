package com.axior.guardian.core.detection

/**
 * Greedy per-class Non-Maximum Suppression. Detectors that already emit one box
 * per object (YOLOv10 / YOLO26 / DETR) skip this; the YOLOv8 head does not, so
 * NMS is a required post-processing step for it.
 */
object Nms {

    /** Intersection-over-Union of two boxes in the same coordinate space. */
    fun iou(a: PixelBox, b: PixelBox): Float {
        val interLeft = maxOf(a.left, b.left)
        val interTop = maxOf(a.top, b.top)
        val interRight = minOf(a.right, b.right)
        val interBottom = minOf(a.bottom, b.bottom)
        val interW = (interRight - interLeft).coerceAtLeast(0f)
        val interH = (interBottom - interTop).coerceAtLeast(0f)
        val inter = interW * interH
        val union = a.area + b.area - inter
        return if (union <= 0f) 0f else inter / union
    }

    /**
     * Suppress overlapping boxes of the *same class*. Boxes of different classes
     * never suppress each other. Returns survivors sorted by descending score,
     * truncated to [maxDetections].
     */
    fun suppress(
        detections: List<RawDetection>,
        iouThreshold: Float,
        maxDetections: Int = Int.MAX_VALUE,
    ): List<RawDetection> {
        if (detections.isEmpty()) return emptyList()
        val byScore = detections.sortedByDescending { it.score }
        val kept = ArrayList<RawDetection>(byScore.size)
        val removed = BooleanArray(byScore.size)

        for (i in byScore.indices) {
            if (removed[i]) continue
            val a = byScore[i]
            kept.add(a)
            if (kept.size >= maxDetections) break
            for (j in i + 1 until byScore.size) {
                if (removed[j]) continue
                val b = byScore[j]
                if (b.classIndex != a.classIndex) continue
                if (iou(a.box, b.box) > iouThreshold) removed[j] = true
            }
        }
        return kept
    }
}
