import os
import cv2
import numpy as np

from core.model_runner import create_model_runner


class WeaponDetector:

    INPUT_SIZE = 640
    CONF_TH = 0.4
    IOU_TH = 0.5

    def __init__(self):

        MODEL_PATH = "/var/lib/synora/models/weapon.rknn"

        self.runner = create_model_runner(
            MODEL_PATH
        )

        self.last_detections = []

    # ------------------------------------------------
    # PREPROCESS
    # ------------------------------------------------

    def preprocess(self, frame):

        img = cv2.resize(frame, (self.INPUT_SIZE, self.INPUT_SIZE))

        img = img[:, :, ::-1]
        img = img.astype(np.float32) / 255.0

        img = np.transpose(img, (2, 0, 1))
        img = np.expand_dims(img, 0)

        return img

    # ------------------------------------------------
    # IOU
    # ------------------------------------------------

    def iou(self, a, b):

        ax1, ay1, ax2, ay2 = a
        bx1, by1, bx2, by2 = b

        inter_x1 = max(ax1, bx1)
        inter_y1 = max(ay1, by1)
        inter_x2 = min(ax2, bx2)
        inter_y2 = min(ay2, by2)

        inter_area = max(0, inter_x2 - inter_x1) * max(0, inter_y2 - inter_y1)

        area_a = (ax2 - ax1) * (ay2 - ay1)
        area_b = (bx2 - bx1) * (by2 - by1)

        union = area_a + area_b - inter_area

        if union == 0:
            return 0

        return inter_area / union

    # ------------------------------------------------
    # NMS
    # ------------------------------------------------

    def nms(self, boxes, scores):

        indices = cv2.dnn.NMSBoxes(
            boxes,
            scores,
            self.CONF_TH,
            self.IOU_TH
        )

        if len(indices) == 0:
            return []

        return [i[0] if isinstance(i, (list, tuple, np.ndarray)) else i for i in indices]

    # ------------------------------------------------
    # POSTPROCESS YOLO
    # ------------------------------------------------

    def postprocess(self, outputs, frame):

        preds = np.asarray(outputs[0])

        if preds.ndim == 3:
            preds = preds[0]

        if preds.ndim == 2 and preds.shape[0] < preds.shape[1]:
            preds = preds.T

        h, w = frame.shape[:2]

        boxes = []
        scores = []

        for p in preds:

            obj = p[4]

            if obj < self.CONF_TH:
                continue

            cx, cy, bw, bh = p[0:4]

            x1 = int((cx - bw/2) * w)
            y1 = int((cy - bh/2) * h)
            x2 = int((cx + bw/2) * w)
            y2 = int((cy + bh/2) * h)

            boxes.append([x1, y1, x2-x1, y2-y1])
            scores.append(float(obj))

        keep = self.nms(boxes, scores)

        detections = []

        for i in keep:

            x, y, bw, bh = boxes[i]

            detections.append({
                "box": (x, y, x+bw, y+bh),
                "confidence": scores[i]
            })

        return detections

    # ------------------------------------------------
    # DETECT
    # ------------------------------------------------

    def detect(self, frame, tracks):

        inp = self.preprocess(frame)

        outputs = self.runner.infer(inp)

        detections = self.postprocess(outputs, frame)

        results = []

        for det in detections:

            box = det["box"]

            for track in tracks:

                if self.iou(box, track.box) > 0.2:

                    results.append({
                        "track": track.id,
                        "confidence": det["confidence"],
                        "box": box
                    })

        self.last_detections = results

        return results
