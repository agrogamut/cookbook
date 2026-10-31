import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import native
from check import environment


class NativeCommandTest(unittest.TestCase):
    def invoke(self, fixture_exit=0, full_exit=0, complete_events=True):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            reports = root/'reports'
            reports.mkdir()
            lock = root/'runtime.lock'
            lock.write_bytes(b'lock fixture\n')
            calls = []
            env = environment({'PATH': '/usr/bin', 'TEST_DATABASE_URL': 'fixture',
                               'CASE_SOURCE_TEST_DATABASE_URL': 'fixture', 'PORTAL_TEST_DATABASE_URL': 'fixture'})

            def path(value):
                if value == '/reports':
                    return reports
                if value in ('/usr/local/share/pdf-runtime.lock', 'docker/pdf-runtime.lock'):
                    return lock
                self.fail('unexpected path: '+value)

            def output(command, **kwargs):
                if command == ['go', 'version']:
                    return 'go version go1.26.10 linux/amd64'
                if command == ['python3', '-B', 'docker/pdf_runtime_lock_test.py', '--get', 'GO_VERSION']:
                    return '1.26.10'
                if command == ['go', 'list', './...']:
                    return 'example/package\n'
                return 'version fixture'

            def run(command, destination, name, timeout, values):
                calls.append((command, name, timeout, dict(values)))
                self.assertEqual(destination, reports)
                if name == 'fixtures':
                    return fixture_exit
                self.assertEqual(name, 'native-full')
                (reports/'native-full.log').write_text(json.dumps({'Action': 'pass', 'Package': 'example/package'})+'\n')
                return full_exit

            validator = native.validate
            if complete_events:
                validator = lambda events, code, packages: {'errors': []}
            with patch('native.Path', side_effect=path), patch('native.environment', return_value=env), \
                    patch('native.shutil.which', return_value='/tool'), patch('native.shutil.copyfile'), \
                    patch('native.subprocess.check_output', side_effect=output), patch('native.run', side_effect=run), \
                    patch('native.validate', wraps=validator) as validate:
                result = native.main()
            receipt = reports/'native-result.json'
            return result, calls, validate.call_args, json.loads(receipt.read_text()) if receipt.exists() else None

    def test_full_command_serializes_packages_with_inherited_parallel_flags(self):
        result, calls, validation, receipt = self.invoke()
        self.assertEqual(result, 0)
        self.assertEqual(len(calls), 2)
        command, name, timeout, env = calls[1]
        self.assertEqual(command, ['go', 'test', '-p=1', '-json', '-count=1', '-timeout=45m', './...'])
        self.assertEqual((name, timeout), ('native-full', 3300))
        self.assertEqual((env['GOFLAGS'], env['GOMAXPROCS']), ('-p=2', '2'))
        self.assertEqual((env['BOOKDOCX_INTEGRATION'], env['BOOK_READER_NATIVE'], env['CGO_ENABLED']), ('1', '1', '0'))
        self.assertEqual(validation.args[1:], (0, ['example/package']))
        self.assertEqual(receipt['errors'], [])

    def test_fixture_failure_prevents_full_command(self):
        result, calls, validation, receipt = self.invoke(fixture_exit=7)
        self.assertEqual(result, 1)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0][:3], (['go', 'run', './scripts/ci/fixtures'], 'fixtures', 600))
        self.assertIsNone(validation)
        self.assertIsNone(receipt)

    def test_full_failure_reaches_event_validation(self):
        result, calls, validation, receipt = self.invoke(full_exit=7, complete_events=False)
        self.assertEqual(result, 1)
        self.assertEqual(len(calls), 2)
        self.assertEqual(validation.args[1], 7)
        self.assertIn('whole command exit: 7', receipt['errors'])

    def test_zero_exit_without_required_native_events_is_rejected(self):
        result, calls, validation, receipt = self.invoke(complete_events=False)
        self.assertEqual(result, 1)
        self.assertEqual(len(calls), 2)
        self.assertEqual(validation.args[1], 0)
        self.assertTrue(any(error.startswith('required native/source test did not pass:') for error in receipt['errors']))


if __name__ == '__main__':
    unittest.main()
