"""Provision a disposable native environment, or verify an explicit prepared one."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import time
from urllib.parse import parse_qs, unquote, urlsplit
from urllib.request import urlopen
from check import environment, run, wait
from capacity import PG_IMAGE, SpaceGuard, admission, phases, verify_contract

POSTGRES = 'postgres@sha256:639ab7ceb90e13123085b741fb31ef493fba25463002f6da665352e7b534b652'
IMAGE_ID = re.compile(r'sha256:[0-9a-f]{64}')
FIXTURES = {'TEST_DATABASE_URL': 'ci_legacy', 'CASE_SOURCE_TEST_DATABASE_URL': 'ci_cases', 'PORTAL_TEST_DATABASE_URL': 'ci_portal'}
DATASETS = {
    'ifct2017_index.csv': 'https://huggingface.co/datasets/NUTRIC/IFCT-2017-Data/resolve/main/compositions-master/index.csv',
    'indian_food_dataset.csv': 'https://huggingface.co/datasets/Anupam007/indian-recipe-dataset/resolve/main/Cleaned_Indian_Food_Dataset.csv',
}
ACTIVE_GUARD = None


def checked(args, timeout=120, **kwargs):
    if ACTIVE_GUARD is None:
        return subprocess.check_output(args, text=True, timeout=timeout, **kwargs).strip()
    ACTIVE_GUARD.check()
    with subprocess.Popen(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          start_new_session=True, **kwargs) as process:
        output, error = wait(process, timeout, ACTIVE_GUARD)
        if process.returncode:
            raise subprocess.CalledProcessError(process.returncode, args, output, error)
        return output.strip()


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def source_contract():
    if checked(['git', 'status', '--porcelain', '--untracked-files=all']):
        raise RuntimeError('native verification requires a clean committed source')
    return {'source_sha': checked(['git', 'rev-parse', 'HEAD']),
            'lock_sha256': digest('docker/pdf-runtime.lock'),
            'ci_dockerfile_sha256': digest('scripts/ci/Dockerfile'),
            'go_version': checked(['python3', '-B', 'docker/pdf_runtime_lock_test.py', '--get', 'GO_VERSION'])}


def verify_images(manifest, expected):
    if set(manifest) != set(expected) | {'version', 'runtime_image', 'ci_image'} or manifest.get('version') != 1:
        raise ValueError('invalid prepared-image manifest schema')
    if any(manifest.get(key) != value for key, value in expected.items()):
        raise ValueError('prepared images do not match current source, lock, test Dockerfile or toolchain')
    images = []
    for key in ('runtime_image', 'ci_image'):
        image = manifest[key]
        if not isinstance(image, str) or not IMAGE_ID.fullmatch(image):
            raise ValueError('prepared image must be an exact SHA256 ID')
        actual = json.loads(checked(['docker', 'image', 'inspect', '--format', '{{json .}}', image]))
        labels = actual.get('Config', {}).get('Labels') or {}
        required = {'org.opencontainers.image.revision': expected['source_sha'], 'recipe.runtime.lock': expected['lock_sha256']}
        if key == 'ci_image':
            required.update({'recipe.ci.dockerfile': expected['ci_dockerfile_sha256'], 'recipe.go.version': expected['go_version']})
        if actual.get('Id') != image or actual.get('Os') != 'linux' or actual.get('Architecture') != 'amd64' or any(labels.get(k) != v for k, v in required.items()):
            raise ValueError('prepared image identity or labels differ')
        layers = actual.get('RootFS', {}).get('Layers')
        if not isinstance(layers, list) or not layers or not all(IMAGE_ID.fullmatch(x) for x in layers):
            raise ValueError('prepared image has no verified layer identity')
        images.append(layers)
    if images[1][:len(images[0])] != images[0] or len(images[1]) <= len(images[0]):
        raise ValueError('test image does not inherit the prepared runtime layers')
    return manifest['ci_image']


def legacy_builder_preflight(reports, env):
    build_env = dict(env, DOCKER_BUILDKIT='0')
    for name, command in (
        ('docker-version', ['docker', 'version', '--format', '{{json .}}']),
        ('legacy-build-help', ['docker', 'build', '--help']),
    ):
        if run(command, reports, name, 30, build_env, guard=ACTIVE_GUARD):
            raise RuntimeError('legacy builder prerequisite failed: '+name+'; no alternate builder attempted')
    required = ('--cpu-period', '--memory-swap', '--force-rm')
    try:
        version = json.loads((reports/'docker-version.log').read_text())
        if not isinstance(version, dict):
            raise ValueError('invalid Docker version response')
        client, server = version.get('Client'), version.get('Server')
        if not all(isinstance(part, dict) and part.get('Os') == 'linux'
                   and all(isinstance(part.get(key), str) and part[key] for key in ('Version', 'ApiVersion'))
                   for part in (client, server)) or server.get('Arch') != 'amd64':
            raise ValueError('Linux client and reachable amd64 daemon required')
        help_text = (reports/'legacy-build-help.log').read_text()
        if not all(re.search(r'(?m)^\s+'+re.escape(flag)+r'(?:\s|$)', help_text) for flag in required):
            raise ValueError('classic build CLI flags unavailable')
    except (OSError, ValueError) as error:
        raise RuntimeError('legacy builder capability check failed; no alternate builder attempted') from error
    receipt = {'mode': 'legacy', 'DOCKER_BUILDKIT': '0', 'client': client, 'server': server,
               'required_cli_flags': required, 'build_execution_verified': False}
    (reports/'builder-contract.json').write_text(json.dumps(receipt, indent=2)+'\n')
    return build_env


def select_images(prepared, expected, reports, token, env):
    if prepared is not None:
        return verify_images(prepared, expected)
    build_env = legacy_builder_preflight(reports, env)
    runtime, tests = 'recipe-native:'+token, 'recipe-native-tests:'+token
    toolchain = checked(['python3', '-B', 'docker/pdf_runtime_lock_test.py', '--get', 'BUILD_IMAGE'])
    labels = ['--label', 'org.opencontainers.image.revision='+expected['source_sha'],
              '--label', 'recipe.runtime.lock='+expected['lock_sha256']]
    limits = ['--cpuset-cpus=0,1', '--cpu-period=100000', '--cpu-quota=200000',
              '--memory=3g', '--memory-swap=3g', '--force-rm']
    commands = [
        ('runtime-build', ['docker', 'build', '--platform=linux/amd64', *limits, *labels, '-t', runtime, '.']),
        ('test-runtime-build', ['docker', 'build', '--platform=linux/amd64', *limits, *labels,
                               '--label', 'recipe.ci.dockerfile='+expected['ci_dockerfile_sha256'],
                               '--label', 'recipe.go.version='+expected['go_version'],
                               '-f', 'scripts/ci/Dockerfile', '--build-arg', 'TEST_RUNTIME='+runtime,
                               '--build-arg', 'BUILD_IMAGE='+toolchain, '-t', tests, '.']),
    ]
    for name, command in commands:
        if run(command, reports, name, 1800, build_env, guard=ACTIVE_GUARD):
            raise RuntimeError(name+' failed; actual result retained')
    manifest = dict(expected, version=1,
                    runtime_image=checked(['docker', 'image', 'inspect', '--format', '{{.Id}}', runtime]),
                    ci_image=checked(['docker', 'image', 'inspect', '--format', '{{.Id}}', tests]))
    (reports/'prepared-images.json').write_text(json.dumps(manifest, indent=2)+'\n')
    return verify_images(manifest, expected)


def fixture_environment(path):
    path = Path(path)
    if not path.is_file() or path.stat().st_size > 16384 or path.stat().st_mode & 0o077 or path.stat().st_uid != os.getuid():
        raise ValueError('fixture environment must be a private owned regular file')
    values = {}
    for line in path.read_text().splitlines():
        key, sep, value = line.partition('=')
        if not sep or key in values or key not in FIXTURES:
            raise ValueError('fixture environment has an unknown or repeated key')
        values[key] = value
    if set(values) != set(FIXTURES):
        raise ValueError('all three isolated fixtures are required')
    ports = set()
    for key, value in values.items():
        try:
            parsed = urlsplit(value)
            valid = (parsed.scheme in ('postgres', 'postgresql') and parsed.hostname == '127.0.0.1'
                     and unquote(parsed.username or '') == 'ci_fixture' and bool(parsed.password)
                     and parsed.path == '/'+FIXTURES[key] and not parsed.fragment
                     and parse_qs(parsed.query, strict_parsing=True) == {'sslmode': ['disable']}
                     and parsed.port is not None)
            ports.add(parsed.port)
        except ValueError:
            valid = False
        if not valid:
            raise ValueError('fixture target is not the declared isolated loopback database')
    if len(ports) != 1:
        raise ValueError('fixture destinations must share one guarded cluster port')
    return values


def remove_owned(name, token):
    found = checked(['docker', 'container', 'ls', '-aq', '--filter', 'name=^'+name+'$'])
    if not found:
        return
    observed = checked(['docker', 'inspect', '--format', '{{index .Config.Labels "recipe.ci.run"}}', found])
    if observed != token:
        raise RuntimeError('fixture ownership changed; refusing cleanup')
    checked(['docker', 'rm', '-f', '-v', found])


def require_unused_names(names):
    for name in names:
        if checked(['docker', 'container', 'ls', '-aq', '--filter', 'name=^'+name+'$']):
            raise RuntimeError('run container name already exists; refusing reuse')


def session_labels():
    session = os.environ.get('AO_SESSION_ID')
    return ['--label', 'ao.session='+session] if session else []


def provision(root, reports, pg, token):
    (root/'pgdata').mkdir()
    container = checked(['docker', 'run', '-d', '--name', pg, '--label', 'recipe.ci.run='+token,
                         *session_labels(),
                         '--cpuset-cpus', '0,1', '--cpus=1', '--memory=3g', '--memory-swap=3g',
                         '-v', str(root/'pgdata')+':/var/lib/postgresql/data',
                         '-p', '127.0.0.1::5432', '-e', 'POSTGRES_USER=ci_fixture',
                         '-e', 'POSTGRES_PASSWORD=ci-disposable-only', POSTGRES])
    deadline = time.monotonic()+120
    while time.monotonic() < deadline:
        if ACTIVE_GUARD is not None:
            ACTIVE_GUARD.check()
        try:
            checked(['docker', 'exec', container, 'pg_isready', '-U', 'ci_fixture'], timeout=5)
            break
        except (subprocess.CalledProcessError, subprocess.TimeoutExpired):
            for _ in range(4):
                time.sleep(0.5)
                if ACTIVE_GUARD is not None:
                    ACTIVE_GUARD.check()
    else:
        raise RuntimeError('disposable PostgreSQL did not become ready')
    bindings = json.loads(checked(['docker', 'inspect', '--format', '{{json .NetworkSettings.Ports}}', container]))['5432/tcp']
    if len(bindings) != 1 or bindings[0]['HostIp'] != '127.0.0.1':
        raise RuntimeError('database is not loopback-only')
    port = int(bindings[0]['HostPort'])
    receipt = {'container': container, 'image': POSTGRES, 'loopback_port': port,
               'cluster': checked(['docker', 'exec', container, 'psql', '-U', 'ci_fixture', '-d', 'postgres', '-Atc', 'SELECT system_identifier FROM pg_control_system()']),
               'databases': list(FIXTURES.values())}
    (reports/'fixture-identity.json').write_text(json.dumps(receipt, indent=2)+'\n')
    lines = []
    for key, database in FIXTURES.items():
        checked(['docker', 'exec', container, 'createdb', '-U', 'ci_fixture', database])
        lines.append(key+'=postgres://ci_fixture:ci-disposable-only@127.0.0.1:'+str(port)+'/'+database+'?sslmode=disable')
    env_file = root/'fixture.env'
    env_file.write_text('\n'.join(lines)+'\n')
    env_file.chmod(0o600)
    return env_file


def native_command(image, root, env_file, token):
    return ['docker', 'run', '--rm', '--pull=never', '--name', 'recipe-native-run-'+token, '--label', 'recipe.ci.run='+token,
            *session_labels(),
            '--network=host', '--cpuset-cpus=0,1', '--cpus=2', '--memory=3g', '--memory-swap=3g', '--shm-size=512m',
            '--env-file', str(env_file), '-v', str(root/'source')+':/src:ro',
            '-v', str(root/'external')+':/src/data/external:ro',
            '-v', str(root/'reports')+':/reports', '-v', str(root)+':/work',
            '-e', 'PATH=/toolchain/bin:/usr/local/bin:/usr/bin:/bin', '-e', 'GOROOT=/toolchain',
            '-e', 'GOMODCACHE=/work/mod', '-e', 'GOCACHE=/work/cache', '-e', 'GOPATH=/work/gopath',
            '-e', 'HOME=/work/home', '-e', 'TMPDIR=/work/tmp', '-w', '/src',
            '--entrypoint', 'python3', image, '-B', 'scripts/ci/native.py']


def archive_source(root, source_sha):
    with (root/'source.tar').open('wb') as archive:
        subprocess.run(['git', 'archive', '--format=tar', source_sha], stdout=archive, check=True, timeout=120)
    with tarfile.open(root/'source.tar') as archive:
        archive.extractall(root/'source', filter='data')
    tree = subprocess.check_output(['git', 'ls-tree', '-rz', '--full-tree', source_sha], timeout=120)
    entries = {}
    for row in tree.split(b'\0'):
        if ACTIVE_GUARD is not None:
            ACTIVE_GUARD.check()
        if not row:
            continue
        header, raw_path = row.split(b'\t', 1)
        mode, kind, blob = header.decode().split()
        path = raw_path.decode()
        file = root/'source'/path
        if kind != 'blob' or mode not in ('100644', '100755', '120000'):
            raise ValueError('unsupported source archive entry')
        data = os.readlink(file).encode() if mode == '120000' else file.read_bytes()
        actual = hashlib.sha1(b'blob '+str(len(data)).encode()+b'\0'+data).hexdigest()
        if actual != blob or (mode == '120000') != file.is_symlink() or (mode != '120000' and bool(file.stat().st_mode & 0o111) != (mode == '100755')):
            raise ValueError('archived source bytes or mode differ from commit')
        entries[path] = {'mode': mode, 'blob': blob, 'sha256': hashlib.sha256(data).hexdigest()}
    actual_paths = {str(p.relative_to(root/'source')) for p in (root/'source').rglob('*') if p.is_file() or p.is_symlink()}
    if actual_paths != set(entries):
        raise ValueError('archived source entry set differs from commit')
    receipt = {'source_sha': source_sha, 'archive_sha256': digest(root/'source.tar'), 'entries': entries}
    (root/'reports'/'source-archive.json').write_text(json.dumps(receipt, indent=2)+'\n')


def prepare_external(root, retained):
    checksums = root/'source/data/external/SHA256SUMS'
    expected = {}
    for line in checksums.read_text().splitlines():
        sha, name = line.split()
        if name not in DATASETS or name in expected or not re.fullmatch(r'[0-9a-f]{64}', sha):
            raise ValueError('unexpected external fixture checksum contract')
        expected[name] = sha
    if set(expected) != set(DATASETS):
        raise ValueError('external fixture checksum contract is incomplete')
    target = root/'external'
    target.mkdir()
    (target/'SHA256SUMS').write_bytes(checksums.read_bytes())
    receipt = {}
    for name, sha in expected.items():
        source = (Path(retained)/name).open('rb') if retained else urlopen(DATASETS[name], timeout=60)
        with source, (target/name).open('xb') as output:
            size = 0
            while chunk := source.read(1024*1024):
                if ACTIVE_GUARD is not None:
                    ACTIVE_GUARD.check()
                size += len(chunk)
                if size > 32*1024**2:
                    raise ValueError('external fixture exceeds its 32 MiB bound')
                output.write(chunk)
        if digest(target/name) != sha:
            raise ValueError('external fixture checksum mismatch: '+name)
        receipt[name] = {'sha256': sha, 'bytes': size, 'source': 'retained verified file' if retained else DATASETS[name]}
    (root/'reports/external-fixtures.json').write_text(json.dumps(receipt, indent=2)+'\n')


def main():
    global ACTIVE_GUARD
    parser = argparse.ArgumentParser()
    parser.add_argument('--existing-images', type=Path)
    parser.add_argument('--work-dir', type=Path)
    parser.add_argument('--fixture-env', type=Path)
    parser.add_argument('--run-id')
    parser.add_argument('--external-data', type=Path)
    parser.add_argument('--pg-data-dir', type=Path)
    args = parser.parse_args()
    env = environment(os.environ)
    local = args.existing_images is not None
    if local:
        if not args.work_dir or not args.fixture_env or not args.external_data or not args.pg_data_dir or not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9-]{0,63}', args.run_id or ''):
            parser.error('existing-image mode requires work-dir, fixture-env, external-data, pg-data-dir and run-id')
        if not args.pg_data_dir.is_dir():
            parser.error('prepared PostgreSQL data path must exist')
        token, root = args.run_id, args.work_dir.resolve()
        fixture_environment(args.fixture_env)
        if args.existing_images.stat().st_size > 16384:
            parser.error('image handoff exceeds its bound')
        prepared = json.loads(args.existing_images.read_text())
    else:
        if args.work_dir or args.fixture_env or args.run_id or args.external_data or args.pg_data_dir:
            parser.error('local options require an existing-image handoff')
        token = os.environ.get('GITHUB_RUN_ID', '')+'-'+os.environ.get('GITHUB_RUN_ATTEMPT', '')
        if not re.fullmatch(r'[0-9]+-[0-9]+', token):
            parser.error('default mode requires a dedicated Actions run identity')
        root = Path(os.environ['RUNNER_TEMP'])/('recipe-ci-'+token)
        prepared = None
    expected = source_contract()
    verify_contract(expected['lock_sha256'], expected['ci_dockerfile_sha256'])
    root.mkdir(mode=0o700)
    for name in ('reports', 'mod', 'cache', 'tmp', 'home', 'source'):
        (root/name).mkdir()
    reports = root/'reports'
    memory = int(next(line.split()[1] for line in Path('/proc/meminfo').read_text().splitlines() if line.startswith('MemAvailable:')))*1024
    if memory < 10*1024**3:
        raise RuntimeError('native verification RAM admission failed; no opt-out')
    docker_root = Path(checked(['docker', 'info', '--format', '{{.DockerRootDir}}']))
    if not docker_root.is_absolute() or not docker_root.is_dir():
        raise ValueError('Docker filesystem root is unavailable')
    paths = {'work': root, 'docker': docker_root,
             'pg': args.pg_data_dir.resolve() if local else root/'pgdata'}
    plan = phases(local)
    try:
        receipt = admission(paths, plan)
    except RuntimeError as error:
        (reports/'capacity-admission.json').write_text(json.dumps({'error': str(error), 'phases': plan})+'\n')
        raise
    (reports/'capacity-admission.json').write_text(json.dumps(receipt, indent=2)+'\n')
    pg = 'recipe-pg-'+token
    require_unused_names(['recipe-native-run-'+token] + ([] if local else [pg]))
    try:
        ACTIVE_GUARD = SpaceGuard(paths, reports, 'prepared' if local else 'build', plan['native' if local else 'build'])
        ACTIVE_GUARD.check(force=True)
        image = select_images(prepared, expected, reports, token, env)
        if source_contract() != expected:
            raise ValueError('source changed during image preparation')
        if not local:
            ACTIVE_GUARD.finish()
            native = phases(True)['native']
            native['docker'] += PG_IMAGE
            ACTIVE_GUARD = SpaceGuard(paths, reports, 'native', native)
            ACTIVE_GUARD.check(force=True)
        (reports/'source-contract.json').write_text(json.dumps(expected, indent=2)+'\n')
        archive_source(root, expected['source_sha'])
        prepare_external(root, args.external_data)
        env_file = args.fixture_env.resolve() if local else provision(root, reports, pg, token)
        fixture_environment(env_file)
        code = run(native_command(image, root, env_file, token), reports, 'native-container', 3600, env, guard=ACTIVE_GUARD)
        result = reports/'native-result.json'
        if code or not result.is_file():
            return 1
        value = json.loads(result.read_text())
        return int(value.get('command_exit') != 0 or value.get('errors') != [])
    finally:
        try:
            if ACTIVE_GUARD is not None:
                ACTIVE_GUARD.finish()
        finally:
            ACTIVE_GUARD = None
            try:
                remove_owned('recipe-native-run-'+token, token)
            finally:
                if not local:
                    remove_owned(pg, token)


if __name__ == '__main__':
    raise SystemExit(main())
