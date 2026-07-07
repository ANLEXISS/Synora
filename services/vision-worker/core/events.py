import time


ALLOWED_EVENT_TYPES = {
    "vision.identity",
    "vision.unknown",
    "vision.uncertain",
    "vision.motion",
    "vision.weapon",
    "vision.fall",
    "vision.tamper",
}


class EventBuilder:

    VERSION = 1

    def __init__(self, context=None):

        self.context = dict(context or {})

    # ------------------------------------------------

    def set_context(self, **context):

        clean = {
            key: value
            for key, value in context.items()
            if value is not None and value != ""
        }

        self.context = clean

    # ------------------------------------------------

    def clear_context(self):

        self.context = {}

    # ------------------------------------------------

    def _base(self, event_type, camera, scene_id, track_id=None, payload=None):

        if event_type not in ALLOWED_EVENT_TYPES:
            raise ValueError(f"unsupported vision event type: {event_type}")

        payload = payload or {}

        camera_id = self.context.get("camera_id") or camera
        device_id = self.context.get("device_id") or camera_id

        payload.setdefault("camera_id", camera_id)
        payload.setdefault("camera", camera_id)
        payload.setdefault("device_id", device_id)
        payload.setdefault("scene_id", scene_id)

        for key in (
            "node_id",
            "clip_id",
            "clip_path",
        ):
            value = self.context.get(key)
            if value is not None and value != "":
                payload.setdefault(key, value)

        if track_id is not None:
            payload.setdefault("track_id", str(track_id))

        timestamp = time.time()
        payload.setdefault("timestamp", timestamp)

        event = {
            "type": event_type,
            "source": device_id,
            "camera_id": camera_id,
            "device_id": device_id,
            "scene_id": scene_id,
            "timestamp": timestamp,
            "version": self.VERSION,
            "payload": payload
        }

        if self.context.get("node_id"):
            event["node_id"] = self.context["node_id"]

        if self.context.get("clip_id"):
            event["clip_id"] = self.context["clip_id"]

        if track_id is not None:
            event["track_id"] = str(track_id)

        identity = payload.get("identity")
        if identity:
            event["identity"] = identity

        if "confidence" in payload:
            event["confidence"] = float(payload["confidence"])

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
            scene_id,
            payload={
                "motion": True,
                "confidence": 1.0,
            },
        )

    # ------------------------------------------------

    def tamper(self, camera, scene_id):

        return self._base(
            "vision.tamper",
            camera,
            scene_id,
            payload={
                "tamper": True,
                "confidence": 1.0,
            },
        )

    # ------------------------------------------------

    def weapon(self, camera, scene_id, track_id=None, confidence=0.0, weapon_type="weapon"):

        return self._base(
            "vision.weapon",
            camera,
            scene_id,
            track_id,
            {
                "weapon": weapon_type,
                "weapon_type": weapon_type,
                "confidence": float(confidence),
            }
        )

    # ------------------------------------------------

    def weapon_firearm(self, camera, scene_id, track_id=None, confidence=0.0):

        return self.weapon(
            camera,
            scene_id,
            track_id,
            confidence,
            "firearm",
        )

    # ------------------------------------------------

    def fall(self, camera, scene_id, track_id=None, confidence=1.0):

        return self._base(
            "vision.fall",
            camera,
            scene_id,
            track_id,
            {
                "fall": True,
                "confidence": float(confidence),
            }
        )

    # ------------------------------------------------

    def fall_detected(self, camera, scene_id, track_id=None, confidence=1.0):

        return self.fall(
            camera,
            scene_id,
            track_id,
            confidence,
        )
