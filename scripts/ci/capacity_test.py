import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from capacity import (BOUND_CI_DOCKERFILE, BOUND_LOCK, CapacityError, GIB, RESERVE,
                      SpaceGuard, admission, filesystem, phases, verify_contract)


class CapacityTest(unittest.TestCase):
    def test_changed_runtime_cannot_reuse_old_capacity_contract(self):
        verify_contract(BOUND_LOCK, BOUND_CI_DOCKERFILE)
        for values in [('changed', BOUND_CI_DOCKERFILE), (BOUND_LOCK, 'changed')]:
            with self.assertRaisesRegex(CapacityError, 'capacity contract requires review'):
                verify_contract(*values)

    def test_cold_phase_overlap_fits_documented_capacity_without_promising_free_space(self):
        paths = {'docker': '/docker', 'pg': '/work/pg', 'work': '/work'}
        sample = {'device': '1', 'path': '/', 'free': 14_000_000_000, 'used': 0}
        with patch('capacity.filesystem', return_value=sample):
            result = admission(paths, phases(False))
        self.assertEqual(result['required_free_by_device'], {'1': 11490336768})
        self.assertFalse(result['hosted_execution_observed'])
        with patch('capacity.filesystem', return_value=dict(sample, free=11490336767)):
            with self.assertRaisesRegex(CapacityError, 'admission'):
                admission(paths, phases(False))

    def test_prepared_mode_accounts_for_pg_and_work_on_same_device(self):
        paths = {'docker': '/docker', 'pg': '/tmp/pg', 'work': '/tmp/run'}
        def sample(path):
            return {'device': '1' if path == '/docker' else '2', 'path': path, 'free': 10*GIB, 'used': 0}
        with patch('capacity.filesystem', side_effect=sample):
            result = admission(paths, phases(True))
        self.assertEqual(result['required_free_by_device'], {'1': 9*GIB//4, '2': 13*GIB//2})
        self.assertEqual(set(result['phases']), {'native'})

    def test_independent_filesystems_do_not_share_reserve(self):
        paths = {'docker': '/docker', 'pg': '/pg', 'work': '/work'}
        def sample(path):
            return {'device': path, 'path': path, 'free': 5*GIB if path != '/pg' else 3*GIB, 'used': 0}
        with patch('capacity.filesystem', side_effect=sample):
            with self.assertRaisesRegex(CapacityError, 'device /pg'):
                admission(paths, phases(True))

    def test_unknown_measurement_is_not_a_resource_breach(self):
        with patch('pathlib.Path.stat', side_effect=PermissionError('private')):
            with self.assertRaisesRegex(CapacityError, 'measurement failed: PermissionError'):
                filesystem('/private')
        with tempfile.TemporaryDirectory() as directory:
            self.assertEqual(filesystem(Path(directory)/'not-created')['device'], filesystem(directory)['device'])

    def test_growth_failure_and_summary_survive(self):
        with tempfile.TemporaryDirectory() as directory:
            guard = SpaceGuard({'work': directory}, directory, 'growth', {'work': 100})
            sample = {'device': '1', 'path': directory, 'free': RESERVE+1000, 'used': 1000}
            with patch('capacity.filesystem', return_value=sample):
                guard.check(force=True)
            with patch('capacity.filesystem', return_value=dict(sample, used=1101)):
                with self.assertRaisesRegex(CapacityError, 'growth allowance'):
                    guard.check(force=True)
            result = json.loads((Path(directory)/'growth.capacity.json').read_text())
            self.assertEqual(result['maximum_sampled_net_growth'], {'1': 101})
            self.assertEqual(result['samples'], 2)
            with self.assertRaises(CapacityError):
                guard.check()

    def test_reserve_and_device_change_are_fatal(self):
        for failure in ('reserve', 'identity'):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                guard = SpaceGuard({'work': directory}, directory, failure)
                sample = {'device': '1', 'path': directory, 'free': RESERVE+100, 'used': 0}
                with patch('capacity.filesystem', return_value=sample):
                    guard.check(force=True)
                sample = dict(sample, **({'free': RESERVE-1} if failure == 'reserve' else {'device': '2'}))
                with patch('capacity.filesystem', return_value=sample):
                    with self.assertRaisesRegex(CapacityError, failure if failure == 'reserve' else 'identity'):
                        guard.check(force=True)


if __name__ == '__main__':
    unittest.main()
