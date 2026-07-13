import logging
import os
import numpy as np

try:
    from rknnlite.api import RKNNLite
except Exception as e:
    _RKNN_IMPORT_ERROR = e
    RKNNLite = None
else:
    _RKNN_IMPORT_ERROR = None

log = logging.getLogger(
    "synora.vision.model_runner"
)


class ModelUnavailableError(RuntimeError):
    """A model cannot be used, but this is not a worker-fatal error."""

    def __init__(self, code, model_path, message):
        super().__init__(message)
        self.code = code
        self.model_path = model_path
        self.message = message

    def as_dict(self):
        return {
            "status": "unavailable",
            "code": self.code,
            "path": self.model_path,
            "error": self.message,
        }

def rknn_available():
    if RKNNLite is None:
        return False

    try:
        test = RKNNLite()
        del test
        return True
    except Exception:
        return False


def model_status(model_path):
    """Return a cheap startup diagnostic without loading the model."""
    if not model_path:
        return {"status": "missing", "path": model_path or "", "error": "model path is empty"}
    if not os.path.isfile(model_path):
        return {"status": "missing", "path": model_path, "error": "model file is missing"}
    if not model_path.endswith(".rknn"):
        return {"status": "invalid", "path": model_path, "error": "RKNN model extension required"}
    if RKNNLite is None:
        return {"status": "unavailable", "path": model_path, "error": f"RKNNLite unavailable: {_RKNN_IMPORT_ERROR}"}
    return {"status": "present", "path": model_path}

class ModelRunner:

    backend = "unknown"

    def infer(
        self,
        input_tensor,
    ):
        raise NotImplementedError

    def close(self):
        pass


class RKNNRunner(ModelRunner):

    backend = "rknn"

    def __init__(
        self,
        model_path,
        core_mask,
    ):

        if RKNNLite is None:
            raise ModelUnavailableError(
                "backend_unavailable",
                model_path,
                f"RKNNLite unavailable: {_RKNN_IMPORT_ERROR}",
            )

        self.model_path = model_path
        self.core_mask = core_mask
        self._logged_outputs = False

        self.rknn = RKNNLite()

        ret = self.rknn.load_rknn(
            model_path
        )

        if ret != 0:
            raise ModelUnavailableError(
                "invalid_model",
                model_path,
                f"RKNN load failed ret={ret}",
            )

        ret = self.rknn.init_runtime(
            core_mask=core_mask
        )

        if ret != 0:
            raise ModelUnavailableError(
                "rknn_runtime_error",
                model_path,
                f"RKNN init runtime failed ret={ret}",
            )

        log.warning(
            "RKNN backend ready model=%s",
            model_path,
        )

    def infer(
        self,
        input_tensor,
    ):

        tensor = np.ascontiguousarray(
            input_tensor
        )

        outputs = self.rknn.inference(
            inputs=[tensor]
        )

        if (
            not self._logged_outputs
            and outputs
        ):

            shapes = [
                np.asarray(o).shape
                for o in outputs
            ]

            log.warning(
                "RKNN output shapes=%s",
                shapes,
            )

            self._logged_outputs = True

        return outputs

    def close(self):

        try:
            self.rknn.release()
        except Exception:
            pass

def create_model_runner(
    model_path,
):


    backend = os.getenv(
        "VISION_BACKEND",
        "auto"
    ).lower()

    force_cpu = os.getenv(
        "VISION_FORCE_CPU",
        "0"
    ) == "1"

    if force_cpu:
        log.warning(
            "VISION_FORCE_CPU ignored: RKNN runtime is required"
        )

    log.warning(
        "Requested backend=%s model=%s",
        backend,
        model_path,
    )

    if backend not in (
        "auto",
        "rknn",
    ):
        raise ModelUnavailableError(
            "backend_unavailable",
            model_path,
            f"Unsupported backend: {backend}; RKNN is required",
        )

    if not model_path.endswith(
        ".rknn"
    ):
        raise ModelUnavailableError(
            "invalid_model",
            model_path,
            f"RKNN model required: {model_path}",
        )

    if not os.path.isfile(model_path):
        raise ModelUnavailableError(
            "missing_file",
            model_path,
            f"RKNN model file missing: {model_path}",
        )

    if RKNNLite is None:
        raise ModelUnavailableError(
            "backend_unavailable",
            model_path,
            f"RKNNLite unavailable: {_RKNN_IMPORT_ERROR}",
        )

    core_mask = getattr(
        RKNNLite,
        "NPU_CORE_0_1_2",
        None,
    )

    if core_mask is None:
        core_mask = RKNNLite.NPU_CORE_0

    return RKNNRunner(
        model_path,
        core_mask,
    )
