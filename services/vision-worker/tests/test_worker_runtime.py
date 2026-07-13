import os
import sys
import unittest
from unittest import mock

ROOT = os.path.dirname(os.path.dirname(__file__))
if ROOT not in sys.path:
    sys.path.insert(0, ROOT)

from worker import VisionWorker


class WorkerRuntimeTests(unittest.TestCase):

    def test_init_continues_when_flask_is_missing(self):
        real_import = __import__

        def import_without_flask(name, *args, **kwargs):
            if name == "flask":
                raise ImportError("flask unavailable")
            return real_import(name, *args, **kwargs)

        with mock.patch("builtins.__import__", side_effect=import_without_flask):
            worker = VisionWorker(dry_run=True)
            self.assertIsNone(worker.debug_app)
            self.assertEqual(worker.debug_http_error, "flask_not_installed")


if __name__ == "__main__":
    unittest.main()
