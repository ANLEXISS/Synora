import os
import cv2
import numpy as np


class FaceDB:

    def __init__(self, app, faces_dir="/var/lib/synora/services/vision-worker/data/faces"):

        self.app = app
        self.db = self._build(faces_dir)

    def _normalize(self, v):
        return v / np.linalg.norm(v)

    def _build(self, faces_dir):

        db = {}

        for person in os.listdir(faces_dir):

            person_path = os.path.join(faces_dir, person)

            if not os.path.isdir(person_path):
                continue

            embeddings = []

            for img_name in os.listdir(person_path):

                img_path = os.path.join(person_path, img_name)

                img = cv2.imread(img_path)

                if img is None:
                    continue

                faces = self.app.get(img)

                if not faces:
                    continue

                emb = faces[0].embedding
                emb = self._normalize(emb)

                embeddings.append(emb)

            if embeddings:

                mean_embedding = np.mean(embeddings, axis=0)
                mean_embedding = self._normalize(mean_embedding)

                db[person] = {
                    "embeddings": embeddings,
                    "mean": mean_embedding
                }

                print(person, "loaded with", len(embeddings), "images")

        return db

    def identify(self, frame, threshold=0.5):

        faces = self.app.get(frame)

        if not faces:
            return None, 0

        emb = faces[0].embedding
        emb = self._normalize(emb)

        best_id = None
        best_score = 0

        for person, data in self.db.items():

            score = float(np.dot(emb, data["mean"]))

            if score > best_score:
                best_score = score
                best_id = person

        if best_score > threshold:
            return best_id, best_score

        return None, best_score
