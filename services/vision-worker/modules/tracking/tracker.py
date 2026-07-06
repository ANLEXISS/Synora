import numpy as np
from scipy.optimize import linear_sum_assignment
import logging

log = logging.getLogger("synora.vision.tracker")


class Track:

    def __init__(self, box, track_id):
        self.box = np.array(box, dtype=float)
        self.id = track_id
        self.age = 1
        self.missed = 0
        self.last_seen = 0
        self.velocity = np.array([0.0, 0.0], dtype=float)
        self.bbox_history = [self.box.tolist()]

    def update(self, box):
        old = self.box.copy()
        self.box = np.array(box, dtype=float)
        self.age += 1
        self.missed = 0
        old_cx = (old[0] + old[2]) * 0.5
        old_cy = (old[1] + old[3]) * 0.5
        new_cx = (self.box[0] + self.box[2]) * 0.5
        new_cy = (self.box[1] + self.box[3]) * 0.5
        self.velocity = np.array(
            [
                new_cx - old_cx,
                new_cy - old_cy,
            ],
            dtype=float,
        )
        self.last_seen = self.age
        self.bbox_history.append(self.box.tolist())
        if len(self.bbox_history) > 20:
            del self.bbox_history[:-20]


class Tracker:

    IOU_THRESHOLD = 0.35
    NEW_TRACK_IOU_THRESHOLD = 0.2

    MAX_MISSED = 20
    MAX_TRACKS = 32

    def __init__(self):

        log.info("TRACKER INIT")

        self.tracks = []
        self.next_id = 0

    # ------------------------------------------------

    def reset(self):

        log.info("TRACKER RESET")

        self.tracks = []
        self.next_id = 0

    # ------------------------------------------------

    def update(self, detections):

        detections = np.asarray(detections, dtype=float)

        log.debug(
            "TRACKER UPDATE detections=%d tracks=%d",
            len(detections),
            len(self.tracks),
        )

        # ------------------------------------------------
        # NO DETECTIONS
        # ------------------------------------------------

        if len(detections) == 0:

            for track in self.tracks:
                track.missed += 1

            self._cleanup_tracks()

            return self.tracks

        # ------------------------------------------------
        # BOOTSTRAP
        # ------------------------------------------------

        if len(self.tracks) == 0:

            for det in detections[: self.MAX_TRACKS]:
                self._create_track(det)

            return self.tracks

        # ------------------------------------------------
        # IOU MATCHING
        # ------------------------------------------------

        iou_matrix = self._iou_matrix(detections)

        rows, cols = linear_sum_assignment(-iou_matrix)

        assigned_tracks = set()
        assigned_dets = set()

        for r, c in zip(rows, cols):

            if r >= len(detections) or c >= len(self.tracks):
                continue

            iou = iou_matrix[r, c]

            if iou >= self.IOU_THRESHOLD:

                self.tracks[c].update(detections[r])

                assigned_tracks.add(c)
                assigned_dets.add(r)

        # ------------------------------------------------
        # DISTANCE FALLBACK
        # ------------------------------------------------

        for r in range(len(detections)):

            if r in assigned_dets:
                continue

            det = detections[r]

            best_track = None
            best_dist = float("inf")

            for i, track in enumerate(self.tracks):

                if i in assigned_tracks:
                    continue

                tx1, ty1, tx2, ty2 = track.box
                dx1, dy1, dx2, dy2 = det

                tcx = (tx1 + tx2) * 0.5
                tcy = (ty1 + ty2) * 0.5

                dcx = (dx1 + dx2) * 0.5
                dcy = (dy1 + dy2) * 0.5

                dist = abs(tcx - dcx) + abs(tcy - dcy)

                size = max(tx2 - tx1, ty2 - ty1)

                if dist < size * 1.5 and dist < best_dist:

                    best_dist = dist
                    best_track = i

            if best_track is not None:

                self.tracks[best_track].update(det)

                assigned_tracks.add(best_track)
                assigned_dets.add(r)

        # ------------------------------------------------
        # MISSED TRACKS
        # ------------------------------------------------

        for i, track in enumerate(self.tracks):

            if i not in assigned_tracks:
                track.missed += 1

        self._cleanup_tracks()

        # ------------------------------------------------
        # NEW TRACKS
        # ------------------------------------------------

        for i, det in enumerate(detections):

            if i in assigned_dets:
                continue

            if len(self.tracks) >= self.MAX_TRACKS:
                break

            close = False

            for track in self.tracks:

                if self._iou(track.box, det) > self.NEW_TRACK_IOU_THRESHOLD:
                    close = True
                    break

            if close:
                continue

            self._create_track(det)

        return self.tracks

    # ------------------------------------------------

    def _cleanup_tracks(self):

        self.tracks = [
            t for t in self.tracks
            if t.missed < self.MAX_MISSED
        ]

    # ------------------------------------------------

    def _create_track(self, box):

        self.tracks.append(
            Track(box, self.next_id)
        )

        log.debug("NEW TRACK id=%d", self.next_id)

        self.next_id += 1

    # ------------------------------------------------

    def _iou_matrix(self, detections):

        tracks = np.array([t.box for t in self.tracks])

        matrix = np.zeros((len(detections), len(tracks)))

        for d, det in enumerate(detections):
            for t, trk in enumerate(tracks):
                matrix[d, t] = self._iou(det, trk)

        return matrix

    # ------------------------------------------------

    def _iou(self, a, b):

        ax1, ay1, ax2, ay2 = a
        bx1, by1, bx2, by2 = b

        inter_x1 = max(ax1, bx1)
        inter_y1 = max(ay1, by1)
        inter_x2 = min(ax2, bx2)
        inter_y2 = min(ay2, by2)

        inter_w = max(0, inter_x2 - inter_x1)
        inter_h = max(0, inter_y2 - inter_y1)

        inter = inter_w * inter_h

        area_a = (ax2 - ax1) * (ay2 - ay1)
        area_b = (bx2 - bx1) * (by2 - by1)

        union = area_a + area_b - inter

        if union <= 0:
            return 0.0

        return inter / union
