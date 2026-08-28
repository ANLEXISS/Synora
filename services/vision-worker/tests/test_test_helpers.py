import unittest

import numpy as np

from test_helpers import FakeModelRunner


class TestHermeticHelpers(unittest.TestCase):

    def test_fake_model_runner_is_deterministic_and_bounded(self):
        runner = FakeModelRunner([np.array([[1.0, 2.0]], dtype=np.float32)])
        first = runner.infer(np.zeros((1, 2), dtype=np.float32))
        second = runner.infer(np.ones((1, 2), dtype=np.float32))
        self.assertEqual(runner.backend, "fake")
        self.assertEqual(runner.calls, 2)
        np.testing.assert_array_equal(first[0], second[0])
        runner.close()
        self.assertTrue(runner.closed)
        with self.assertRaises(RuntimeError):
            runner.infer(np.zeros((1, 2), dtype=np.float32))


if __name__ == "__main__":
    unittest.main()
