import time


class EventBuilder:

    VERSION = 1

    # ------------------------------------------------

    def _base(self, event_type, camera, scene_id, track_id=None, payload=None):

        payload = payload or {}
        payload.setdefault("camera_id", camera)
        payload.setdefault("camera", camera)
        payload.setdefault("scene_id", scene_id)

        if track_id is not None:
            payload.setdefault("track_id", str(track_id))

        event = {
            "type": event_type,
            "source": camera,
            "scene_id": scene_id,
            "timestamp": time.time(),
            "version": self.VERSION,
            "payload": payload
        }

        if track_id is not None:
            event["track_id"] = track_id

        return event

    # ------------------------------------------------

    def _scene(self, weapon=False, weapon_confidence=0.0, fall=False):

        return {
            "weapon": bool(weapon),
            "weapon_confidence": float(weapon_confidence),
            "fall": bool(fall)
        }

    # ------------------------------------------------

    def identity(self, camera, scene_id, track_id, person, score, persons,
                 weapon=False, weapon_confidence=0.0, fall=False):

        payload = {
            "identity": person,
            "resident_id": person,
            "confidence": float(score),
            "persons": int(persons),
            "scene": self._scene(weapon, weapon_confidence, fall)
        }

        return self._base(
            "vision.identity",
            camera,
            scene_id,
            track_id,
            payload
        )

    # ------------------------------------------------

    def unknown(self, camera, scene_id, track_id, persons,
                weapon=False, weapon_confidence=0.0, fall=False):

        payload = {
            "identity": "unknown",
            "confidence": 0.0,
            "persons": int(persons),
            "scene": self._scene(weapon, weapon_confidence, fall)
        }

        return self._base(
            "vision.unknown",
            camera,
            scene_id,
            track_id,
            payload
        )

    # ------------------------------------------------

    def uncertain(self, camera, scene_id, track_id, identity, score, persons,
                  weapon=False, weapon_confidence=0.0, fall=False):

        payload = {
            "identity": "uncertain",
            "best_match": identity,
            "resident_id": identity,
            "confidence": float(score),
            "persons": int(persons),
            "scene": self._scene(weapon, weapon_confidence, fall)
        }

        return self._base(
            "vision.uncertain",
            camera,
            scene_id,
            track_id,
            payload
        )

    # ------------------------------------------------

    def motion(self, camera, scene_id):

        return self._base(
            "vision.motion",
            camera,
            scene_id
        )

    # ------------------------------------------------

    def tamper(self, camera, scene_id):

        return self._base(
            "vision.tamper",
            camera,
            scene_id
        )

    # ------------------------------------------------

    def fight(self, camera, scene_id):

        return self._base(
            "vision.fight",
            camera,
            scene_id
        )
