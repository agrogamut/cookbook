"""Run one bounded check with a fresh environment and preserved exit evidence."""
import argparse
from contextlib import ExitStack
import datetime
import json
import os
from pathlib import Path
import signal
import subprocess
import time
from capacity import CapacityError, SpaceGuard

LIVE_KEYS = ("GEMINI_API_KEY", "SUPABASE_SECRET_KEY", "RAZORPAY_KEY_ID",
             "RAZORPAY_KEY_SECRET", "RAZORPAY_WEBHOOK_SECRET")
ALLOWED = ("PATH", "HOME", "LANG", "GOROOT", "GOMODCACHE", "GOCACHE", "GOPATH",
           "TMPDIR", "TEST_DATABASE_URL", "CASE_SOURCE_TEST_DATABASE_URL",
           "PORTAL_TEST_DATABASE_URL", "BOOKDOCX_ARTIFACT_DIR", "BOOK_READER_EVIDENCE_DIR")
STDERR_LIMIT = 16*1024**2


def environment(source):
    if any(source.get(key) for key in LIVE_KEYS):
        raise ValueError("live credentials must be absent from verification")
    result = {key: source[key] for key in ALLOWED if key in source}
    result.update(GOMAXPROCS="2", GOFLAGS="-p=2", GOTOOLCHAIN="local", NEXT_TELEMETRY_DISABLED="1", NODE_OPTIONS="--max-old-space-size=1536", CI="true")
    return result


def stop(process):
    if process.poll() is None:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            return
        try:
            process.communicate(timeout=15)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.communicate()


def wait(process, timeout, guard):
    deadline = time.monotonic()+timeout
    try:
        while True:
            guard.check()
            remaining = deadline-time.monotonic()
            if remaining <= 0:
                raise subprocess.TimeoutExpired(process.args, timeout)
            try:
                output = process.communicate(timeout=min(0.25, remaining))
                guard.check(force=True)
                return output
            except subprocess.TimeoutExpired:
                if time.monotonic() >= deadline:
                    raise
    except BaseException:
        stop(process)
        raise


class StderrGuard:
    def __init__(self, guard, stream):
        self.guard, self.stream = guard, stream

    def check(self, force=False):
        self.guard.check(force=force)
        if os.fstat(self.stream.fileno()).st_size > STDERR_LIMIT:
            raise CapacityError('stderr report exceeded '+str(STDERR_LIMIT)+' bytes; observed bytes retained')


def run(command, directory, name, timeout, env=None, guard=None, *, stderr_path=None):
    if hasattr(os, 'sched_getaffinity'):
        os.sched_setaffinity(0, sorted(os.sched_getaffinity(0))[:2])
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True)
    log = directory/(name+".log")
    if log.exists():
        raise ValueError("refusing to replace existing verification log")
    if stderr_path is not None:
        stderr_path = Path(stderr_path)
        if stderr_path != directory/(name+'.stderr.log') or stderr_path.exists() or stderr_path.is_symlink():
            raise ValueError('stderr report must be a fresh named sibling of the stdout log')
    record = {"command": command, "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat()}
    started = time.monotonic()
    env = environment(os.environ) if env is None else env
    own_guard = guard is None
    if own_guard:
        paths = {'reports': directory, 'cwd': Path.cwd()}
        paths.update({key: env[key] for key in ('TMPDIR', 'GOMODCACHE', 'GOCACHE', 'GOPATH') if key in env})
        guard = SpaceGuard(paths, directory, name)
    process = None
    with ExitStack() as stack:
        output = stack.enter_context(log.open("w"))
        diagnostic = stack.enter_context(stderr_path.open("x")) if stderr_path is not None else None
        command_guard = StderrGuard(guard, diagnostic) if diagnostic is not None else guard
        if diagnostic is not None:
            record.update(stderr_log=stderr_path.name, stderr_limit_bytes=STDERR_LIMIT)
        try:
            command_guard.check()
            process = subprocess.Popen(command, env=env,
                                       stdout=output, stderr=diagnostic if diagnostic is not None else subprocess.STDOUT,
                                       start_new_session=True)
            wait(process, timeout, command_guard)
            code = process.returncode
        except CapacityError as error:
            record['capacity_error'] = str(error)
            code = 125
        except OSError as error:
            (diagnostic if diagnostic is not None else output).write("check process could not start: "+type(error).__name__+"\n")
            record['startup_error'] = type(error).__name__
            code = 127
        except subprocess.TimeoutExpired:
            record["timeout"] = True
            code = 124
        finally:
            if process is not None:
                stop(process)
                record['observed_process_exit'] = process.returncode
            if diagnostic is not None:
                diagnostic.flush()
                record['stderr_bytes'] = os.fstat(diagnostic.fileno()).st_size
            if own_guard:
                guard.finish()
    record.update(exit=code, elapsed_seconds=time.monotonic()-started)
    (directory/(name+".json")).write_text(json.dumps(record, indent=2)+"\n")
    return code


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--reports", required=True)
    parser.add_argument("--name", required=True)
    parser.add_argument("--timeout", type=int, required=True)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    if not command or not 1 <= args.timeout <= 3600 or not args.name.replace("-", "").isalnum():
        parser.error("invalid command, bound or report name")
    raise SystemExit(run(command, args.reports, args.name, args.timeout))
