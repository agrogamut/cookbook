import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
from capacity import CapacityError, RESERVE
from check import environment, run


class CheckTest(unittest.TestCase):
    def test_separate_stderr_preserves_exact_streams(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            stderr = root/'json.stderr.log'
            command = [sys.executable, '-c', 'import os; os.write(1, b\'{"Action":"pass"}\\n\'); os.write(2, b"go: downloading example/module v1.0.0\\n")']
            self.assertEqual(run(command, root, 'json', 5, stderr_path=stderr), 0)
            self.assertEqual((root/'json.log').read_bytes(), b'{"Action":"pass"}\n')
            self.assertEqual(stderr.read_bytes(), b'go: downloading example/module v1.0.0\n')
            receipt = json.loads((root/'json.json').read_text())
            self.assertEqual(receipt['stderr_log'], stderr.name)
            self.assertEqual(receipt['stderr_bytes'], stderr.stat().st_size)

    def test_default_caller_keeps_combined_output(self):
        with tempfile.TemporaryDirectory() as root:
            command = [sys.executable, '-c', 'import os; os.write(1, b"stdout\\n"); os.write(2, b"stderr\\n")']
            self.assertEqual(run(command, root, 'combined', 5), 0)
            self.assertEqual((Path(root)/'combined.log').read_bytes(), b'stdout\nstderr\n')
            self.assertFalse((Path(root)/'combined.stderr.log').exists())

    def test_separate_stderr_retained_on_failure_and_timeout(self):
        for name, suffix, timeout, expected in [('failure', 'raise SystemExit(7)', 5, 7),
                                               ('timeout', 'import time; time.sleep(5)', 0.2, 124)]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as root:
                stderr = Path(root)/(name+'.stderr.log')
                command = [sys.executable, '-c', 'import os; os.write(1, b"{}\\n"); os.write(2, b"diagnostic\\n"); '+suffix]
                self.assertEqual(run(command, root, name, timeout, stderr_path=stderr), expected)
                self.assertEqual((Path(root)/(name+'.log')).read_bytes(), b'{}\n')
                self.assertEqual(stderr.read_bytes(), b'diagnostic\n')
                receipt = json.loads((Path(root)/(name+'.json')).read_text())
                self.assertEqual(receipt['exit'], expected)
                self.assertNotEqual(receipt['observed_process_exit'], 0)

    def test_stderr_limit_returns_guard_failure_without_truncation(self):
        for suffix in ('', '; import time; time.sleep(10)'):
            with self.subTest(suffix=suffix), tempfile.TemporaryDirectory() as root:
                stderr = Path(root)/'limit.stderr.log'
                with patch('check.STDERR_LIMIT', 128):
                    code = run([sys.executable, '-c', 'import os; os.write(2, b"x"*256)'+suffix], root, 'limit', 5, stderr_path=stderr)
                self.assertEqual(code, 125)
                self.assertEqual(stderr.read_bytes(), b'x'*256)
                receipt = json.loads((Path(root)/'limit.json').read_text())
                self.assertIn('stderr report', receipt['capacity_error'])
                self.assertEqual((receipt['stderr_limit_bytes'], receipt['stderr_bytes']), (128, 256))
                if suffix:
                    self.assertNotEqual(receipt['observed_process_exit'], 0)
                    self.assertLess(receipt['elapsed_seconds'], 3)

    def test_separate_stderr_refuses_existing_or_misbound_path(self):
        with tempfile.TemporaryDirectory() as root:
            root = Path(root)
            stderr = root/'safe.stderr.log'
            stderr.write_bytes(b'preserved')
            for path in (stderr, root/'safe.log', root/'unexpected.log'):
                with self.subTest(path=path), self.assertRaises(ValueError):
                    run([sys.executable, '-c', 'pass'], root, 'safe', 5, stderr_path=path)
            self.assertEqual(stderr.read_bytes(), b'preserved')
            self.assertFalse((root/'safe.log').exists())

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
                code = run([sys.executable, '-u', '-c', 'import os,time; print("started"); os.write(2, b"diagnostic\\n"); time.sleep(10)'], root, 'loss', 5, env={'PATH': '/usr/bin'}, stderr_path=Path(root)/'loss.stderr.log')
            self.assertEqual(code, 125)
            result = json.loads((Path(root)/'loss.json').read_text())
            self.assertIn('reserve crossed', result['capacity_error'])
            self.assertNotEqual(result['observed_process_exit'], 0)
            self.assertIn('started', (Path(root)/'loss.log').read_text())
            self.assertEqual((Path(root)/'loss.stderr.log').read_bytes(), b'diagnostic\n')


if __name__ == "__main__":
    unittest.main()
