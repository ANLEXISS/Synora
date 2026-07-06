import cv2
import logging
import numpy as np
import os
import time

from core.model_runner import create_model_runner


log = logging.getLogger(
    "synora.vision.face_detector"
)


# ------------------------------------------------
# SCRFD Runner
# ------------------------------------------------

class SCRFDFaceRunner:

    MODEL_PATH = (
        "/var/lib/synora/models/det_10g.rknn"
    )

    DEBUG_DIR = (
        "/var/lib/synora/debug/scrfd"
    )

    def __init__(
        self,
        model_path=MODEL_PATH,
    ):

        self.runner = create_model_runner(
            model_path
        )

        self.input_size = 640

        # beaucoup plus permissif
        self.conf_thresh = 0.15

        self.nms_thresh = 0.4

        self.strides = [8, 16, 32]

        os.makedirs(
            self.DEBUG_DIR,
            exist_ok=True,
        )

        self.debug_counter = 0

        log.info(
            "SCRFD backend=%s model=%s",
            self.runner.backend,
            model_path,
        )

    # ------------------------------------------------
    # LETTERBOX PREPROCESS
    # ------------------------------------------------

    def preprocess(
        self,
        img,
    ):

        orig_h, orig_w = img.shape[:2]

        scale = min(
            self.input_size / orig_w,
            self.input_size / orig_h,
        )

        new_w = int(orig_w * scale)
        new_h = int(orig_h * scale)

        resized = cv2.resize(
            img,
            (new_w, new_h),
        )

        canvas = np.zeros(
            (
                self.input_size,
                self.input_size,
                3,
            ),
            dtype=np.uint8,
        )

        pad_x = (
            self.input_size - new_w
        ) // 2

        pad_y = (
            self.input_size - new_h
        ) // 2

        canvas[
            pad_y:pad_y + new_h,
            pad_x:pad_x + new_w
        ] = resized

        img = canvas[
            :,
            :,
            ::-1
        ].astype(
            np.float32
        )

        img = (
            img - 127.5
        ) * 0.0078125

        img = np.transpose(
            img,
            (2, 0, 1),
        )

        img = np.expand_dims(
            img,
            0,
        )

        meta = {
            "scale": scale,
            "pad_x": pad_x,
            "pad_y": pad_y,
            "orig_w": orig_w,
            "orig_h": orig_h,
        }

        return (
            np.ascontiguousarray(img),
            meta,
        )

    # ------------------------------------------------
    # NMS
    # ------------------------------------------------

    def nms(
        self,
        dets,
    ):

        if len(dets) == 0:
            return []

        x1 = dets[:, 0]
        y1 = dets[:, 1]
        x2 = dets[:, 2]
        y2 = dets[:, 3]

        scores = dets[:, 4]

        areas = (
            (x2 - x1) *
            (y2 - y1)
        )

        order = scores.argsort()[::-1]

        keep = []

        while order.size > 0:

            i = order[0]

            keep.append(i)

            xx1 = np.maximum(
                x1[i],
                x1[order[1:]],
            )

            yy1 = np.maximum(
                y1[i],
                y1[order[1:]],
            )

            xx2 = np.minimum(
                x2[i],
                x2[order[1:]],
            )

            yy2 = np.minimum(
                y2[i],
                y2[order[1:]],
            )

            w = np.maximum(
                0,
                xx2 - xx1,
            )

            h = np.maximum(
                0,
                yy2 - yy1,
            )

            inter = w * h

            ovr = inter / (
                areas[i] +
                areas[order[1:]] -
                inter
            )

            inds = np.where(
                ovr <= self.nms_thresh
            )[0]

            order = order[
                inds + 1
            ]

        return keep

    # ------------------------------------------------
    # DEBUG SAVE
    # ------------------------------------------------

    def save_debug(
        self,
        frame,
        detections,
    ):

        if len(detections) == 0:
            return

        dbg = frame.copy()

        for det in detections:

            x1, y1, x2, y2, score = det

            cv2.rectangle(
                dbg,
                (int(x1), int(y1)),
                (int(x2), int(y2)),
                (0, 255, 0),
                2,
            )

            cv2.putText(
                dbg,
                f"{score:.2f}",
                (int(x1), int(y1) - 10),
                cv2.FONT_HERSHEY_SIMPLEX,
                0.5,
                (0, 255, 0),
                1,
            )

        ts = int(
            time.time() * 1000
        )

        path = os.path.join(
            self.DEBUG_DIR,
            f"scrfd_{ts}_{self.debug_counter}.jpg"
        )

        self.debug_counter += 1

        cv2.imwrite(
            path,
            dbg,
        )

    # ------------------------------------------------
    # DETECTION
    # ------------------------------------------------

    def detect(
        self,
        frame,
    ):

        orig_h, orig_w = frame.shape[:2]

        blob, meta = self.preprocess(
            frame
        )

        try:

            outputs = self.runner.infer(
                blob
            )

        except Exception:

            log.exception(
                "SCRFD inference failed"
            )

            return np.empty((0, 5)), None

        outputs = [
            np.asarray(o)
            for o in outputs
        ]

        if len(outputs) < 6:

            log.error(
                "INVALID SCRFD OUTPUT COUNT=%d",
                len(outputs),
            )

            return np.empty((0, 5)), None

        scores_list = []

        boxes_list = []

        kps_list = []

        for i, stride in enumerate(
            self.strides
        ):

            try:

                scores = self._score_values(
                    outputs[i].reshape(-1)
                )

                bbox = outputs[
                    i + 3
                ].reshape(
                    -1,
                    4,
                )

                kps = None

                if len(outputs) >= 9:

                    kps = outputs[
                        i + 6
                    ].reshape(
                        -1,
                        10,
                    )

            except Exception:

                log.exception(
                    "SCRFD OUTPUT PARSE FAILED"
                )

                continue

            feature = (
                self.input_size //
                stride
            )

            grid = np.stack(
                np.meshgrid(
                    np.arange(feature),
                    np.arange(feature),
                ),
                axis=-1,
            ).reshape(-1, 2)

            centers = (
                grid + 0.5
            ) * stride

            # ------------------------------------------------
            # ADAPTATIVE ANCHOR FIX
            # ------------------------------------------------

            if len(centers) != len(bbox):

                repeat_factor = (
                    len(bbox) //
                    len(centers)
                )

                if repeat_factor > 1:

                    centers = np.repeat(
                        centers,
                        repeat_factor,
                        axis=0,
                    )

            usable = min(
                len(centers),
                len(bbox),
                len(scores),
            )

            if usable <= 0:
                continue

            centers = centers[:usable]
            bbox = bbox[:usable]
            scores = scores[:usable]

            if kps is not None:
                kps = kps[:usable]

            bbox *= stride

            x1 = (
                centers[:, 0] -
                bbox[:, 0]
            )

            y1 = (
                centers[:, 1] -
                bbox[:, 1]
            )

            x2 = (
                centers[:, 0] +
                bbox[:, 2]
            )

            y2 = (
                centers[:, 1] +
                bbox[:, 3]
            )

            boxes = np.stack(
                [x1, y1, x2, y2],
                axis=1,
            )

            mask = (
                scores >
                self.conf_thresh
            )

            if not np.any(mask):
                continue

            boxes = boxes[mask]

            scores_sel = scores[mask]

            kps_sel = None

            if kps is not None:

                kps = kps * stride

                kps_points = kps.reshape(
                    -1,
                    5,
                    2,
                )

                kps_points[:, :, 0] = (
                    kps_points[:, :, 0] +
                    centers[:, 0:1]
                )

                kps_points[:, :, 1] = (
                    kps_points[:, :, 1] +
                    centers[:, 1:2]
                )

                kps_sel = kps_points[mask]

            scale = meta["scale"]

            pad_x = meta["pad_x"]
            pad_y = meta["pad_y"]

            boxes[:, 0] = (
                boxes[:, 0] - pad_x
            ) / scale

            boxes[:, 1] = (
                boxes[:, 1] - pad_y
            ) / scale

            boxes[:, 2] = (
                boxes[:, 2] - pad_x
            ) / scale

            boxes[:, 3] = (
                boxes[:, 3] - pad_y
            ) / scale

            boxes[:, 0] = np.clip(
                boxes[:, 0],
                0,
                orig_w,
            )

            boxes[:, 1] = np.clip(
                boxes[:, 1],
                0,
                orig_h,
            )

            boxes[:, 2] = np.clip(
                boxes[:, 2],
                0,
                orig_w,
            )

            boxes[:, 3] = np.clip(
                boxes[:, 3],
                0,
                orig_h,
            )

            if kps_sel is not None:

                kps_sel[:, :, 0] = (
                    kps_sel[:, :, 0] - pad_x
                ) / scale

                kps_sel[:, :, 1] = (
                    kps_sel[:, :, 1] - pad_y
                ) / scale

                kps_sel[:, :, 0] = np.clip(
                    kps_sel[:, :, 0],
                    0,
                    orig_w,
                )

                kps_sel[:, :, 1] = np.clip(
                    kps_sel[:, :, 1],
                    0,
                    orig_h,
                )

            boxes_list.append(
                boxes
            )

            scores_list.append(
                scores_sel
            )

            if kps_sel is not None:

                kps_list.append(
                    kps_sel
                )

        if not boxes_list:

            return np.empty(
                (0, 5)
            ), None

        boxes = np.concatenate(
            boxes_list
        )

        scores = np.concatenate(
            scores_list
        )

        det = np.hstack([
            boxes,
            scores[:, None],
        ])

        kps_all = None

        if kps_list:

            kps_all = np.concatenate(
                kps_list
            )

        keep = self.nms(det)

        det = det[keep]

        if kps_all is not None:

            kps_all = kps_all[keep]

        if len(det) > 0:

            self.save_debug(
                frame,
                det,
            )

            log.info(
                "SCRFD faces=%d best=%.3f",
                len(det),
                float(np.max(det[:, 4])),
            )

        return det, kps_all

    def _score_values(
        self,
        values,
    ):

        values = np.asarray(
            values,
            dtype=np.float32,
        )

        if values.size == 0:
            return values

        if (
            np.nanmin(values) >= 0.0 and
            np.nanmax(values) <= 1.0
        ):
            return values

        return (
            1.0 /
            (1.0 + np.exp(-values))
        )


# ------------------------------------------------
# FACE DETECTOR WRAPPER
# ------------------------------------------------

class FaceDetector:

    MIN_SCORE = 0.15

    MIN_SIZE = 30

    MAX_FACES = 10

    def __init__(self):

        log.info(
            "FACE DETECTOR INIT"
        )

        self.detector = (
            SCRFDFaceRunner()
        )

    def detect(
        self,
        frame,
    ):

        try:

            det, landmarks = self.detector.detect(
                frame
            )

        except Exception:

            log.exception(
                "FACE DETECTOR INFERENCE FAILED"
            )

            return []

        if (
            det is None or
            len(det) == 0
        ):

            return []

        results = []

        h, w = frame.shape[:2]

        for idx, row in enumerate(det):

            x1, y1, x2, y2, score = row

            if (
                score <
                self.MIN_SCORE
            ):
                continue

            x1 = max(
                0,
                int(x1),
            )

            y1 = max(
                0,
                int(y1),
            )

            x2 = min(
                w,
                int(x2),
            )

            y2 = min(
                h,
                int(y2),
            )

            bw = x2 - x1
            bh = y2 - y1

            if (
                bw < self.MIN_SIZE or
                bh < self.MIN_SIZE
            ):
                continue

            if (
                x2 <= x1 or
                y2 <= y1
            ):
                continue

            roi = frame[
                y1:y2,
                x1:x2,
            ]

            self.save_face_roi(
                roi
            )

            results.append({
                "bbox": (
                    x1,
                    y1,
                    x2,
                    y2,
                ),
                "landmarks": (
                    landmarks[idx].astype(
                        np.float32
                    ).tolist()
                    if landmarks is not None and
                    idx < len(landmarks)
                    else None
                ),
                "score": float(score),
            })

        results.sort(
            key=lambda r: r["score"],
            reverse=True,
        )

        if results:

            log.info(
                "FACE DETECTOR VALID=%d best=%.3f",
                len(results),
                results[0]["score"],
            )

        return results[
            :self.MAX_FACES
        ]
    

    def save_face_roi(
        self,
        roi,
    ):

        if roi is None:
            return

        if roi.size == 0:
            return

        ts = int(
            time.time() * 1000
        )

        path = os.path.join(
            self.detector.DEBUG_DIR,
            f"face_roi_{ts}_{self.detector.debug_counter}.jpg",
        )

        self.detector.debug_counter += 1

        cv2.imwrite(
            path,
            roi,
        )
