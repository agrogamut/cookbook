import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
from check import environment, run
from events import validate


def main():
    reports = Path('/reports')
    env = environment(os.environ)
    for name in ('go', 'git', 'chromium', 'libreoffice', 'qpdf', 'pdftotext', 'pdftohtml', 'pdftoppm', 'pdffonts', 'pdfinfo', 'fc-match', 'fc-scan', 'prlimit', 'fish'):
        if shutil.which(name, path=env['PATH']) is None:
            raise RuntimeError('missing native prerequisite: '+name)
    for name in ('TEST_DATABASE_URL', 'CASE_SOURCE_TEST_DATABASE_URL', 'PORTAL_TEST_DATABASE_URL'):
        if not env.get(name):
            raise RuntimeError('missing database fixture: '+name)
    env.update(BOOKDOCX_INTEGRATION='1', BOOK_READER_NATIVE='1', BOOKDOCX_ARTIFACT_DIR='/reports/docx', BOOK_READER_EVIDENCE_DIR='/reports/reader', CHROMIUM_NO_SANDBOX='1', CGO_ENABLED='0')
    versions = {}
    for command in (['go', 'version'], ['git', '--version'], ['chromium', '--version'], ['libreoffice', '--headless', '--version'], ['qpdf', '--version'], ['fish', '--version']):
        versions[command[0]] = subprocess.check_output(command, env=env, text=True, stderr=subprocess.STDOUT, timeout=30).strip()
    expected_go = subprocess.check_output(['python3', '-B', 'docker/pdf_runtime_lock_test.py', '--get', 'GO_VERSION'], env=env, text=True, timeout=30).strip()
    if versions['go'] != 'go version go'+expected_go+' linux/amd64':
        raise RuntimeError('actual Go toolchain differs from the source lock')
    if hashlib.sha256(Path('/usr/local/share/pdf-runtime.lock').read_bytes()).digest() != hashlib.sha256(Path('docker/pdf-runtime.lock').read_bytes()).digest():
        raise RuntimeError('installed runtime lock differs from source')
    (reports/'tool-versions.json').write_text(json.dumps(versions, indent=2)+'\n')
    shutil.copyfile('/usr/local/share/ci-packages.tsv', reports/'ci-packages.tsv')
    if run(['go', 'run', './scripts/ci/fixtures'], reports, 'fixtures', 600, env):
        return 1
    packages = subprocess.check_output(['go', 'list', './...'], env=env, text=True, timeout=120).splitlines()
    (reports/'packages.txt').write_text('\n'.join(packages)+'\n')
    code = run(['go', 'test', '-json', '-count=1', '-timeout=45m', './...'], reports, 'native-full', 3300, env)
    events, invalid = [], []
    for line in (reports/'native-full.log').read_text().splitlines():
        if not line.strip():
            continue
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            invalid.append('non-JSON command output retained in native-full.log')
    result = validate(events, code, packages)
    result['errors'].extend(invalid)
    (reports/'native-result.json').write_text(json.dumps(result, indent=2)+'\n')
    return int(bool(result['errors']))


if __name__ == '__main__':
    sys.exit(main())
