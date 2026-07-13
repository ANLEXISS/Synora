import cv2
import logging
import os
import numpy as np
import time

from core.model_runner import create_model_runner, ModelUnavailableError, model_status

DEBUG_LOG = "/tmp/yolo_debug.txt"
log = logging.getLogger(
    "synora.vision.person_detector"
)


class PersonDetector:

    MAX_PERSONS = 10

    DEBUG_DIR = "/var/lib/synora/debug/yolo"

    def __init__(self):

        log.info(
            "PERSON DETECTOR INIT"
        )

        cv2.setNumThreads(1)

        os.makedirs(
            self.DEBUG_DIR,
            exist_ok=True,
        )

        model_path = (
            "/var/lib/synora/models/yolov8.rknn"
        )
        self.model_path = model_path
        self.available = False
        self.error = None
        self.capability_status = model_status(model_path)
        self.runner = None
        try:
            self.runner = create_model_runner(model_path)
            self.available = True
        except ModelUnavailableError as exc:
            self.error = exc.message
            self.capability_status = exc.as_dict()
            log.error("YOLO unavailable code=%s model=%s error=%s", exc.code, model_path, exc.message)
        except Exception as exc:
            self.error = str(exc)
            self.capability_status = {"status": "unavailable", "code": "rknn_runtime_error", "path": model_path, "error": self.error}
            log.exception("YOLO unavailable model=%s", model_path)

        self.input_size = 640

        # plus strict pour éviter
        # les faux positifs absurdes
        self.conf_threshold = 0.40

        self.nms_threshold = 0.45

        self.canvas = np.zeros(
            (
                self.input_size,
                self.input_size,
                3,
            ),
            dtype=np.uint8,
        )

        self.debug_counter = 0

        if self.available:
            log.info(
                "PERSON DETECTOR READY backend=%s model=%s",
                self.runner.backend,
                model_path,
            )

    def capability(self):
        status = dict(self.capability_status or {})
        status.setdefault("path", self.model_path)
        status["status"] = "available" if self.available else "unavailable"
        if self.error:
            status["error"] = self.error
        return status

    # ------------------------------------------------

    def preprocess(
        self,
        frame,
    ):

        h, w = frame.shape[:2]

        scale = min(
            self.input_size / w,
            self.input_size / h,
        )

        new_w = int(w * scale)
        new_h = int(h * scale)

        resized = cv2.resize(
            frame,
            (new_w, new_h),
            interpolation=cv2.INTER_LINEAR,
        )

        self.canvas.fill(0)

        pad_x = (
            self.input_size - new_w
        ) // 2

        pad_y = (
            self.input_size - new_h
        ) // 2

        self.canvas[
            pad_y:pad_y + new_h,
            pad_x:pad_x + new_w
        ] = resized

        img = self.canvas[
            :,
            :,
            ::-1
        ].astype(np.float32)

        img /= 255.0

        tensor = np.transpose(
            img,
            (2, 0, 1),
        )

        tensor = np.expand_dims(
            tensor,
            0,
        )

        meta = {
            "scale": scale,
            "pad_x": pad_x,
            "pad_y": pad_y,
            "orig_h": h,
            "orig_w": w,
        }

        return (
            np.ascontiguousarray(
                tensor
            ),
            meta,
        )

    # ------------------------------------------------

    def save_detection_frame(
        self,
        frame,
        boxes,
    ):

        debug = frame.copy()

        for box in boxes:

            x1, y1, x2, y2 = box

            cv2.rectangle(
                debug,
                (x1, y1),
                (x2, y2),
                (0, 255, 0),
                2,
            )

        ts = int(
            time.time() * 1000
        )

        path = os.path.join(
            self.DEBUG_DIR,
            f"detection_{ts}_{self.debug_counter}.jpg",
        )

        self.debug_counter += 1

        cv2.imwrite(
            path,
            debug,
        )

    def save_person_roi(
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
            self.DEBUG_DIR,
            f"person_roi_{ts}_{self.debug_counter}.jpg",
        )

        self.debug_counter += 1

        cv2.imwrite(
            path,
            roi,
        )

    # ------------------------------------------------

    def detect(
        self,
        frame,
    ):

        if not self.available or self.runner is None:
            return []

        blob, meta = self.preprocess(
            frame
        )

        try:

            outputs = self.runner.infer(
                blob
            )

            with open(DEBUG_LOG, "a") as f:

                f.write("\n")
                f.write("=" * 80 + "\n")
                f.write("NEW INFERENCE\n")
                f.write("=" * 80 + "\n")

                f.write(f"Number outputs: {len(outputs)}\n")

                for idx, out in enumerate(outputs):

                    arr = np.asarray(out)

                    f.write(
                        f"OUTPUT {idx}\n"
                    )

                    f.write(
                        f"shape={arr.shape}\n"
                    )

                    f.write(
                        f"dtype={arr.dtype}\n"
                    )

                    f.write(
                        f"min={arr.min()}\n"
                    )

                    f.write(
                        f"max={arr.max()}\n"
                    )

                    f.write(
                        f"mean={arr.mean()}\n"
                    )

                    flat = arr.flatten()

                    f.write(
                        f"sample={flat[:50]}\n"
                    )

        except Exception:

            log.exception(
                "YOLO inference failed"
            )

            return []

        if not outputs:

            return []

        outputs = np.asarray(
            outputs[0]
        )

        with open(DEBUG_LOG, "a") as f:

            f.write(
                f"\nAFTER outputs[0]\n"
            )

            f.write(
                f"shape={outputs.shape}\n"
            )

            f.write(
                f"dtype={outputs.dtype}\n"
            )

        log.info(
            "YOLO output_shape=%s",
            outputs.shape,
        )

        if outputs.ndim == 3:

            outputs = outputs[0]

        if outputs.shape[0] < outputs.shape[1]:

            outputs = outputs.transpose()
            with open(DEBUG_LOG, "a") as f:

                f.write(
                    f"\nTRANSPOSED_SHAPE={outputs.shape}\n"
                )

                for idx in [0, 1, 10, 100, 1000]:

                    if idx < len(outputs):

                        f.write(
                            f"\nDETECTION {idx}\n"
                        )

                        f.write(
                            f"{outputs[idx][:20].tolist()}\n"
                        )

        boxes = []

        scores = []

        scale = meta["scale"]

        pad_x = meta["pad_x"]

        pad_y = meta["pad_y"]

        orig_w = meta["orig_w"]

        orig_h = meta["orig_h"]

        print("\nROWS TO PARSE")
        print("outputs.shape =", outputs.shape)

        if len(outputs) > 0:
            print("first row sample:")
            print(outputs[0][:20])

        row_debug = 0
        for row in outputs:


            if row_debug < 20:

                with open(DEBUG_LOG, "a") as f:

                    f.write(
                        f"\nROW {row_debug}\n"
                    )

                    f.write(
                        f"len={len(row)}\n"
                    )

                    f.write(
                        f"data={row[:20]}\n"
                    )

                row_debug += 1

            if len(row) < 6:
                continue

            cx, cy, bw, bh = row[:4]

            if max(cx, cy, bw, bh) <= 2.0:
                cx *= self.input_size
                cy *= self.input_size
                bw *= self.input_size
                bh *= self.input_size

            # RKNN YOLOv8 exports are commonly either:
            # [x y w h cls0 cls1 ...] or [x y w h obj cls0 cls1 ...].
            if len(row) >= 85:
                obj_raw = row[4]
                cls_raw = row[5:]
                obj_conf = self._score_value(obj_raw)
            else:
                obj_conf = 1.0
                cls_raw = row[4:]

            class_scores = self._score_values(
                cls_raw
            )
            if row_debug < 5:

                with open(DEBUG_LOG, "a") as f:

                    f.write("\nCLASS DEBUG\n")

                    f.write(
                        f"raw_cls_min={np.min(cls_raw)}\n"
                    )

                    f.write(
                        f"raw_cls_max={np.max(cls_raw)}\n"
                    )

                    f.write(
                        f"raw_cls_mean={np.mean(cls_raw)}\n"
                    )

                    top_idx = np.argsort(cls_raw)[-10:]

                    f.write(
                        f"top10_raw_idx={top_idx.tolist()}\n"
                    )

                    f.write(
                        f"top10_raw_values={cls_raw[top_idx].tolist()}\n"
                    )

            class_id = int(
                np.argmax(
                    class_scores
                )
            )

            if row_debug < 5:

                with open(DEBUG_LOG, "a") as f:

                    f.write(
                        f"argmax_class={class_id}\n"
                    )

                    f.write(
                        f"best_score={class_scores[class_id]}\n"
                    )

            class_conf = float(
                class_scores[
                    class_id
                ]
            )


            confidence = (
                obj_conf *
                class_conf
            )

            if len(boxes) < 20:
                if row_debug < 20:
                    with open(DEBUG_LOG, "a") as f:

                        f.write(
                            f"class_id={class_id} "
                            f"class_conf={class_conf} "
                            f"obj_conf={obj_conf} "
                            f"confidence={confidence}\n"
                        )

            # personne uniquement
            if class_id != 0:
                continue

            if confidence < self.conf_threshold:
                continue

            x1 = cx - bw / 2
            y1 = cy - bh / 2

            x2 = cx + bw / 2
            y2 = cy + bh / 2

            x1 = (
                x1 - pad_x
            ) / scale

            y1 = (
                y1 - pad_y
            ) / scale

            x2 = (
                x2 - pad_x
            ) / scale

            y2 = (
                y2 - pad_y
            ) / scale

            x1 = int(np.clip(
                x1,
                0,
                orig_w,
            ))

            y1 = int(np.clip(
                y1,
                0,
                orig_h,
            ))

            x2 = int(np.clip(
                x2,
                0,
                orig_w,
            ))

            y2 = int(np.clip(
                y2,
                0,
                orig_h,
            ))

            if (
                x2 <= x1 or
                y2 <= y1
            ):
                continue

            # évite les boxes absurdes
            box_w = x2 - x1
            box_h = y2 - y1

            aspect = (
                box_h /
                max(box_w, 1)
            )

            if aspect > 5.0:
                continue

            if aspect < 0.5:
                continue

            if box_w < 40:
                continue

            if box_h < 80:
                continue

            boxes.append([
                x1,
                y1,
                x2,
                y2,
            ])

            scores.append(
                confidence
            )

            log.info(
                "YOLO person conf=%.3f obj=%.3f class=%.3f bbox=(%d,%d,%d,%d)",
                confidence,
                obj_conf,
                class_conf,
                x1,
                y1,
                x2,
                y2,
            )

        if not boxes:
            print("\nNO BOXES AFTER FILTERING")
            print("conf_threshold =", self.conf_threshold)
            return []

        nms_boxes = []

        for b in boxes:

            x1, y1, x2, y2 = b

            nms_boxes.append([
                x1,
                y1,
                x2 - x1,
                y2 - y1,
            ])

        indices = cv2.dnn.NMSBoxes(
            nms_boxes,
            scores,
            self.conf_threshold,
            self.nms_threshold,
        )

        results = []

        if len(indices) > 0:

            for i in indices.flatten(): # type: ignore

                x1, y1, x2, y2 = boxes[i]

                roi = frame[
                    y1:y2,
                    x1:x2,
                ]

                self.save_person_roi(
                    roi
                )

                results.append({
                    "bbox": (
                        x1,
                        y1,
                        x2,
                        y2,
                    ),
                    "score": float(
                        scores[i]
                    ),
                })

        results.sort(
            key=lambda r: r["score"],
            reverse=True,
        )

        if results:

            self.save_detection_frame(
                frame,
                [
                    r["bbox"]
                    for r in results
                ],
            )

            best = results[0]

            log.info(
                "YOLO FINAL persons=%d best=%.3f bbox=%s",
                len(results),
                best["score"],
                best["bbox"],
            )

        return results[
            :self.MAX_PERSONS
        ]

    def _score_value(
        self,
        value,
    ):

        value = float(value)

        if 0.0 <= value <= 1.0:
            return value

        return float(
            1.0 /
            (1.0 + np.exp(-value))
        )

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
