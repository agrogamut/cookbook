"""Run one bounded check with a fresh environment and preserved exit evidence."""
import argparse
import datetime
import json
import os
from pathlib import Path
import signal
import subprocess
import time

LIVE_KEYS = ("GEMINI_API_KEY", "SUPABASE_SECRET_KEY", "RAZORPAY_KEY_ID",
             "RAZORPAY_KEY_SECRET", "RAZORPAY_WEBHOOK_SECRET")
ALLOWED = ("PATH", "HOME", "LANG", "GOROOT", "GOMODCACHE", "GOCACHE", "GOPATH",
           "TMPDIR", "TEST_DATABASE_URL", "CASE_SOURCE_TEST_DATABASE_URL",
           "PORTAL_TEST_DATABASE_URL", "BOOKDOCX_ARTIFACT_DIR", "BOOK_READER_EVIDENCE_DIR")


def environment(source):
    if any(source.get(key) for key in LIVE_KEYS):
        raise ValueError("live credentials must be absent from verification")
    result = {key: source[key] for key in ALLOWED if key in source}
    result.update(GOMAXPROCS="2", GOFLAGS="-p=2", GOTOOLCHAIN="local", NEXT_TELEMETRY_DISABLED="1", NODE_OPTIONS="--max-old-space-size=1536", CI="true")
    return result


def run(command, directory, name, timeout, env=None):
    if hasattr(os, 'sched_getaffinity'):
        os.sched_setaffinity(0, sorted(os.sched_getaffinity(0))[:2])
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True)
    log = directory/(name+".log")
    if log.exists():
        raise ValueError("refusing to replace existing verification log")
    record = {"command": command, "started_at": datetime.datetime.now(datetime.timezone.utc).isoformat()}
    started = time.monotonic()
    with log.open("w") as output:
        try:
            process = subprocess.Popen(command, env=environment(os.environ) if env is None else env,
                                       stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
        except OSError as error:
            output.write("check process could not start: "+type(error).__name__+"\n")
            record.update(exit=127, elapsed_seconds=time.monotonic()-started, startup_error=type(error).__name__)
            (directory/(name+".json")).write_text(json.dumps(record, indent=2)+"\n")
            return 127
        try:
            code = process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            record["timeout"] = True
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
            code = 124
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
