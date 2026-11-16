import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import numpy as np

import gen_report


SCRIPT = Path(__file__).with_name("gen_report.py")


def request(first, tokens):
    return {"t0": 0, "t_first": first, "t_tokens": tokens}


class CleanArrayTests(unittest.TestCase):
    def test_two_samples_survive_trimming(self):
        np.testing.assert_array_equal(
            gen_report.clean_array(np.array([1.0, 2.0]), 5, 3.5), [1.0, 2.0]
        )

    def test_empty_intervals_stay_empty(self):
        self.assertEqual(gen_report.clean_array(np.array([]), 5, 3.5).size, 0)

    def test_normal_sample_still_trims_outliers(self):
        np.testing.assert_array_equal(
            gen_report.clean_array(np.arange(101), 5, 3.5), np.arange(5, 96)
        )


class ReportCLITests(unittest.TestCase):
    def run_report(self, root, datasets):
        command = [sys.executable, str(SCRIPT), "--output-dir", str(root / "report")]
        for name, rows in datasets.items():
            source = root / f"{name}.jsonl"
            source.write_text("".join(json.dumps(row) + "\n" for row in rows))
            command.extend(["--dataset", name, str(source)])
        return subprocess.run(
            command, capture_output=True, text=True, timeout=30,
            env={**os.environ, "MPLBACKEND": "Agg"},
        )

    def test_small_and_normal_reports(self):
        cases = [
            ("one_chunk", [request(1, [1])], "N/A", 3),
            ("one_request", [request(1, [1, 2])], "`1.0000`", 6),
            ("zero_latency", [request(0, [0, 0])], "`0.0000`", 6),
            ("two_requests", [request(1, [1, 2]), request(2, [2, 4])], "`1.5000`", 6),
            ("normal", [request(i, [i, i + 1]) for i in range(1, 21)], "`1.0000`", 6),
        ]
        for name, rows, mean, figures in cases:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                result = self.run_report(root, {name: rows})
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn("RuntimeWarning", result.stderr)
                self.assertNotIn("UserWarning", result.stderr)
                text = (root / "report/report.md").read_text()
                self.assertIn(f"clean mean per-token: {mean}", text)
                if name == "two_requests":
                    self.assertIn("clean mean TTFT: `1.5000`", text)
                self.assertEqual(text.count(".png)"), figures)
                self.assertEqual(len(list((root / "report").glob("*.png"))), figures)
                for path in (root / "report").glob("*.png"):
                    self.assertTrue(path.read_bytes().startswith(b"\x89PNG\r\n\x1a\n"))

    def test_mixed_datasets_keep_available_intervals(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result = self.run_report(root, {
                "single": [request(1, [1])],
                "multiple": [request(2, [2, 3, 4])],
            })
            self.assertEqual(result.returncode, 0, result.stderr)
            text = (root / "report/report.md").read_text()
            self.assertIn("clean mean per-token: N/A", text)
            self.assertIn("clean mean per-token: `1.0000`", text)
            self.assertEqual(text.count(".png)"), 6)

    def test_reused_output_does_not_link_stale_interval_plots(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            output = root / "report"
            output.mkdir()
            (output / "pt_hist.png").write_bytes(b"stale")
            result = self.run_report(root, {"single": [request(1, [1])]})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn("pt_hist.png", (output / "report.md").read_text())

    def test_no_first_token_has_clear_dataset_error(self):
        for name, rows in [("empty", []), ("no_first_token", [request(None, [])])]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                result = self.run_report(root, {name: rows})
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(name, result.stderr)
                self.assertIn("no requests with a first-token timestamp", result.stderr)
                self.assertNotIn("Traceback", result.stderr)
                self.assertFalse((root / "report/report.md").exists())


if __name__ == "__main__":
    unittest.main()
