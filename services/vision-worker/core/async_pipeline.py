import logging
import queue
import threading
import time


log = logging.getLogger("synora.vision.async_pipeline")


class BoundedStage:

    def __init__(self, name, capacity, metrics, handler):
        self.name = name
        self.capacity = capacity
        self.metrics = metrics
        self.handler = handler
        self.input = queue.Queue(maxsize=capacity)
        self.output = None
        self.thread = None
        self.running = False

    def connect(self, next_stage):
        self.output = next_stage
        return next_stage

    def put(self, item):
        try:
            self.input.put_nowait(item)
            self.metrics.queue(
                self.name,
                self.input.qsize(),
                self.capacity,
            )
            return True
        except queue.Full:
            self.metrics.incr("frames_dropped")
            self.metrics.error(
                self.name,
                "bounded queue full; item dropped",
                item.get("trace_id") if isinstance(item, dict) else None,
            )
            log.warning(
                "QUEUE DROP stage=%s capacity=%d",
                self.name,
                self.capacity,
            )
            return False

    def start(self):
        if self.running:
            return
        self.running = True
        self.thread = threading.Thread(
            target=self._run,
            name=f"vision-{self.name}",
            daemon=True,
        )
        self.thread.start()

    def stop(self):
        self.running = False
        if self.thread:
            self.thread.join(timeout=2.0)

    def _run(self):
        while self.running:
            try:
                item = self.input.get(timeout=0.2)
            except queue.Empty:
                continue

            start = time.perf_counter()
            try:
                result = self.handler(item)
                self.metrics.timing(
                    self.name,
                    (time.perf_counter() - start) * 1000.0,
                )
                if self.output and result is not None:
                    self.output.put(result)
            except Exception as exc:
                trace_id = (
                    item.get("trace_id")
                    if isinstance(item, dict)
                    else None
                )
                self.metrics.error(
                    self.name,
                    str(exc),
                    trace_id,
                )
                log.exception(
                    "ASYNC STAGE FAILURE stage=%s trace=%s",
                    self.name,
                    trace_id,
                )
            finally:
                self.input.task_done()
                self.metrics.queue(
                    self.name,
                    self.input.qsize(),
                    self.capacity,
                )


class AsyncVisionPipeline:

    def __init__(self, metrics, handlers, capacity=4):
        self.metrics = metrics
        self.rtsp = BoundedStage(
            "rtsp",
            capacity,
            metrics,
            handlers["rtsp"],
        )
        self.yolo = self.rtsp.connect(BoundedStage(
            "yolo",
            capacity,
            metrics,
            handlers["yolo"],
        ))
        self.scrfd = self.yolo.connect(BoundedStage(
            "scrfd",
            capacity,
            metrics,
            handlers["scrfd"],
        ))
        self.arcface = self.scrfd.connect(BoundedStage(
            "arcface",
            capacity,
            metrics,
            handlers["arcface"],
        ))
        self.event = self.arcface.connect(BoundedStage(
            "event",
            capacity,
            metrics,
            handlers["event"],
        ))
        self.stages = [
            self.rtsp,
            self.yolo,
            self.scrfd,
            self.arcface,
            self.event,
        ]

    def start(self):
        for stage in reversed(self.stages):
            stage.start()

    def stop(self):
        for stage in self.stages:
            stage.stop()
