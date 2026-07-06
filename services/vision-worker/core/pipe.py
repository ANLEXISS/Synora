import cv2
import numpy as np
import time
import logging
import uuid
import os

from modules.detect.face_detector import FaceDetector
from modules.tracking.tracker import Tracker
from modules.predict.face_roi_predictor import FaceROIPredictor
from core.track_context import TrackContext
from core.events import EventBuilder

log = logging.getLogger("synora.vision.pipeline")


class VisionPipeline:

    TARGET_FPS = 5
    PERSON_DETECT_INTERVAL = 3
    FACE_DETECT_INTERVAL = 3

    DETECT_SCALE = 0.5

    MAX_DETECTIONS = 3
    MAX_TRACKS_ANALYZED = 3
    MAX_TRACK_CONTEXTS = 32
    MAX_FACE_CANDIDATES = 8

    FACE_MIN_SIZE = 60

    CROP_DIR = os.path.expanduser("~/Synora/vision_faces")

    ARCFACE_TEMPLATE = np.array([
        [38.2946, 51.6963],
        [73.5318, 51.5014],
        [56.0252, 71.7366],
        [41.5493, 92.3655],
        [70.7299, 92.2041]
    ], dtype=np.float32)

    def __init__(self, face_recognizer, person_detector):

        log.info("PIPELINE INIT")

        cv2.setNumThreads(1)
        cv2.ocl.setUseOpenCL(False)

        self.face_recognizer = face_recognizer
        self.person_detector = person_detector
        self.face_detector = FaceDetector()

        self.tracker = Tracker()
        self.roi_predictor = FaceROIPredictor()

        self.frame_id = 0

        self.track_contexts = {}
        self.last_detections = []

        self.debug_display = True

        os.makedirs(self.CROP_DIR, exist_ok=True)

    # ------------------------------------------------

    def ensure_context(self, track_id):

        ctx = self.track_contexts.get(track_id)

        if ctx:
            return ctx

        if len(self.track_contexts) >= self.MAX_TRACK_CONTEXTS:
            self.track_contexts.pop(next(iter(self.track_contexts)))

        ctx = TrackContext(track_id)
        ctx.candidates = []
        ctx.face_detect_counter = 0

        self.track_contexts[track_id] = ctx

        return ctx

    # ------------------------------------------------

    def detect_persons(self, frame):

        frame_small = cv2.resize(
            frame,
            None,
            fx=self.DETECT_SCALE,
            fy=self.DETECT_SCALE
        )

        persons_small = self.person_detector.detect(frame_small)

        detections = []

        for p in persons_small:

            x1, y1, x2, y2 = p["bbox"]

            detections.append([
                int(x1 / self.DETECT_SCALE),
                int(y1 / self.DETECT_SCALE),
                int(x2 / self.DETECT_SCALE),
                int(y2 / self.DETECT_SCALE)
            ])

        return detections[:self.MAX_DETECTIONS]

    # ------------------------------------------------

    def process_person_frame(self, frame):

        self.frame_id += 1
        display = frame.copy()

        if self.frame_id % self.PERSON_DETECT_INTERVAL == 0:

            detections = self.detect_persons(frame)
            self.last_detections = detections

        else:

            detections = self.last_detections

        tracks = self.tracker.update(detections)

        log.info(
            "FRAME=%d detections=%d tracks=%d",
            self.frame_id,
            len(detections),
            len(tracks)
        )

        # ------------------------------------------------
        # DRAW PERSON TRACKS
        # ------------------------------------------------

        for t in tracks:

            x1,y1,x2,y2 = map(int,t.box)

            cv2.rectangle(display,(x1,y1),(x2,y2),(0,255,0),2)

            cv2.putText(
                display,
                f"id {t.id}",
                (x1,y1-5),
                cv2.FONT_HERSHEY_SIMPLEX,
                0.5,
                (0,255,0),
                1
            )

        active_ids = {t.id for t in tracks}

        self.track_contexts = {
            tid: ctx for tid, ctx in self.track_contexts.items()
            if tid in active_ids
        }

        for track in tracks:
            self.ensure_context(track.id)

        tracks = sorted(
            tracks,
            key=lambda t: (t.box[2]-t.box[0])*(t.box[3]-t.box[1]),
            reverse=True
        )[:self.MAX_TRACKS_ANALYZED]

        # ------------------------------------------------
        # FACE DETECTION ROI PAR PERSONNE
        # ------------------------------------------------

        for track in tracks:

            ctx = self.ensure_context(track.id)

            ctx.face_detect_counter += 1

            if ctx.face_detect_counter % self.FACE_DETECT_INTERVAL != 0:
                continue

            x1,y1,x2,y2 = map(int,track.box)

            h,w = frame.shape[:2]

            x1=max(0,x1)
            y1=max(0,y1)
            x2=min(w,x2)
            y2=min(h,y2)

            if (x2-x1) < 80 or (y2-y1) < 80:
                continue

            roi = frame[y1:y2, x1:x2]

            faces = self.face_detector.detect(roi)

            if not faces:
                continue

            for face in faces:

                if face["landmarks"] is None:
                    continue   
                
                fx1,fy1,fx2,fy2 = face["bbox"]

                # remap ROI -> frame global
                fx1 += x1
                fy1 += y1
                fx2 += x1
                fy2 += y1

                if (fx2-fx1) < self.FACE_MIN_SIZE:
                    continue

                cv2.rectangle(display,(fx1,fy1),(fx2,fy2),(255,0,0),2)

                aligned = self.align_face_arcface(
                    frame,
                    [(lx+x1,ly+y1) for (lx,ly) in face["landmarks"]]
                )

                if aligned is None:
                    continue

                blur = self.face_quality(aligned)

                ctx.candidates.append((blur,aligned))

                if len(ctx.candidates) > self.MAX_FACE_CANDIDATES:
                    ctx.candidates.pop(0)

        # ------------------------------------------------
        # DEBUG DISPLAY
        # ------------------------------------------------

        if self.debug_display:

            cv2.imshow("Synora Vision Debug", display)

            if cv2.waitKey(1) == 27:
                cv2.destroyAllWindows()
                exit()

    # ------------------------------------------------

    def face_quality(self, face):

        gray=cv2.cvtColor(face,cv2.COLOR_BGR2GRAY)

        return cv2.Laplacian(gray,cv2.CV_64F).var()

    # ------------------------------------------------

    def align_face_arcface(self, frame, landmarks):

        src=np.array(landmarks,dtype=np.float32)

        M,_=cv2.estimateAffinePartial2D(src,self.ARCFACE_TEMPLATE)

        if M is None:
            return None

        return cv2.warpAffine(frame,M,(112,112))

    # ------------------------------------------------

    def run_recognition(self, camera, scene_id):

        builder = EventBuilder()
        events = []

        persons = len(self.track_contexts)

        for ctx in self.track_contexts.values():

            if not ctx.candidates:

                events.append(
                    builder.unknown(camera,scene_id,ctx.id,persons)
                )
                continue

            best_blur, best = max(ctx.candidates, key=lambda x: x[0])

            self.save_face_crop(best,camera,scene_id,ctx.id,best_blur)

            emb = self.face_recognizer.embed(best)
            emb = self.face_recognizer._normalize(emb)

            status, identity, score = self.face_recognizer.identify_embedding(emb)

            if status == "match":

                event = builder.identity(
                    camera,scene_id,ctx.id,identity,score,persons
                )

            elif status == "uncertain":

                event = builder.uncertain(
                    camera,scene_id,ctx.id,identity,score,persons
                )

            else:

                event = builder.unknown(
                    camera,scene_id,ctx.id,persons
                )

            events.append(event)

        return events

    # ------------------------------------------------

    def process_clip(self, clip_path, camera):

        scene_id=uuid.uuid4().hex[:12]

        cap=cv2.VideoCapture(clip_path)

        if not cap.isOpened():
            log.error("VIDEO OPEN FAILED -> %s",clip_path)
            return {"events":[]}

        start=time.time()

        fps=cap.get(cv2.CAP_PROP_FPS)

        frame_index=0
        sample=max(1,int(fps/self.TARGET_FPS))

        while True:

            ret,frame=cap.read()

            if not ret:
                break

            if frame_index % sample != 0:
                frame_index+=1
                continue

            self.process_person_frame(frame)

            frame_index+=1

        cap.release()

        events=self.run_recognition(camera,scene_id)

        elapsed=time.time()-start

        log.info(
            "CLIP DONE camera=%s scene=%s time=%.2fs events=%d",
            camera,
            scene_id,
            elapsed,
            len(events)
        )

        self.tracker.reset()
        self.track_contexts.clear()

        return {
            "type":"vision.clip",
            "source":camera,
            "scene_id":scene_id,
            "events":events
        }

    # ------------------------------------------------

    def save_face_crop(self, face, camera, scene_id, track_id, blur):

        ts = int(time.time() * 1000)

        cam_dir = os.path.join(self.CROP_DIR, camera, scene_id)

        os.makedirs(cam_dir, exist_ok=True)

        filename = f"track{track_id}_blur{int(blur)}_{ts}.jpg"

        path = os.path.join(cam_dir, filename)

        try:

            cv2.imwrite(path, face)

            log.info("FACE CROP SAVED -> %s", path)

        except Exception:

            log.exception("FAILED TO SAVE FACE CROP")