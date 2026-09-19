import importlib.util
import json
import unittest
from collections import Counter
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


def load():
    path = ROOT / "tools/incident_v3_pipeline.py"
    spec = importlib.util.spec_from_file_location("incident_v3_pipeline", path)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


class IncidentV3CorpusTests(unittest.TestCase):
    def test_corpus_is_balanced_and_redteam_free(self):
        pipeline = load()
        spec = json.loads((ROOT / "testdata/cognitive-v1/incident-v3-corpus-spec.json").read_text())
        self.assertEqual(spec["counts"]["train_per_class"], pipeline.COUNTS["train"])
        self.assertEqual(spec["counts"]["validation_per_class"], pipeline.COUNTS["validation"])
        self.assertEqual(spec["counts"]["independent_test_per_class"], pipeline.COUNTS["independent-test"])
        for split in pipeline.COUNTS:
            records = [pipeline.make_record(split, label, index) for label in pipeline.LABELS for index in range(4)]
            counts = Counter(record["labels"]["incident"] for record in records)
            self.assertEqual(set(counts.values()), {4})
            self.assertTrue(all(record["id"].startswith("incident-v3/") for record in records))
            self.assertTrue(all("incident-redteam-v1" not in json.dumps(record) for record in records))
            self.assertTrue(all(len(record["vector"]) == 86 for record in records))

    def test_training_function_has_incident_only_input(self):
        pipeline = load()
        records = [pipeline.make_record("train", label, 0) for label in pipeline.LABELS]
        model = pipeline.train(records * 2)
        self.assertEqual(model["head"], "incident")
        self.assertEqual(model["input_size"], 86)
        self.assertEqual(model["labels"], pipeline.LABELS)


if __name__ == "__main__":
    unittest.main()
