import json
import logging
import os
import signal
import socket
import sys
import threading

from core.pipeline import VisionPipeline
from modules.detect.person_detector import PersonDetector
from modules.face.FaceRecognizer import FaceRecognizer
from flask import Flask, jsonify


SOCKET_PATH = "/run/synora/vision-worker.sock"


logging.basicConfig(
    level=logging.INFO,
    format="[%(asctime)s] [%(levelname)s] [%(name)s] %(message)s"
)

log = logging.getLogger(
    "synora.vision"
)


class VisionWorker:

    def __init__(self):

        log.info(
            "VISION WORKER INIT"
        )

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

        app = Flask(__name__)

        @app.get("/debug/pipeline")
        def pipeline_debug():
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

        camera_id = req.get(
            "camera_id"
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

        result = self.pipeline.process_clip(
            clip_path,
            camera_id,
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


# ------------------------------------------------
# MAIN
# ------------------------------------------------

if __name__ == "__main__":

    worker = VisionWorker()

    worker.start()
