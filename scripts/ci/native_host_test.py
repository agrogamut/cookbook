import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from native_host import DATASETS, FIXTURES, archive_source, fixture_environment, native_command, prepare_external, remove_owned, require_unused_names, select_images, verify_images


class ExistingImageTest(unittest.TestCase):
    def setUp(self):
        self.expected = {'source_sha': 'a'*40, 'lock_sha256': 'b'*64, 'ci_dockerfile_sha256': 'c'*64, 'go_version': '1.26.10'}
        self.manifest = dict(self.expected, version=1, runtime_image='sha256:'+'1'*64, ci_image='sha256:'+'2'*64)
        labels = {'org.opencontainers.image.revision': self.expected['source_sha'], 'recipe.runtime.lock': self.expected['lock_sha256']}
        runtime = {'Id': self.manifest['runtime_image'], 'Os': 'linux', 'Architecture': 'amd64', 'Config': {'Labels': labels}, 'RootFS': {'Layers': ['sha256:'+'3'*64]}}
        ci = copy.deepcopy(runtime)
        ci['Id'] = self.manifest['ci_image']
        ci['Config']['Labels'].update({'recipe.ci.dockerfile': self.expected['ci_dockerfile_sha256'], 'recipe.go.version': self.expected['go_version']})
        ci['RootFS']['Layers'].append('sha256:'+'4'*64)
        self.images = [runtime, ci]

    @patch('native_host.checked')
    def test_existing_container_refused_before_launch_or_cleanup(self, checked):
        checked.return_value = 'existing-container'
        with self.assertRaisesRegex(RuntimeError, 'refusing reuse'):
            require_unused_names(['recipe-native-run-fixture'])
        self.assertEqual(len(checked.call_args_list), 1)
        self.assertEqual(checked.call_args.args[0][:3], ['docker', 'container', 'ls'])

    @patch('native_host.run')
    @patch('native_host.checked')
    def test_existing_mode_never_builds(self, checked, run):
        checked.side_effect = [json.dumps(x) for x in self.images]
        self.assertEqual(select_images(self.manifest, self.expected, Path('/unused'), 'fixture', {}), self.manifest['ci_image'])
        run.assert_not_called()
        self.assertTrue(all(call.args[0][:3] == ['docker', 'image', 'inspect'] for call in checked.call_args_list))
        command = native_command(self.manifest['ci_image'], Path('/owned'), Path('/private/fixture.env'), 'fixture')
        self.assertEqual(command[-3:], [self.manifest['ci_image'], '-B', 'scripts/ci/native.py'])
        self.assertIn('--memory=3g', command)
        self.assertIn('--memory-swap=3g', command)
        self.assertIn('--pull=never', command)
        self.assertIn('/owned/source:/src:ro', command)
        self.assertIn('/owned/external:/src/data/external:ro', command)

    @patch('native_host.checked')
    def test_wrong_metadata_rejected_before_docker(self, checked):
        for key, bad in [('source_sha', 'd'*40), ('lock_sha256', 'd'*64), ('ci_dockerfile_sha256', 'd'*64), ('go_version', '1.0'), ('runtime_image', 'mutable:tag')]:
            with self.subTest(key=key):
                manifest = dict(self.manifest, **{key: bad})
                with self.assertRaises(ValueError):
                    verify_images(manifest, self.expected)
        checked.assert_not_called()

    def test_wrong_image_labels_layers_or_platform_rejected(self):
        for field in ('id', 'label', 'layer', 'platform'):
            images = copy.deepcopy(self.images)
            if field == 'id':
                images[1]['Id'] = 'sha256:'+'5'*64
            elif field == 'label':
                images[1]['Config']['Labels']['recipe.ci.dockerfile'] = 'wrong'
            elif field == 'layer':
                images[1]['RootFS']['Layers'][0] = 'sha256:'+'5'*64
            else:
                images[1]['Architecture'] = 'arm64'
            with self.subTest(field=field), patch('native_host.checked', side_effect=[json.dumps(x) for x in images]):
                with self.assertRaises(ValueError):
                    verify_images(self.manifest, self.expected)

    def test_private_fixture_contract(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)/'fixture.env'
            lines = [key+'=postgres://ci_fixture:local@127.0.0.1:5432/'+db+'?sslmode=disable' for key, db in FIXTURES.items()]
            path.write_text('\n'.join(lines)+'\n')
            path.chmod(0o600)
            self.assertEqual(set(fixture_environment(path)), set(FIXTURES))
            for bad in ('\n'.join(lines[:2]), '\n'.join(lines+lines[:1]), '\n'.join(lines).replace('127.0.0.1', 'remote.example'), '\n'.join(lines).replace('sslmode=disable', 'sslmode=disable&host=remote.example')):
                path.write_text(bad)
                with self.assertRaises(ValueError):
                    fixture_environment(path)
            path.write_text('\n'.join(lines))
            path.chmod(0o644)
            with self.assertRaises(ValueError):
                fixture_environment(path)

    def test_archive_excludes_ignored_environment(self):
        previous = Path.cwd()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            checkout, output = root/'checkout', root/'output'
            checkout.mkdir()
            (output/'source').mkdir(parents=True)
            (output/'reports').mkdir()
            (checkout/'.gitignore').write_text('.env\n')
            (checkout/'.env').write_text('UNRELATED_PRIVATE_VALUE=not-copied\n')
            (checkout/'source.txt').write_text('exact source\n')
            try:
                os.chdir(checkout)
                env = dict(os.environ, GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL='/dev/null')
                for args in (['init', '-q'], ['add', '.gitignore', 'source.txt'], ['-c', 'user.name=Fixture', '-c', 'user.email=fixture@localhost', '-c', 'commit.gpgsign=false', 'commit', '-qm', 'test: archive fixture']):
                    subprocess.run(['git', *args], env=env, check=True, capture_output=True, timeout=10)
                sha = subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True, timeout=10).strip()
                archive_source(output, sha)
            finally:
                os.chdir(previous)
            receipt = json.loads((output/'reports/source-archive.json').read_text())
            self.assertEqual(set(receipt['entries']), {'.gitignore', 'source.txt'})
            self.assertFalse((output/'source/.env').exists())
            self.assertEqual((output/'source/source.txt').read_text(), 'exact source\n')

    @patch('native_host.urlopen')
    def test_retained_external_inputs_require_exact_checksums_without_download(self, download):
        for changed in (False, True):
            with self.subTest(changed=changed), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root/'source/data/external').mkdir(parents=True)
                (root/'reports').mkdir()
                (root/'retained').mkdir()
                lines = []
                for name in DATASETS:
                    data = name.encode()
                    (root/'retained'/name).write_bytes(data + (b'changed' if changed else b''))
                    lines.append(hashlib.sha256(data).hexdigest()+'  '+name)
                (root/'source/data/external/SHA256SUMS').write_text('\n'.join(lines)+'\n')
                if changed:
                    with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
                        prepare_external(root, root/'retained')
                else:
                    prepare_external(root, root/'retained')
                    self.assertEqual(set(json.loads((root/'reports/external-fixtures.json').read_text())), set(DATASETS))
                download.assert_not_called()


class CleanupTest(unittest.TestCase):
    @patch('native_host.checked')
    def test_absent_or_matching_container_only(self, checked):
        checked.return_value = ''
        remove_owned('recipe-pg-12-1', '12-1')
        self.assertEqual(checked.call_count, 1)
        checked.reset_mock()
        checked.side_effect = ['container-id', '12-1', 'container-id']
        remove_owned('recipe-pg-12-1', '12-1')
        self.assertEqual(checked.call_args.args[0], ['docker', 'rm', '-f', '-v', 'container-id'])

    @patch('native_host.checked', side_effect=['container-id', 'another-run'])
    def test_wrong_owner_is_retained(self, checked):
        with self.assertRaisesRegex(RuntimeError, 'ownership changed'):
            remove_owned('recipe-pg-12-1', '12-1')
        self.assertEqual(checked.call_count, 2)


if __name__ == '__main__':
    unittest.main()
