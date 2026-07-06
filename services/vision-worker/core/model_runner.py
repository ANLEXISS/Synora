import logging
import os
import numpy as np

try:
    import onnxruntime as ort
except ImportError:
    ort = None

try:
    from rknnlite.api import RKNNLite
except Exception as e:
    print("RKNN IMPORT ERROR:", e)
    RKNNLite = None

log = logging.getLogger(
    "synora.vision.model_runner"
)

def rknn_available():

    if RKNNLite is None:
        return False

    try:
        test = RKNNLite()
        del test
        return True

    except Exception:
        return False

class ModelRunner:

    backend = "unknown"

    def infer(
        self,
        input_tensor,
    ):
        raise NotImplementedError

    def close(self):
        pass


class ONNXRunner(ModelRunner):

    backend = "onnx"

    def __init__(
        self,
        model_path,
    ):

        if ort is None:
            raise RuntimeError(
                "onnxruntime not installed"
            )

        if not os.path.exists(
            model_path
        ):
            raise RuntimeError(
                f"ONNX model missing: {model_path}"
            )

        so = ort.SessionOptions()

        so.intra_op_num_threads = 1
        so.inter_op_num_threads = 1

        self.session = ort.InferenceSession(
            model_path,
            sess_options=so,
            providers=[
                "CPUExecutionProvider"
            ],
        )

        self.input_name = (
            self.session.get_inputs()[0].name
        )

        self.output_names = [
            o.name
            for o in self.session.get_outputs()
        ]

        log.warning(
            "ONNX backend ready model=%s",
            model_path,
        )

    def infer(
        self,
        input_tensor,
    ):

        tensor = np.ascontiguousarray(
            input_tensor
        )

        return self.session.run(
            self.output_names,
            {
                self.input_name: tensor
            },
        )

class RKNNRunner(ModelRunner):

    backend = "rknn"

    def __init__(
        self,
        model_path,
        core_mask,
    ):

        if RKNNLite is None:
            raise RuntimeError(
                "RKNNLite unavailable"
            )

        self.model_path = model_path
        self.core_mask = core_mask
        self._logged_outputs = False

        self.rknn = RKNNLite()

        ret = self.rknn.load_rknn(
            model_path
        )

        if ret != 0:
            raise RuntimeError(
                f"RKNN load failed ret={ret}"
            )

        ret = self.rknn.init_runtime(
            core_mask=core_mask
        )

        if ret != 0:
            raise RuntimeError(
                f"RKNN init runtime failed ret={ret}"
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
        backend = "onnx"

    log.warning(
        "Requested backend=%s model=%s",
        backend,
        model_path,
    )

    # ----------------------------
    # FORCE RKNN
    # ----------------------------

    if backend == "rknn":

        if RKNNLite is None:
            raise RuntimeError(
                "RKNN requested but unavailable"
            )

        core_mask = getattr(
            RKNNLite,
            "NPU_CORE_0_1_2",
            RKNNLite.NPU_CORE_0,
        )

        return RKNNRunner(
            model_path,
            core_mask,
        )

    # ----------------------------
    # FORCE ONNX
    # ----------------------------

    if backend == "onnx":

        onnx_model = model_path.replace(
            ".rknn",
            ".onnx",
        )

        return ONNXRunner(
            onnx_model
        )

    # ----------------------------
    # AUTO
    # ----------------------------

    if backend == "auto":

        if (
            model_path.endswith(".rknn")
            and rknn_available()
        ):

            try:

                log.warning(
                    "AUTO selected RKNN"
                )

                core_mask = getattr(
                    RKNNLite,
                    "NPU_CORE_0_1_2",
                    RKNNLite.NPU_CORE_0,
                )

                return RKNNRunner(
                    model_path,
                    core_mask,
                )

            except Exception:

                log.exception(
                    "RKNN startup failed"
                )

        onnx_model = model_path.replace(
            ".rknn",
            ".onnx",
        )

        log.warning(
            "AUTO fallback to ONNX"
        )

        return ONNXRunner(
            onnx_model
        )

    raise RuntimeError(
        f"Unknown backend: {backend}"
    )
