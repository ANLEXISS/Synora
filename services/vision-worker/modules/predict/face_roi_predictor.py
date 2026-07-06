import numpy as np


class FaceROIPredictor:

    def __init__(self):
        self.last = {}

    def predict(self, track_id, bbox):

        x1, y1, x2, y2 = bbox

        w = x2 - x1
        h = y2 - y1

        cx = (x1 + x2) / 2
        cy = (y1 + y2) / 2

        if track_id not in self.last:
            self.last[track_id] = (cx, cy)
            return None

        px, py = self.last[track_id]

        dx = cx - px
        dy = cy - py

        self.last[track_id] = (cx, cy)

        pred_x1 = int(x1 + dx * 0.8)
        pred_y1 = int(y1 + dy * 0.8)

        pred_x2 = int(x2 + dx * 0.8)
        pred_y2 = int(y2 + dy * 0.8)

        return pred_x1, pred_y1, pred_x2, pred_y2

    def reset(self):
        self.last.clear()
