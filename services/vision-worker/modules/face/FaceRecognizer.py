import cv2
import numpy as np
import os
import logging
import time

from core.model_runner import create_model_runner


log = logging.getLogger(
    "synora.vision.facerec"
)


class FaceRecognizer:

    def __init__(
        self,
        model_path="/var/lib/synora/models/arcface_w600k_r50.rknn",
        faces_dir="/opt/synora/services/vision-worker/data/faces",
        match_threshold=0.35,
        uncertain_threshold=0.20,
        debug_dir="/var/lib/synora/debug/arcface_runtime"
    ):

        log.info(
            "FACE RECOGNIZER INIT"
        )

        self.match_threshold = (
            match_threshold
        )

        self.uncertain_threshold = (
            uncertain_threshold
        )

        self.faces_dir = faces_dir

        self.debug_dir = debug_dir

        os.makedirs(
            self.debug_dir,
            exist_ok=True,
        )

        self.runner = create_model_runner(
            model_path
        )

        self.embedding_dim = 512

        log.info(
            "ARCFACE MODEL READY dim=%d backend=%s model=%s",
            self.embedding_dim,
            self.runner.backend,
            model_path,
        )

        self.resident_embeddings = {}

        self.debug_runtime_saves = True

        self.runtime_save_counter = 0

        self.load_faces(
            faces_dir
        )

    # ------------------------------------------------
    # DEBUG SAVE
    # ------------------------------------------------

    def save_runtime_face(
        self,
        face,
        prefix="runtime",
    ):

        if not self.debug_runtime_saves:
            return

        if face is None:
            return

        if face.size == 0:
            return

        try:

            ts = int(
                time.time() * 1000
            )

            path = os.path.join(
                self.debug_dir,
                f"{prefix}_{ts}_{self.runtime_save_counter}.jpg"
            )

            self.runtime_save_counter += 1

            cv2.imwrite(
                path,
                face,
            )

        except Exception:

            log.exception(
                "FAILED TO SAVE RUNTIME FACE"
            )

    # ------------------------------------------------
    # NORMALIZE
    # ------------------------------------------------

    def _normalize(
        self,
        v,
    ):

        v = np.asarray(
            v,
            dtype=np.float32,
        ).flatten()

        norm = np.linalg.norm(v)

        if norm == 0:
            return v

        return v / norm

    # ------------------------------------------------
    # UPSCALE SMALL FACES
    # ------------------------------------------------

    def improve_face_resolution(
        self,
        face,
    ):

        h, w = face.shape[:2]

        min_size = min(h, w)

        if min_size >= 112:
            return face

        scale = 112 / min_size

        new_w = int(w * scale)
        new_h = int(h * scale)

        face = cv2.resize(
            face,
            (new_w, new_h),
            interpolation=cv2.INTER_CUBIC,
        )

        return face

    # ------------------------------------------------
    # RAW EMBEDDING
    # ------------------------------------------------

    def _embed_raw(
        self,
        face,
    ):

        face = cv2.cvtColor(
            face,
            cv2.COLOR_BGR2RGB,
        )

        face = face.astype(
            np.float32
        )

        face = (
            face - 127.5
        ) * 0.0078125

        face = np.transpose(
            face,
            (2, 0, 1),
        )

        face = np.expand_dims(
            face,
            axis=0,
        )

        face = np.ascontiguousarray(
            face
        )

        outputs = self.runner.infer(
            face
        )

        if not outputs:

            log.error(
                "ARCFACE EMPTY OUTPUT"
            )

            return None

        emb = np.asarray(
            outputs[0],
            dtype=np.float32,
        ).flatten()

        if emb.size != self.embedding_dim:

            log.error(
                "INVALID EMBEDDING SIZE=%d",
                emb.size,
            )

            return None

        if np.isnan(emb).any():

            log.error(
                "EMBEDDING CONTAINS NAN"
            )

            return None

        return emb

    # ------------------------------------------------
    # MAIN EMBED
    # ------------------------------------------------

    def embed(
        self,
        face,
    ):

        if face is None:

            log.warning(
                "EMBED FACE NONE"
            )

            return None

        if face.size == 0:

            log.warning(
                "EMBED FACE EMPTY"
            )

            return None

        h, w = face.shape[:2]

        if h < 20 or w < 20:

            log.warning(
                "FACE TOO SMALL h=%d w=%d",
                h,
                w,
            )

            return None

        # ------------------------------------------------
        # SAVE RAW ROI BEFORE ANY PROCESSING
        # ------------------------------------------------

        self.save_runtime_face(
            face,
            "raw_roi",
        )

        face = self.improve_face_resolution(
            face
        )

        if (
            face.shape[0] != 112 or
            face.shape[1] != 112
        ):

            face = cv2.resize(
                face,
                (112, 112),
                interpolation=cv2.INTER_CUBIC,
            )

        self.save_runtime_face(
            face,
            "arcface_input",
        )

        try:

            face_flip = cv2.flip(
                face,
                1,
            )

            emb1 = self._embed_raw(
                face
            )

            emb2 = self._embed_raw(
                face_flip
            )

            if emb1 is None:
                return None

            if emb2 is None:
                return None

            emb = (
                emb1 + emb2
            ) / 2

            emb = self._normalize(
                emb
            )

            log.info(
                "ARCFACE EMBEDDING OK norm=%.4f",
                float(np.linalg.norm(emb)),
            )

            return emb

        except Exception:

            log.exception(
                "ARCFACE EMBED FAILED"
            )

            return None

    # ------------------------------------------------
    # LOAD DATASET
    # ------------------------------------------------

    def load_faces(
        self,
        faces_root,
    ):

        log.info(
            "LOADING FACE DATASET"
        )

        if not os.path.exists(
            faces_root
        ):

            log.warning(
                "faces_root missing -> %s",
                faces_root,
            )

            return

        residents = [

            r for r in os.listdir(
                faces_root
            )

            if os.path.isdir(
                os.path.join(
                    faces_root,
                    r,
                )
            )

            and not r.startswith(".")
        ]

        runtime_debug_state = (
            self.debug_runtime_saves
        )

        self.debug_runtime_saves = False

        for resident in residents:

            resident_dir = os.path.join(
                faces_root,
                resident,
            )

            base_dir = os.path.join(
                resident_dir,
                "base",
            )

            auto_dir = os.path.join(
                resident_dir,
                "auto",
            )

            base_embeddings = []

            auto_embeddings = []

            def load_dir(
                directory,
                target,
            ):

                if not os.path.isdir(
                    directory
                ):
                    return

                for img in os.listdir(
                    directory
                ):

                    if not img.lower().endswith(
                        (
                            ".jpg",
                            ".jpeg",
                            ".png",
                        )
                    ):
                        continue

                    path = os.path.join(
                        directory,
                        img,
                    )

                    frame = cv2.imread(
                        path
                    )

                    if frame is None:

                        log.warning(
                            "FAILED TO LOAD FACE=%s",
                            path,
                        )

                        continue

                    face = cv2.resize(
                        frame,
                        (112, 112),
                    )

                    emb = self.embed(
                        face
                    )

                    if emb is None:
                        continue

                    target.append(
                        emb
                    )

            load_dir(
                base_dir,
                base_embeddings,
            )

            load_dir(
                auto_dir,
                auto_embeddings,
            )

            self.resident_embeddings[
                resident
            ] = {
                "base": np.array(
                    base_embeddings,
                    dtype=np.float32,
                ),
                "auto": np.array(
                    auto_embeddings,
                    dtype=np.float32,
                ),
            }

            log.info(
                "RESIDENT=%s base=%d auto=%d",
                resident,
                len(base_embeddings),
                len(auto_embeddings),
            )

        self.debug_runtime_saves = (
            runtime_debug_state
        )

        log.info(
            "FACE DATASET READY residents=%d",
            len(
                self.resident_embeddings
            ),
        )

    # ------------------------------------------------
    # IDENTIFICATION
    # ------------------------------------------------

    def identify_embedding(
        self,
        embedding,
    ):

        if embedding is None:

            return (
                "unknown",
                None,
                0.0,
            )

        query = self._normalize(
            embedding
        ).astype("float32")

        best_identity = None

        best_score = 0.0

        for resident, embeddings in (
            self.resident_embeddings.items()
        ):

            base_embeddings = (
                embeddings["base"]
            )

            auto_embeddings = (
                embeddings["auto"]
            )

            score = 0.0

            # ------------------------------------------------
            # BASE EMBEDDINGS
            # ------------------------------------------------

            if len(base_embeddings) > 0:

                base_scores = np.dot(
                    base_embeddings,
                    query,
                )

                base_score = float(
                    np.max(base_scores)
                )

                score = base_score

            # ------------------------------------------------
            # AUTO EMBEDDINGS
            # ------------------------------------------------

            if len(auto_embeddings) > 0:

                auto_scores = np.dot(
                    auto_embeddings,
                    query,
                )

                auto_score = float(
                    np.max(auto_scores)
                )

                score = max(
                    score,
                    auto_score,
                )

            if score > best_score:

                best_score = score

                best_identity = resident

        # ------------------------------------------------
        # MATCH
        # ------------------------------------------------

        if (
            best_score >=
            self.match_threshold
        ):

            log.info(
                "MATCH resident=%s score=%.3f",
                best_identity,
                best_score,
            )

            return (
                "match",
                best_identity,
                best_score,
            )

        # ------------------------------------------------
        # UNCERTAIN
        # ------------------------------------------------

        if (
            best_score >=
            self.uncertain_threshold
        ):

            log.info(
                "UNCERTAIN resident=%s score=%.3f",
                best_identity,
                best_score,
            )

            return (
                "uncertain",
                best_identity,
                best_score,
            )

        log.info(
            "UNKNOWN score=%.3f",
            best_score,
        )

        return (
            "unknown",
            None,
            best_score,
        )
