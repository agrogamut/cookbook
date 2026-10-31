import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
from capacity import CapacityError, RESERVE
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

    def test_reserve_failure_prevents_launch(self):
        with tempfile.TemporaryDirectory() as root:
            marker = Path(root)/'launched'
            sample = {'device': '1', 'path': root, 'free': RESERVE-1, 'used': 0}
            with patch('capacity.filesystem', return_value=sample):
                code = run([sys.executable, '-c', 'from pathlib import Path; Path('+repr(str(marker))+').touch()'], root, 'reserve', 5)
            self.assertEqual(code, 125)
            self.assertFalse(marker.exists())
            self.assertIn('reserve', json.loads((Path(root)/'reserve.json').read_text())['capacity_error'])

    def test_measurement_failure_stops_running_child(self):
        with tempfile.TemporaryDirectory() as root:
            calls = 0
            def sample(path):
                nonlocal calls
                calls += 1
                if calls > 4:
                    raise CapacityError('fixture measurement unavailable')
                return {'device': '1', 'path': root, 'free': RESERVE+1000, 'used': 0}
            with patch('capacity.filesystem', side_effect=sample):
                code = run([sys.executable, '-u', '-c', 'import time; print("started"); time.sleep(10)'], root, 'measurement', 5, env={'PATH': '/usr/bin'})
            self.assertEqual(code, 125)
            result = json.loads((Path(root)/'measurement.json').read_text())
            self.assertIn('measurement unavailable', result['capacity_error'])
            self.assertLess(result['elapsed_seconds'], 3)
            self.assertIn('started', (Path(root)/'measurement.log').read_text())

    def test_reserve_loss_stops_running_child(self):
        with tempfile.TemporaryDirectory() as root:
            calls = 0
            def sample(path):
                nonlocal calls
                calls += 1
                return {'device': '1', 'path': root, 'free': RESERVE+(1000 if calls <= 4 else -1), 'used': 0}
            with patch('capacity.filesystem', side_effect=sample):
                code = run([sys.executable, '-u', '-c', 'import time; print("started"); time.sleep(10)'], root, 'loss', 5, env={'PATH': '/usr/bin'})
            self.assertEqual(code, 125)
            result = json.loads((Path(root)/'loss.json').read_text())
            self.assertIn('reserve crossed', result['capacity_error'])
            self.assertNotEqual(result['observed_process_exit'], 0)
            self.assertIn('started', (Path(root)/'loss.log').read_text())


if __name__ == "__main__":
    unittest.main()
