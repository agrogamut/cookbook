import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from capacity import BOUND_CI_DOCKERFILE, BOUND_LOCK
from native_host import DATASETS, FIXTURES, archive_source, fixture_environment, main, native_command, prepare_external, provision, remove_owned, require_unused_names, select_images, verify_images


class LegacyBuilderTest(unittest.TestCase):
    def setUp(self):
        self.version = {'Client': {'Version': '29.2.1', 'ApiVersion': '1.52', 'Os': 'linux'},
                        'Server': {'Version': '29.2.1', 'ApiVersion': '1.52', 'Os': 'linux', 'Arch': 'amd64'}}
        self.help = 'Usage: docker build\n --cpu-period int\n --memory-swap bytes\n --force-rm\n'
        self.calls = []

    def runner(self, command, reports, name, timeout, env, **kwargs):
        self.calls.append((command, env))
        text = json.dumps(self.version) if command[1] == 'version' else self.help
        (reports/(name+'.log')).write_text(text)
        return 0

    def select(self, reports):
        expected = {'source_sha': 'a'*40, 'lock_sha256': 'b'*64, 'ci_dockerfile_sha256': 'c'*64, 'go_version': '1.26.10'}
        with patch('native_host.checked', side_effect=['pinned-builder', 'sha256:'+'1'*64, 'sha256:'+'2'*64]), \
                patch('native_host.verify_images', return_value='verified-image'):
            return select_images(None, expected, reports, 'fixture', {'DOCKER_BUILDKIT': '1'})

    def test_default_uses_single_measured_builder(self):
        with tempfile.TemporaryDirectory() as directory, patch('native_host.run', side_effect=self.runner):
            reports = Path(directory)
            self.assertEqual(self.select(reports), 'verified-image')
            builds = [call for call in self.calls if call[0][:2] == ['docker', 'build'] and '--help' not in call[0]]
            self.assertEqual(len(builds), 2)
            for command, _ in builds:
                self.assertIn('--memory=3g', command)
                self.assertIn('--memory-swap=3g', command)
                self.assertIn('--cpu-quota=200000', command)
            self.assertTrue(all(env['DOCKER_BUILDKIT'] == '0' for _, env in self.calls))
            receipt = json.loads((reports/'builder-contract.json').read_text())
            self.assertEqual(receipt['mode'], 'legacy')
            self.assertEqual(receipt['client'], self.version['Client'])
            self.assertEqual(receipt['server'], self.version['Server'])
            self.assertEqual(receipt['build_execution_verified'], False)

    def test_missing_capabilities_refuse_before_build(self):
        for failure in ('missing-server', 'wrong-platform', 'missing-flag', 'malformed-json'):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                self.setUp()
                if failure == 'missing-server':
                    del self.version['Server']
                elif failure == 'wrong-platform':
                    self.version['Server']['Os'] = 'windows'
                elif failure == 'missing-flag':
                    self.help = self.help.replace('--memory-swap', '--different')
                elif failure == 'malformed-json':
                    self.version = []
                with patch('native_host.run', side_effect=self.runner):
                    with self.assertRaisesRegex(RuntimeError, 'legacy builder'):
                        self.select(Path(directory))
                self.assertFalse(any(command[:2] == ['docker', 'build'] and '--help' not in command for command, _ in self.calls))

    def test_unreachable_daemon_or_missing_cli_does_not_fall_back(self):
        for failed_step in ('docker-version', 'legacy-build-help'):
            with self.subTest(failed_step=failed_step), tempfile.TemporaryDirectory() as directory:
                self.calls = []
                def failed(command, reports, name, timeout, env, **kwargs):
                    self.runner(command, reports, name, timeout, env)
                    return 127 if name == failed_step else 0
                with patch('native_host.run', side_effect=failed):
                    with self.assertRaisesRegex(RuntimeError, 'legacy builder'):
                        self.select(Path(directory))
                self.assertEqual(len(self.calls), 1 if failed_step == 'docker-version' else 2)

    def test_failed_build_stops_without_alternate_builder(self):
        with tempfile.TemporaryDirectory() as directory:
            def failed(command, reports, name, timeout, env, **kwargs):
                self.runner(command, reports, name, timeout, env)
                return 1 if name == 'runtime-build' else 0
            with patch('native_host.run', side_effect=failed):
                with self.assertRaisesRegex(RuntimeError, 'runtime-build failed'):
                    self.select(Path(directory))
            self.assertEqual(self.calls[-1][0][:2], ['docker', 'build'])
            self.assertEqual(len(self.calls), 3)
            self.assertTrue(all(env['DOCKER_BUILDKIT'] == '0' for _, env in self.calls))


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

    def test_prepared_main_guards_real_pg_filesystem_without_provision_or_build(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ('pg', 'external', 'docker'):
                (root/name).mkdir()
            fixture = root/'fixture.env'
            fixture.write_text('\n'.join(key+'=postgres://ci_fixture:local@127.0.0.1:5432/'+db+'?sslmode=disable' for key, db in FIXTURES.items()))
            fixture.chmod(0o600)
            manifest = root/'images.json'
            manifest.write_text(json.dumps(self.manifest))
            expected = dict(self.expected, lock_sha256=BOUND_LOCK, ci_dockerfile_sha256=BOUND_CI_DOCKERFILE)
            arguments = ['native_host.py', '--existing-images', str(manifest), '--work-dir', str(root/'run'),
                         '--fixture-env', str(fixture), '--pg-data-dir', str(root/'pg'),
                         '--external-data', str(root/'external'), '--run-id', 'fixture']
            original_read = Path.read_text
            def read(path, *args, **kwargs):
                return 'MemAvailable: 16777216 kB\n' if path == Path('/proc/meminfo') else original_read(path, *args, **kwargs)
            def execute(command, reports, name, timeout, env, **kwargs):
                self.assertEqual(command[:3], ['docker', 'run', '--rm'])
                self.assertIn('--pull=never', command)
                self.assertIsNotNone(kwargs['guard'])
                (reports/'native-result.json').write_text(json.dumps({'command_exit': 0, 'errors': []}))
                return 0
            with patch.object(sys, 'argv', arguments), patch('pathlib.Path.read_text', read), \
                 patch('native_host.source_contract', return_value=expected), \
                 patch('native_host.checked', return_value=str(root/'docker')) as checked, \
                 patch('native_host.admission', return_value={}) as admit, \
                 patch('native_host.SpaceGuard'), patch('native_host.require_unused_names'), \
                 patch('native_host.select_images', return_value=self.manifest['ci_image']) as images, \
                 patch('native_host.archive_source'), patch('native_host.prepare_external'), \
                 patch('native_host.provision') as provision, patch('native_host.remove_owned') as remove, \
                 patch('native_host.run', side_effect=execute):
                self.assertEqual(main(), 0)
            provision.assert_not_called()
            self.assertEqual(images.call_args.args[0], self.manifest)
            self.assertEqual(admit.call_args.args[0]['pg'], root/'pg')
            self.assertEqual(checked.call_args_list[0].args[0][:2], ['docker', 'info'])
            remove.assert_called_once_with('recipe-native-run-fixture', 'fixture')

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
    def test_new_fixture_data_uses_accounted_run_bind(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root/'reports').mkdir()
            responses = ['owned-container', 'ready', json.dumps({'5432/tcp': [{'HostIp': '127.0.0.1', 'HostPort': '34567'}]}), '123456', '', '', '']
            with patch('native_host.checked', side_effect=responses) as checked:
                result = provision(root, root/'reports', 'owned-name', 'owned-token')
            command = checked.call_args_list[0].args[0]
            self.assertEqual(command[command.index('-v')+1], str(root/'pgdata')+':/var/lib/postgresql/data')
            self.assertTrue((root/'pgdata').is_dir())
            self.assertIn('--memory-swap=3g', command)
            self.assertEqual(result.stat().st_mode & 0o777, 0o600)
            self.assertEqual(set(fixture_environment(result)), set(FIXTURES))

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
