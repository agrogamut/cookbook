import json
from pathlib import Path
import sys
import tempfile
import unittest
from check import environment, run


class CheckTest(unittest.TestCase):
    def test_environment_has_no_implicit_credentials(self):
        result = environment({"PATH": "/bin", "UNRELATED_SECRET": "discarded", "BOOK_READER_NATIVE": "0"})
        self.assertNotIn("UNRELATED_SECRET", result)
        self.assertNotIn("BOOK_READER_NATIVE", result)
        with self.assertRaises(ValueError):
            environment({"GEMINI_API_KEY": "test-value"})

    def test_failure_and_timeout_preserve_exit(self):
        with tempfile.TemporaryDirectory() as root:
            self.assertEqual(run([sys.executable, "-c", "raise SystemExit(7)"], root, "failure", 5), 7)
            self.assertEqual(json.loads((Path(root)/"failure.json").read_text())["exit"], 7)
            self.assertEqual(run([sys.executable, "-c", "import time; time.sleep(5)"], root, "timeout", 0.05), 124)
            with self.assertRaises(ValueError):
                run([sys.executable, "-c", "pass"], root, "failure", 5)

    def test_missing_program_records_failure(self):
        with tempfile.TemporaryDirectory() as root:
            self.assertEqual(run([str(Path(root)/'missing-program')], root, 'startup', 5), 127)
            self.assertEqual(json.loads((Path(root)/'startup.json').read_text())['startup_error'], 'FileNotFoundError')


if __name__ == "__main__":
    unittest.main()
