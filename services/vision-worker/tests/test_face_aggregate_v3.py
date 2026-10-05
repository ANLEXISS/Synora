import sys
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parents[1]))

from core.face_aggregate_v3 import FaceConsensusV3, FaceObservationV3


NOW = datetime(2026, 1, 1, tzinfo=timezone.utc)


class FaceAggregateV3Tests(unittest.TestCase):
  def test_recognized_requires_consensus_and_never_exports_identity(self):
    aggregate = FaceConsensusV3().aggregate([
        FaceObservationV3("recognized", .9, .92),
        FaceObservationV3("recognized", .8, .88),
    ], now=NOW)
    payload = aggregate.as_bus_payload()
    self.assertEqual(payload["status"], "recognized")
    self.assertEqual(payload["qualification_provenance"], "not_qualified")
    self.assertFalse(any(key in payload for key in ("identity", "resident_id", "embedding", "crop", "image")))


  def test_conflicting_frames_are_uncertain(self):
    aggregate = FaceConsensusV3().aggregate([
        FaceObservationV3("recognized", .9, .9),
        FaceObservationV3("unknown", .9, .1),
    ], now=NOW)
    self.assertEqual(aggregate.status, "uncertain")


  def test_low_quality_is_not_promoted(self):
    aggregate = FaceConsensusV3().aggregate([
        FaceObservationV3("low_quality", .2, .2),
        FaceObservationV3("low_quality", .3, .3),
    ], now=NOW)
    self.assertEqual(aggregate.status, "low_quality")


  def test_expired_results_are_unavailable(self):
    aggregate = FaceConsensusV3().aggregate([
        FaceObservationV3("recognized", .9, .9, NOW - timedelta(seconds=1)),
    ], now=NOW)
    self.assertEqual(aggregate.status, "unavailable")


  def test_backend_unavailable_is_explicit(self):
    aggregate = FaceConsensusV3().aggregate([FaceObservationV3("unavailable")], now=NOW)
    self.assertEqual(aggregate.status, "unavailable")


  def test_invalid_status_and_provenance_are_bounded(self):
    aggregate = FaceConsensusV3().aggregate([
        FaceObservationV3("not-a-status", 4, 4, provenance="not-a-manifest"),
        FaceObservationV3("not-a-status", -1, -1, provenance="not-a-manifest"),
    ], now=NOW)
    self.assertEqual(aggregate.status, "uncertain")
    self.assertEqual(aggregate.qualification_provenance, "not_qualified")
    self.assertGreaterEqual(aggregate.quality, 0)
    self.assertLessEqual(aggregate.quality, 1)
    self.assertGreaterEqual(aggregate.confidence, 0)
    self.assertLessEqual(aggregate.confidence, 1)


  def test_label_consent_is_preserved_only_as_qualification_provenance(self):
    aggregate = FaceConsensusV3().aggregate([
        FaceObservationV3("recognized", .9, .9, provenance="labeled_consent_manifest"),
        FaceObservationV3("recognized", .9, .9, provenance="labeled_consent_manifest"),
    ], now=NOW)
    self.assertEqual(aggregate.qualification_provenance, "labeled_consent_manifest")
    self.assertEqual("recognized", aggregate.as_bus_payload()["status"])
