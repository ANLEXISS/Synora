"""Hermetic Vision test doubles shared by software-only tests."""

import numpy as np


class FakeModelRunner:
    """Deterministic model boundary; never loads RKNN or touches a device."""

    backend = "fake"

    def __init__(self, outputs=None):
        self.outputs = outputs if outputs is not None else [np.ones((1, 512), dtype=np.float32)]
        self.calls = 0
        self.closed = False

    def infer(self, input_tensor):
        del input_tensor
        if self.closed:
            raise RuntimeError("fake model runner is closed")
        self.calls += 1
        return [np.array(output, copy=True) for output in self.outputs]

    def close(self):
        self.closed = True

