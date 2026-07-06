from core.events import EventBuilder
from modules.scene.weapon import WeaponDetector
from modules.scene.fall import FallDetector


class SceneManager:

    def __init__(self):

        self.weapon = WeaponDetector()
        self.fall = FallDetector()

    def analyze(self, camera, scene_id, frame, tracks, contexts):

        builder = EventBuilder()
        events = []

        # -----------------------------
        # PRIORITÉ : DETECTION ARME
        # -----------------------------

        weapons = self.weapon.detect(frame, tracks)

        if weapons:

            for hit in weapons:

                track_id = hit["track"]
                confidence = hit["confidence"]

                ctx = contexts.get(track_id)

                if ctx is None:
                    continue

                identity = ctx.identity

                # Arme sur personne inconnue → danger max
                if identity in [None, "unknown"]:

                    events.append(
                        builder.weapon_firearm(
                            camera,
                            scene_id,
                            track_id,
                            confidence
                        )
                    )

                    # on stop tout le reste
                    return events

                # arme détectée sur résident
                events.append(
                    builder.weapon_firearm(
                        camera,
                        scene_id,
                        track_id,
                        confidence
                    )
                )

        # -----------------------------
        # FALL DETECTOR
        # -----------------------------

        falls = self.fall.detect(tracks)

        for f in falls:

            track_id = f["track"]

            ctx = contexts.get(track_id)

            if ctx is None:
                continue

            if ctx.identity not in [None, "unknown"]:

                events.append(
                    builder.fall_detected(
                        camera,
                        scene_id,
                        track_id
                    )
                )

        return events
