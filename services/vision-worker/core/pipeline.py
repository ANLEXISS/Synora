import cv2
import numpy as np
import time
import logging
import os
from collections import deque

from modules.detect.face_detector import FaceDetector
from modules.tracking.tracker import Tracker
from core.events import EventBuilder
from core.observability import DebugStore, PipelineMetrics, PipelineTrace


log = logging.getLogger(
    "synora.vision.pipeline"
)


class VisionPipeline:

    TARGET_FPS = 5

    PERSON_DETECT_INTERVAL = 3

    ARCFACE_INTERVAL_KNOWN = 8

    ARCFACE_INTERVAL_UNKNOWN = 2

    RECOGNITION_BUFFER = 10

    IDENTITY_STABILITY_THRESHOLD = 0.60

    DETECT_SCALE = 1

    MAX_DETECTIONS = 10

    MAX_TRACKS_ANALYZED = 10

    MAX_FACE_CANDIDATES = 10

    FACE_MIN_SIZE = 60

    FACE_PADDING = 0.20

    DEBUG_DIR = "/var/lib/synora/debug"

    ARCFACE_TEMPLATE = np.array([
        [38.2946, 51.6963],
        [73.5318, 51.5014],
        [56.0252, 71.7366],
        [41.5493, 92.3655],
        [70.7299, 92.2041],
    ], dtype=np.float32)

    # ------------------------------------------------

    def __init__(
        self,
        face_recognizer,
        person_detector,
    ):

        log.info(
            "PIPELINE INIT"
        )

        cv2.setNumThreads(1)

        cv2.ocl.setUseOpenCL(False)

        os.makedirs(
            self.DEBUG_DIR,
            exist_ok=True,
        )

        self.debug_store = DebugStore(
            self.DEBUG_DIR,
            enabled=True,
        )

        self.metrics = PipelineMetrics()

        for queue_name in (
            "rtsp",
            "yolo",
            "scrfd",
            "arcface",
            "event",
        ):
            self.metrics.queue(
                queue_name,
                0,
                4,
            )

        self.face_recognizer = (
            face_recognizer
        )

        self.person_detector = (
            person_detector
        )

        self.face_detector = (
            FaceDetector()
        )

        self.tracker = Tracker()

        self.events = EventBuilder()

        self.frame_id = 0

        self.last_detections = []

        self.last_yolo_frame = -999999

        self.person_seen = False

        self.active_tracks_seen = set()

        self.track_faces = {}

        self.track_identity_memory = {}

        self.track_recognition_buffers = {}

        self.track_last_arcface = {}

        self.frame_traces = {}

    # ------------------------------------------------

    def save_debug_person(
        self,
        frame,
        track_id,
    ):

        path = os.path.join(
            self.DEBUG_DIR,
            "person_detector",
        )

        os.makedirs(
            path,
            exist_ok=True,
        )

        cv2.imwrite(
            os.path.join(
                path,
                f"frame_{self.frame_id}_track_{track_id}.jpg",
            ),
            frame,
        )


    def save_debug_face(
        self,
        face,
        track_id,
        index,
    ):

        path = os.path.join(
            self.DEBUG_DIR,
            "face_detector",
        )

        os.makedirs(
            path,
            exist_ok=True,
        )

        cv2.imwrite(
            os.path.join(
                path,
                f"frame_{self.frame_id}_track_{track_id}_face_{index}.jpg",
            ),
            face,
        )


    def save_debug_arcface(
        self,
        face,
        track_id,
        index,
    ):

        path = os.path.join(
            self.DEBUG_DIR,
            "face_recognizer",
        )

        os.makedirs(
            path,
            exist_ok=True,
        )

        cv2.imwrite(
            os.path.join(
                path,
                f"frame_{self.frame_id}_track_{track_id}_aligned_{index}.jpg",
            ),
            face,
        )


    def save_debug_recognition(
        self,
        face,
        track_id,
        identity,
        score,
    ):

        path = os.path.join(
            self.DEBUG_DIR,
            "face_recognizer",
        )

        os.makedirs(
            path,
            exist_ok=True,
        )

        cv2.imwrite(
            os.path.join(
                path,
                f"track_{track_id}_{identity}_{score:.2f}.jpg",
            ),
            face,
        )

    # ------------------------------------------------

    def debug_step(
        self,
        camera,
        frame_id,
        step,
        raw=None,
        annotated=None,
        duration_ms=0.0,
        success=True,
        error=None,
        output_count=0,
        details=None,
        trace=None,
        item_id=None,
    ):

        return self.debug_store.save_step(
            camera_id=camera,
            frame_id=frame_id,
            step=step,
            raw=raw,
            annotated=annotated,
            duration_ms=duration_ms,
            success=success,
            error=error,
            output_count=output_count,
            details=details or {},
            trace_id=trace.trace_id if trace else None,
            item_id=item_id,
        )

    def draw_boxes(
        self,
        frame,
        boxes,
        color=(0, 255, 0),
        label=None,
    ):

        annotated = frame.copy()

        for idx, box in enumerate(boxes):

            x1, y1, x2, y2 = map(int, box)

            cv2.rectangle(
                annotated,
                (x1, y1),
                (x2, y2),
                color,
                2,
            )

            if label:
                cv2.putText(
                    annotated,
                    f"{label}{idx}",
                    (x1, max(20, y1 - 6)),
                    cv2.FONT_HERSHEY_SIMPLEX,
                    0.5,
                    color,
                    1,
                    cv2.LINE_AA,
                )

        return annotated

    def should_run_arcface(
        self,
        track_id,
    ):

        memory = self.track_identity_memory.get(
            track_id
        )

        interval = (
            self.ARCFACE_INTERVAL_KNOWN
            if memory and memory.get("status") == "match"
            else self.ARCFACE_INTERVAL_UNKNOWN
        )

        last = self.track_last_arcface.get(
            track_id,
            -interval,
        )

        return (
            self.frame_id - last
        ) >= interval

    def recognition_buffer(
        self,
        track_id,
    ):

        return self.track_recognition_buffers.setdefault(
            track_id,
            deque(maxlen=self.RECOGNITION_BUFFER),
        )

    def dashboard(
        self,
    ):

        return self.metrics.snapshot()

    # ------------------------------------------------

    def face_quality(
        self,
        face,
    ):

        if face is None:
            return 0.0

        if face.size == 0:
            return 0.0

        gray = cv2.cvtColor(
            face,
            cv2.COLOR_BGR2GRAY,
        )

        sharpness = cv2.Laplacian(
            gray,
            cv2.CV_64F,
        ).var()

        h, w = face.shape[:2]

        size_score = min(h, w)

        return (
            sharpness * 0.7 +
            size_score * 0.3
        )

    # ------------------------------------------------

    def face_frontal_score(
        self,
        face,
    ):

        h, w = face.shape[:2]

        if h <= 0 or w <= 0:
            return 0.0

        ratio = (
            min(w, h) /
            max(w, h)
        )

        return float(ratio)

    # ------------------------------------------------

    def make_square_crop(
        self,
        frame,
        x1,
        y1,
        x2,
        y2,
    ):

        h, w = frame.shape[:2]

        bw = x2 - x1
        bh = y2 - y1

        pad_x = int(
            bw * self.FACE_PADDING
        )

        pad_y = int(
            bh * self.FACE_PADDING
        )

        x1 -= pad_x
        y1 -= pad_y

        x2 += pad_x
        y2 += pad_y

        x1 = max(0, x1)
        y1 = max(0, y1)

        x2 = min(w, x2)
        y2 = min(h, y2)

        crop = frame[
            y1:y2,
            x1:x2,
        ]

        return crop

    # ------------------------------------------------

    def align_face_arcface(
        self,
        frame,
        landmarks,
    ):

        if landmarks is None:
            return None

        pts = np.asarray(
            landmarks,
            dtype=np.float32,
        )

        if pts.shape != (5, 2):
            return None

        try:

            transform, _ = cv2.estimateAffinePartial2D(
                pts,
                self.ARCFACE_TEMPLATE,
                method=cv2.LMEDS,
            )

            if transform is None:
                return None

            return cv2.warpAffine(
                frame,
                transform,
                (112, 112),
                flags=cv2.INTER_LINEAR,
                borderValue=0,
            )

        except Exception:

            log.exception(
                "ARCFACE ALIGN FAILED"
            )

            return None

    # ------------------------------------------------

    def detect_persons(
        self,
        frame,
        camera=None,
        trace=None,
    ):

        start = time.perf_counter()

        frame_small = cv2.resize(
            frame,
            None,
            fx=self.DETECT_SCALE,
            fy=self.DETECT_SCALE,
        )

        try:

            persons_small = (
                self.person_detector.detect(
                    frame_small
                )
            )

            success = True
            error = None

        except Exception as exc:

            log.exception(
                "YOLO FAILURE frame=%d",
                self.frame_id,
            )
            self.metrics.error(
                "yolo",
                str(exc),
                trace.trace_id if trace else None,
            )
            persons_small = []
            success = False
            error = str(exc)

        debug = frame_small.copy()

        for p in persons_small:

            x1,y1,x2,y2 = p["bbox"]

            cv2.rectangle(
                debug,
                (x1,y1),
                (x2,y2),
                (0,255,0),
                2,
            )

        duration_ms = (
            time.perf_counter() - start
        ) * 1000.0

        self.metrics.incr(
            "persons_detected",
            len(persons_small),
        )

        self.debug_step(
            camera,
            self.frame_id,
            "yolo",
            raw=frame_small,
            annotated=debug,
            duration_ms=duration_ms,
            success=success,
            error=error,
            output_count=len(persons_small),
            details={
                "detections": persons_small,
                "detect_scale": self.DETECT_SCALE,
            },
            trace=trace,
        )

        log.info(
            "YOLO DETECTIONS=%d",
            len(persons_small),
        )

        detections = []

        for p in persons_small:

            x1, y1, x2, y2 = p["bbox"]

            detections.append([
                int(x1 / self.DETECT_SCALE),
                int(y1 / self.DETECT_SCALE),
                int(x2 / self.DETECT_SCALE),
                int(y2 / self.DETECT_SCALE),
            ])

        return detections[
            :self.MAX_DETECTIONS
        ]

    # ------------------------------------------------

    def process_person_frame(
        self,
        frame,
        camera=None,
        source_frame_id=None,
    ):

        self.frame_id += 1
        frame_start = time.perf_counter()
        self.metrics.mark_processed()

        trace = PipelineTrace(
            camera,
            source_frame_id if source_frame_id is not None else self.frame_id,
            self.debug_store,
            self.metrics,
        )

        self.frame_traces[self.frame_id] = trace.trace_id

        log.info(
            "PROCESS FRAME id=%d trace=%s shape=%s",
            self.frame_id,
            trace.trace_id,
            frame.shape,
        )

        self.debug_step(
            camera,
            self.frame_id,
            "raw",
            raw=frame,
            annotated=frame,
            output_count=1,
            details={
                "source_frame_id": source_frame_id,
            },
            trace=trace,
        )

        target_yolo_fps = (
            5.0 if self.person_seen else 1.0
        )

        self.PERSON_DETECT_INTERVAL = max(
            1,
            int(
                max(self.TARGET_FPS, 1) /
                target_yolo_fps
            ),
        )

        should_detect_persons = (
            self.frame_id - self.last_yolo_frame
        ) >= self.PERSON_DETECT_INTERVAL

        if should_detect_persons:

            with trace.step(
                "yolo",
                input_shape=list(frame.shape),
            ):

                detections = self.detect_persons(
                    frame,
                    camera=camera,
                    trace=trace,
                )

            self.last_detections = detections

            self.last_yolo_frame = self.frame_id

        else:

            detections = self.last_detections

            trace.skipped(
                "yolo",
                "adaptive_interval",
                {
                    "person_detect_interval": self.PERSON_DETECT_INTERVAL,
                    "target_yolo_fps": target_yolo_fps,
                    "reused_detections": len(detections),
                },
            )

        with trace.step(
            "tracks",
            details={
                "detections": len(detections),
            },
            input_shape=list(frame.shape),
        ):

            tracks = self.tracker.update(
                detections
            )

        log.info(
            "TRACKS=%d",
            len(tracks),
        )

        if tracks:
            self.person_seen = True

        self.active_tracks_seen.update(
            t.id for t in tracks
        )

        h, w = frame.shape[:2]

        for track in tracks:

            x1, y1, x2, y2 = map(
                int,
                track.box,
            )

            x1 = max(0, x1)
            y1 = max(0, y1)

            x2 = min(w, x2)
            y2 = min(h, y2)

            if (
                (x2 - x1) < 40 or
                (y2 - y1) < 40
            ):
                log.warning(
                    "TRACK SKIPPED too_small frame=%d track=%s bbox=%s trace=%s",
                    self.frame_id,
                    track.id,
                    track.box,
                    trace.trace_id,
                )
                continue

            roi = frame[
                y1:y2,
                x1:x2,
            ]

            if roi.size == 0:
                log.warning(
                    "TRACK SKIPPED empty_roi frame=%d track=%s trace=%s",
                    self.frame_id,
                    track.id,
                    trace.trace_id,
                )
                continue

            self.debug_step(
                camera,
                self.frame_id,
                "tracks",
                raw=roi,
                annotated=self.draw_boxes(frame, [track.box], label="track-"),
                output_count=len(tracks),
                details={
                    "track_id": track.id,
                    "bbox": track.box,
                    "age": getattr(track, "age", None),
                    "missed": getattr(track, "missed", None),
                    "velocity": getattr(track, "velocity", [0.0, 0.0]),
                    "bbox_history": getattr(track, "bbox_history", []),
                },
                trace=trace,
                item_id=f"track{track.id}",
            )

            scrfd_start = time.perf_counter()
            try:
                with trace.step(
                    "scrfd",
                    details={
                        "track_id": track.id,
                    },
                    input_shape=list(roi.shape),
                ):
                    faces = self.face_detector.detect(
                        roi
                    )
                scrfd_success = True
                scrfd_error = None
            except Exception as exc:
                faces = []
                scrfd_success = False
                scrfd_error = str(exc)
                log.exception(
                    "SCRFD FAILURE frame=%d track=%s trace=%s",
                    self.frame_id,
                    track.id,
                    trace.trace_id,
                )
                self.metrics.error(
                    "scrfd",
                    str(exc),
                    trace.trace_id,
                )

            log.info(
                "SCRFD faces=%d",
                len(faces),
            )

            self.metrics.incr(
                "faces_detected",
                len(faces),
            )

            scrfd_annotated = roi.copy()
            for face in faces:
                cv2.rectangle(
                    scrfd_annotated,
                    tuple(map(int, face["bbox"][:2])),
                    tuple(map(int, face["bbox"][2:])),
                    (255, 0, 0),
                    2,
                )

            self.debug_step(
                camera,
                self.frame_id,
                "scrfd",
                raw=roi,
                annotated=scrfd_annotated,
                duration_ms=(time.perf_counter() - scrfd_start) * 1000.0,
                success=scrfd_success,
                error=scrfd_error,
                output_count=len(faces),
                details={
                    "track_id": track.id,
                    "faces": faces,
                },
                trace=trace,
                item_id=f"track{track.id}",
            )

            if not faces:
                log.warning(
                    "Personne detectee -> aucun visage trouve frame=%d track=%s trace=%s",
                    self.frame_id,
                    track.id,
                    trace.trace_id,
                )
                continue

            candidates = (
                self.track_faces.setdefault(
                    track.id,
                    [],
                )
            )

            for face in faces:

                fx1, fy1, fx2, fy2 = (
                    face["bbox"]
                )

                crop = self.make_square_crop(
                    roi,
                    fx1,
                    fy1,
                    fx2,
                    fy2,
                )

                self.save_debug_face(
                    crop,
                    track.id,
                    len(candidates),
                )

                if crop.size == 0:
                    log.warning(
                        "FACE CROP EMPTY frame=%d track=%s trace=%s",
                        self.frame_id,
                        track.id,
                        trace.trace_id,
                    )
                    continue

                if (
                    crop.shape[0] < self.FACE_MIN_SIZE or
                    crop.shape[1] < self.FACE_MIN_SIZE
                ):
                    log.warning(
                        "FACE CROP SKIPPED too_small frame=%d track=%s size=%s trace=%s",
                        self.frame_id,
                        track.id,
                        crop.shape,
                        trace.trace_id,
                    )
                    continue

                with trace.step(
                    "align",
                    details={
                        "track_id": track.id,
                        "face_bbox": face.get("bbox"),
                    },
                    input_shape=list(roi.shape),
                ):

                    arcface_input = self.align_face_arcface(
                        roi,
                        face.get("landmarks"),
                    )

                if arcface_input is not None:
                    self.save_debug_arcface(
                        arcface_input,
                        track.id,
                        len(candidates),
                    )

                if arcface_input is None:

                    log.warning(
                        "FACE ALIGN FAILED fallback_resize frame=%d track=%s trace=%s",
                        self.frame_id,
                        track.id,
                        trace.trace_id,
                    )

                    arcface_input = cv2.resize(
                        crop,
                        (112, 112),
                        interpolation=cv2.INTER_CUBIC,
                    )

                self.debug_step(
                    camera,
                    self.frame_id,
                    "align",
                    raw=crop,
                    annotated=arcface_input,
                    output_count=1,
                    details={
                        "track_id": track.id,
                        "face_bbox": face.get("bbox"),
                        "landmarks": face.get("landmarks"),
                    },
                    trace=trace,
                    item_id=f"track{track.id}_face{len(candidates)}",
                )

                quality = self.face_quality(
                    crop
                )

                frontal = self.face_frontal_score(
                    crop
                )

                score = (
                    min(quality, 300.0) * 0.5 +
                    frontal * 100.0 * 0.5
                )

                candidates.append(
                    (
                        score,
                        arcface_input,
                        trace.trace_id,
                    )
                )

                if len(candidates) > self.MAX_FACE_CANDIDATES:

                    candidates.sort(
                        key=lambda x: x[0],
                        reverse=True,
                    )

                    del candidates[
                        self.MAX_FACE_CANDIDATES:
                    ]

        self.metrics.latency(
            (time.perf_counter() - frame_start) * 1000.0
        )

        return trace

    # ------------------------------------------------

    def run_recognition(
        self,
        camera,
    ):
        events = []

        scene_id = (
            f"scene-{int(time.time())}"
        )

        persons_count = len(
            self.active_tracks_seen
        )

        track_ids = sorted(
            set(self.active_tracks_seen) |
            set(self.track_faces.keys())
        )

        for track_id in track_ids:

            trace = PipelineTrace(
                camera,
                f"recognition-track-{track_id}",
                self.debug_store,
                self.metrics,
            )

            faces = self.track_faces.get(
                track_id,
                [],
            )

            identity = None
            score = 0.0
            best_score = 0.0
            consistency = 0.0
            status = "unknown"

            if not faces:

                log.warning(
                    "Personne detectee -> aucun visage trouve track=%s trace=%s",
                    track_id,
                    trace.trace_id,
                )

            else:

                faces.sort(
                    key=lambda x: x[0],
                    reverse=True,
                )

                best_faces = [
                    item[1]
                    for item in faces[:5]
                ]

                embeddings = []

                if not self.should_run_arcface(
                    track_id
                ):

                    trace.skipped(
                        "arcface",
                        "identity_reuse_interval",
                        {
                            "track_id": track_id,
                            "last_arcface_frame": self.track_last_arcface.get(track_id),
                        },
                    )

                    memory = self.track_identity_memory.get(
                        track_id
                    )

                    if memory:
                        status = memory.get(
                            "status",
                            "unknown",
                        )
                        identity = memory.get(
                            "identity"
                        )
                        score = float(
                            memory.get(
                                "score",
                                0.0,
                            )
                        )
                        best_score = float(
                            memory.get(
                                "best_score",
                                score,
                            )
                        )
                        consistency = float(
                            memory.get(
                                "consistency",
                                0.0,
                            )
                        )

                else:

                    self.track_last_arcface[
                        track_id
                    ] = self.frame_id

                    for face_index, face in enumerate(best_faces):

                        arcface_start = time.perf_counter()
                        emb = None
                        arcface_success = True
                        arcface_error = None

                        try:
                            with trace.step(
                                "arcface",
                                details={
                                    "track_id": track_id,
                                    "face_index": face_index,
                                },
                                input_shape=list(face.shape),
                            ):

                                emb = self.face_recognizer.embed(
                                    face
                                )

                        except Exception as exc:

                            arcface_success = False
                            arcface_error = str(exc)
                            log.exception(
                                "ARCFACE EMBED FAILURE track=%s trace=%s",
                                track_id,
                                trace.trace_id,
                            )
                            self.metrics.error(
                                "arcface",
                                str(exc),
                                trace.trace_id,
                            )

                        if emb is None:

                            arcface_success = False
                            arcface_error = (
                                arcface_error or
                                "embedding is None"
                            )
                            log.error(
                                "Visage trouve -> aucun embedding track=%s trace=%s",
                                track_id,
                                trace.trace_id,
                            )

                        else:

                            embeddings.append(
                                emb
                            )

                        self.debug_step(
                            camera,
                            f"recognition-track-{track_id}",
                            "arcface",
                            raw=face,
                            annotated=face,
                            duration_ms=(time.perf_counter() - arcface_start) * 1000.0,
                            success=arcface_success,
                            error=arcface_error,
                            output_count=1 if emb is not None else 0,
                            details={
                                "track_id": track_id,
                                "face_index": face_index,
                                "embedding_created": emb is not None,
                            },
                            trace=trace,
                            item_id=f"track{track_id}_face{face_index}",
                        )

                        self.save_debug_recognition(
                            face,
                            track_id,
                            "candidate",
                            0.0,
                        )

                    any_match = False

                    for emb in embeddings:

                        with trace.step(
                            "match",
                            details={
                                "track_id": track_id,
                            },
                        ):

                            (
                                emb_status,
                                emb_identity,
                                emb_score,
                            ) = self.face_recognizer.identify_embedding(
                                emb
                            )

                        if emb_identity:
                            any_match = True

                        if emb_score > best_score:
                            best_score = emb_score

                        self.recognition_buffer(
                            track_id
                        ).append({
                            "status": emb_status,
                            "identity": emb_identity,
                            "score": float(emb_score),
                            "trace_id": trace.trace_id,
                            "time": time.time(),
                        })

                    if embeddings and not any_match:

                        log.warning(
                            "Embedding cree -> aucun matching track=%s trace=%s",
                            track_id,
                            trace.trace_id,
                        )

                    observations = [
                        obs for obs in self.recognition_buffer(
                            track_id
                        )
                        if obs.get("identity")
                    ]

                    if observations:

                        identity = max(
                            set(obs["identity"] for obs in observations),
                            key=lambda ident: sum(
                                1 for obs in observations
                                if obs["identity"] == ident
                            ),
                        )

                        scores = [
                            obs["score"]
                            for obs in observations
                            if obs["identity"] == identity
                        ]

                        score = float(
                            np.mean(scores)
                        )

                        best_score = float(
                            max(scores)
                        )

                        consistency = (
                            len(scores) /
                            max(len(observations), 1)
                        )

                        if (
                            score >= self.face_recognizer.match_threshold and
                            consistency >= self.IDENTITY_STABILITY_THRESHOLD
                        ):

                            status = "match"

                        else:

                            status = "uncertain"

                    elif best_score >= getattr(
                        self.face_recognizer,
                        "uncertain_threshold",
                        self.face_recognizer.match_threshold,
                    ):

                        status = "uncertain"

                if identity:

                    self.save_debug_recognition(
                        best_faces[0],
                        track_id,
                        identity,
                        score,
                    )

                self.track_identity_memory[
                    track_id
                ] = {
                    "identity": identity,
                    "score": score,
                    "best_score": best_score,
                    "consistency": consistency,
                    "status": status,
                }

            self.metrics.identity(
                track_id,
                {
                    "status": status,
                    "identity": identity,
                    "score": score,
                    "best_score": best_score,
                    "consistency": consistency,
                    "observations": len(self.recognition_buffer(track_id)),
                },
            )

            if status == "match":

                event = self.events.identity(
                    camera=camera,
                    scene_id=scene_id,
                    track_id=track_id,
                    person=identity,
                    score=score,
                    persons=persons_count,
                )
                self.metrics.incr("known_faces")

            elif status == "uncertain":

                event = self.events.uncertain(
                    camera=camera,
                    scene_id=scene_id,
                    track_id=track_id,
                    identity=identity,
                    score=score,
                    persons=persons_count,
                )
                self.metrics.incr("uncertain_faces")

            else:

                event = self.events.unknown(
                    camera=camera,
                    scene_id=scene_id,
                    track_id=track_id,
                    persons=persons_count,
                )
                self.metrics.incr("unknown_faces")

            events.append(event)
            self.metrics.incr("events_generated")

            self.debug_step(
                camera,
                f"recognition-track-{track_id}",
                "events",
                output_count=1,
                details={
                    "track_id": track_id,
                    "event": event,
                    "status": status,
                    "identity": identity,
                    "score": score,
                    "best_score": best_score,
                    "consistency": consistency,
                },
                trace=trace,
                item_id=f"track{track_id}",
            )

        if not events and self.person_seen:

            log.error(
                "Matching effectue -> aucun event camera=%s scene=%s",
                camera,
                scene_id,
            )
            self.metrics.incr("events_dropped")

            event = self.events.unknown(
                camera=camera,
                scene_id=scene_id,
                track_id=0,
                persons=max(1, persons_count),
            )
            events.append(event)
            self.metrics.incr("events_generated")

        return events

    # ------------------------------------------------

    def process_clip(
        self,
        clip_path,
        camera,
    ):

        analysis_levels = [
            {
                "name": "fast",
                "target_fps": 5,
                "face_min": 60,
                "padding": 0.15,
                "all_frames": False,
            },
            {
                "name": "balanced",
                "target_fps": 10,
                "face_min": 45,
                "padding": 0.20,
                "all_frames": False,
            },
            {
                "name": "aggressive",
                "target_fps": 15,
                "face_min": 35,
                "padding": 0.25,
                "all_frames": False,
            },
            {
                "name": "forensic",
                "target_fps": 0,
                "face_min": 30,
                "padding": 0.30,
                "all_frames": True,
            },
        ]

        final_events = []

        for level in analysis_levels:

            log.info(
                "ANALYSIS PASS=%s",
                level["name"],
            )

            self.FACE_MIN_SIZE = (
                level["face_min"]
            )

            self.FACE_PADDING = (
                level["padding"]
            )

            self.tracker.reset()

            self.track_faces.clear()

            self.track_identity_memory.clear()

            self.track_recognition_buffers.clear()

            self.track_last_arcface.clear()

            self.last_detections.clear()

            self.last_yolo_frame = -999999

            self.active_tracks_seen.clear()

            self.person_seen = False

            self.frame_id = 0

            self.TARGET_FPS = (
                level["target_fps"]
                if level["target_fps"] > 0
                else fps
            )

            cap = cv2.VideoCapture(
                clip_path
            )

            if not cap.isOpened():

                log.error(
                    "VIDEO OPEN FAILED -> %s",
                    clip_path,
                )
                self.metrics.error(
                    "decode",
                    f"video open failed: {clip_path}",
                )

                return {
                    "events": []
                }

            fps = cap.get(
                cv2.CAP_PROP_FPS
            )

            if fps <= 0:
                fps = 25

            frame_index = 0

            if level["all_frames"]:

                sample = 1

            else:

                sample = max(
                    1,
                    int(
                        fps /
                        level["target_fps"]
                    ),
                )

            while True:

                ret, frame = cap.read()

                if (
                    not ret or
                    frame is None
                ):
                    log.info(
                        "DECODE END frame_index=%d ret=%s",
                        frame_index,
                        ret,
                    )
                    break

                self.metrics.mark_input()

                if (
                    frame_index % sample
                ) != 0:

                    frame_index += 1

                    continue

                self.process_person_frame(
                    frame,
                    camera=camera,
                    source_frame_id=frame_index,
                )

                frame_index += 1

            cap.release()

            events = self.run_recognition(
                camera
            )

            recognized = any(
                evt["type"] == "vision.identity"
                for evt in events
            )

            uncertain = any(
                evt["type"] == "vision.uncertain"
                for evt in events
            )

            if recognized:

                log.info(
                    "PASS=%s recognized identity",
                    level["name"],
                )

                final_events = events

                break

            if uncertain:

                log.info(
                    "PASS=%s uncertain identity",
                    level["name"],
                )

                final_events = events

                break

            if events:

                final_events = events

        log.info(
            "CLIP DONE final_events=%d",
            len(final_events),
        )

        self.tracker.reset()

        self.track_faces.clear()

        self.track_identity_memory.clear()

        self.track_recognition_buffers.clear()

        self.track_last_arcface.clear()

        self.last_detections.clear()

        self.last_yolo_frame = -999999

        self.active_tracks_seen.clear()

        self.frame_id = 0

        self.person_seen = False

        return {
            "events": final_events
        }
