import argparse
import json
import logging
import os
import signal
import socket
import sys
import threading

from core.events import ALLOWED_EVENT_TYPES, EventBuilder


SOCKET_PATH = "/run/synora/vision-worker.sock"


logging.basicConfig(
    level=logging.INFO,
    format="[%(asctime)s] [%(levelname)s] [%(name)s] %(message)s"
)

log = logging.getLogger(
    "synora.vision"
)


class VisionWorker:

    def __init__(self, dry_run=False):

        log.info(
            "VISION WORKER INIT"
        )

        self.dry_run = dry_run

        if dry_run:
            self.face_recognizer = None
            self.person_detector = None
            self.pipeline = None
        else:
            from core.pipeline import VisionPipeline
            from modules.detect.person_detector import PersonDetector
            from modules.face.FaceRecognizer import FaceRecognizer

            self.face_recognizer = (
                FaceRecognizer()
            )

            self.person_detector = (
                PersonDetector()
            )

            self.pipeline = VisionPipeline(
                self.face_recognizer,
                self.person_detector,
            )

        self.debug_app = self.create_debug_app()

        self.debug_thread = threading.Thread(
            target=self.debug_app.run,
            kwargs={
                "host": "127.0.0.1",
                "port": 8094,
                "threaded": True,
                "use_reloader": False,
            },
            daemon=True,
        )

        self.debug_thread.start()

    # ------------------------------------------------

    def create_debug_app(self):

        from flask import Flask, jsonify

        app = Flask(__name__)

        @app.get("/debug/pipeline")
        def pipeline_debug():
            if self.pipeline is None:
                return jsonify({
                    "dry_run": True,
                })

            return jsonify(
                self.pipeline.dashboard()
            )

        return app

    # ------------------------------------------------

    def process_request(
        self,
        req,
    ):

        clip_path = req.get(
            "clip_path"
        )

        camera_id = (
            req.get("camera_id") or
            req.get("camera") or
            "unknown"
        )

        clip_id = (
            req.get("clip_id") or
            req.get("id")
        )

        node_id = req.get(
            "node_id"
        )

        device_id = (
            req.get("device_id") or
            camera_id
        )

        if not clip_path:

            return {
                "error": "missing clip_path"
            }

        log.info(
            "PROCESS CLIP camera=%s path=%s",
            camera_id,
            clip_path,
        )

        if self.dry_run:
            result = {
                "events": [
                    build_dry_run_event(
                        clip_path=clip_path,
                        camera_id=camera_id,
                        clip_id=clip_id,
                        node_id=node_id,
                        device_id=device_id,
                        event_kind=req.get("debug_event", "unknown"),
                    )
                ]
            }
        else:
            result = self.pipeline.process_clip(
                clip_path,
                camera_id,
                clip_id=clip_id,
                node_id=node_id,
                device_id=device_id,
            )

        log.info(
            "PIPELINE RESULT keys=%s",
            list(result.keys()) if result else None,
        )

        events = result.get(
            "events",
            [],
        )

        log.info(
            "PIPELINE EVENTS RAW=%s",
            events,
        )

        if not result:

            return {
                "events": []
            }

        events = result.get(
            "events",
            [],
        )

        log.info(
            "WORKER RETURN events=%d",
            len(events),
        )

        return {
            "events": events
        }

    # ------------------------------------------------

    def start(self):

        if os.path.exists(
            SOCKET_PATH
        ):

            os.remove(
                SOCKET_PATH
            )

        server = socket.socket(
            socket.AF_UNIX,
            socket.SOCK_STREAM,
        )

        server.bind(
            SOCKET_PATH
        )

        server.listen(1)

        log.info(
            "VISION IPC READY socket=%s",
            SOCKET_PATH,
        )

        while True:

            conn, _ = server.accept()

            log.info(
                "IPC CLIENT CONNECTED"
            )

            with conn:

                reader = conn.makefile("r")

                writer = conn.makefile("w")

                while True:

                    line = reader.readline()

                    if not line:
                        break

                    try:

                        req = json.loads(
                            line
                        )

                        resp = self.process_request(
                            req
                        )

                    except Exception as e:

                        log.exception(
                            "PROCESS ERROR"
                        )

                        resp = {
                            "error": str(e)
                        }

                    writer.write(
                        json.dumps(resp) + "\n"
                    )

                    writer.flush()


# ------------------------------------------------
# SIGNALS
# ------------------------------------------------

def shutdown(
    signum,
    frame,
):

    log.info(
        "Shutdown signal received"
    )

    try:

        os.remove(
            SOCKET_PATH
        )

    except:
        pass

    sys.exit(0)


signal.signal(
    signal.SIGTERM,
    shutdown,
)

signal.signal(
    signal.SIGINT,
    shutdown,
)


def build_dry_run_event(
    clip_path,
    camera_id,
    clip_id=None,
    node_id=None,
    device_id=None,
    event_kind="unknown",
):

    camera_id = camera_id or "unknown"
    clip_id = (
        clip_id or
        os.path.splitext(
            os.path.basename(clip_path)
        )[0] or
        "dry-run"
    )

    builder = EventBuilder({
        "camera_id": camera_id,
        "device_id": device_id or camera_id,
        "node_id": node_id,
        "clip_id": clip_id,
        "clip_path": clip_path,
    })

    scene_id = (
        clip_id or
        "dry-run"
    )

    event_kind = (
        event_kind or
        "unknown"
    ).strip().lower()

    if event_kind == "identity":
        return builder.identity(
            camera_id,
            scene_id,
            "dry-run-track",
            "dry-run-resident",
            0.99,
            1,
        )

    if event_kind == "uncertain":
        return builder.uncertain(
            camera_id,
            scene_id,
            "dry-run-track",
            "dry-run-resident",
            0.42,
            1,
        )

    return builder.unknown(
        camera_id,
        scene_id,
        "dry-run-track",
        1,
    )


def validate_event_contract(event):

    if event.get("type") not in ALLOWED_EVENT_TYPES:
        raise AssertionError(f"unexpected event type: {event.get('type')}")

    for key in (
        "type",
        "source",
        "timestamp",
        "payload",
    ):
        if key not in event:
            raise AssertionError(f"missing event key: {key}")

    payload = event["payload"]

    for key in (
        "camera_id",
        "device_id",
        "clip_id",
        "clip_path",
        "timestamp",
        "confidence",
    ):
        if key not in payload:
            raise AssertionError(f"missing payload key: {key}")


def run_self_test():

    for event_kind in (
        "unknown",
        "identity",
        "uncertain",
    ):
        event = build_dry_run_event(
            clip_path="/tmp/synora-self-test.mp4",
            camera_id="cam_01",
            clip_id="clip_self_test",
            node_id="node_01",
            device_id="device_01",
            event_kind=event_kind,
        )

        validate_event_contract(
            event
        )

        if event["clip_id"] != "clip_self_test":
            raise AssertionError("clip_id not propagated top-level")

        if event["payload"]["clip_id"] != "clip_self_test":
            raise AssertionError("clip_id not propagated in payload")

        expected_type = f"vision.{event_kind}"

        if event["type"] != expected_type:
            raise AssertionError(
                f"expected {expected_type}, got {event['type']}"
            )

    print("vision-worker self-test ok")


def parse_args(argv):

    parser = argparse.ArgumentParser(
        description="Synora Vision Worker"
    )

    parser.add_argument(
        "--self-test",
        action="store_true",
        help="run local event-format checks without RKNN inference",
    )

    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="produce a simulated vision event without RKNN inference",
    )

    parser.add_argument(
        "--clip",
        help="clip path for dry-run one-shot mode",
    )

    parser.add_argument(
        "--camera",
        default="cam_01",
        help="camera id for dry-run one-shot mode",
    )

    parser.add_argument(
        "--clip-id",
        help="clip id for dry-run one-shot mode",
    )

    parser.add_argument(
        "--node-id",
        help="node id for dry-run one-shot mode",
    )

    parser.add_argument(
        "--device-id",
        help="device id for dry-run one-shot mode",
    )

    parser.add_argument(
        "--debug-event",
        choices=("unknown", "identity", "uncertain"),
        default="unknown",
        help="simulated event kind for dry-run",
    )

    return parser.parse_args(
        argv
    )


# ------------------------------------------------
# MAIN
# ------------------------------------------------

if __name__ == "__main__":

    args = parse_args(
        sys.argv[1:]
    )

    if args.self_test:
        run_self_test()
        sys.exit(0)

    if args.dry_run and args.clip:
        event = build_dry_run_event(
            clip_path=args.clip,
            camera_id=args.camera,
            clip_id=args.clip_id,
            node_id=args.node_id,
            device_id=args.device_id,
            event_kind=args.debug_event,
        )
        print(json.dumps({
            "events": [
                event
            ]
        }))
        sys.exit(0)

    worker = VisionWorker(
        dry_run=args.dry_run,
    )

    worker.start()
