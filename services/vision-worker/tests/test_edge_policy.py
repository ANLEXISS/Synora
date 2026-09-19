import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parents[1]))

from edge.policy import central_retrack_allowed  # noqa: E402


class EdgePolicyTests(unittest.TestCase):
    def test_fallback_is_disabled_by_default(self):
        for status in ("unavailable", "invalid", "contradictory"):
            self.assertFalse(central_retrack_allowed(status))

    def test_fallback_requires_explicit_opt_in_and_known_failure(self):
        self.assertTrue(central_retrack_allowed("unavailable", enabled=True))
        self.assertTrue(central_retrack_allowed("invalid", enabled=True))
        self.assertTrue(central_retrack_allowed("contradictory", enabled=True))
        self.assertFalse(central_retrack_allowed("ok", enabled=True))


if __name__ == "__main__":
    unittest.main()
