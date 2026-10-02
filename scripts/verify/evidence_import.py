#!/usr/bin/env python3
"""Host-side capture and import of container evidence. The archive is written
by the process under test, so it is untrusted: nothing is extracted with
tarfile.extract, and nothing reaches the evidence directory until every check
passed.

  capture --out F --err F --max-out N --max-err N --timeout S -- CMD...
      Runs CMD, keeps at most N bytes of stdout/stderr. Exceeding a cap or the
      timeout kills CMD and exits 90 (overflow) or 91 (timeout). Any I/O error
      while saving either stream (open, read, write, flush, close) kills CMD
      and exits 92, even if CMD itself succeeded. Otherwise exits with CMD's
      code.

  import --archive F --dest DIR --staging DIR --mode MODE
      Validates the archive and, only if it is valid, copies the files to DIR
      (which must not exist). Exits 0 on success, 2 on rejection.

Rejected: non-regular members (symlinks, hard links, devices, FIFOs, sparse),
absolute or ".." paths, names outside the allowlist, duplicates, per-file /
total / count limits, truncated data, files missing from or not matching
MANIFEST.sha256, and missing required evidence for MODE.
"""
import argparse
import hashlib
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import threading

MAX_FILE = 16 * 1024 * 1024
MAX_TOTAL = 64 * 1024 * 1024
MAX_FILES = 512

# Steps that must appear, in order, in exit-codes.txt for each mode.
STEPS = {
    "full": ["check-contracts", "check", "race-cover", "verify-b1-01", "guards", "evidence-import-tests", "mutation"],
    "inject": ["injected-failure", "check-contracts"],
    "export-fault": ["check-contracts"],
}
STEP_NAMES = sorted({s for steps in STEPS.values() for s in steps})
RUN_DIR = r"[0-9a-f]{12}-[0-9]{8}T[0-9]{6}Z"
ALLOWED_FILES = [re.compile(p) for p in (
    r"MANIFEST\.sha256",
    r"isolation\.txt",
    r"exit-codes\.txt",
    r"iso-[a-z0-9-]{1,40}\.log",
    r"(%s)\.log" % "|".join(map(re.escape, STEP_NAMES)),
    r"mutation/%s/(summary\.txt|baseline\.log|mutation-[0-9]{2,3}\.log)" % RUN_DIR,  # mutation-check.py: {n:02d}, past 99 three digits
)]
ALLOWED_DIRS = re.compile(r"mutation(/%s)?" % RUN_DIR)


class Rejected(Exception):
    pass


def member_name(raw):
    """Returns the clean relative name, "" for the archive root, or raises."""
    if "\x00" in raw or "\\" in raw or raw.startswith("/"):
        raise Rejected(f"unsafe path {raw!r}")
    parts = [p for p in raw.split("/") if p not in ("", ".")]
    if any(p == ".." for p in parts) or len(raw) > 200:
        raise Rejected(f"unsafe path {raw!r}")
    return "/".join(parts)


def read_archive(path):
    files, total = {}, 0
    try:
        with tarfile.open(path, "r:") as tf:
            for m in tf:
                name = member_name(m.name)
                if m.isdir():
                    if name and not ALLOWED_DIRS.fullmatch(name):
                        raise Rejected(f"unexpected directory {name}")
                    continue
                if not m.isreg() or m.issparse():
                    raise Rejected(f"{name or m.name}: not a regular file (type {m.type!r})")
                if not any(p.fullmatch(name) for p in ALLOWED_FILES):
                    raise Rejected(f"{name}: not an expected evidence file")
                if name in files:
                    raise Rejected(f"{name}: duplicate member")
                if len(files) >= MAX_FILES:
                    raise Rejected(f"more than {MAX_FILES} files")
                if m.size > MAX_FILE:
                    raise Rejected(f"{name}: {m.size} bytes exceeds {MAX_FILE}")
                total += m.size
                if total > MAX_TOTAL:
                    raise Rejected(f"archive content exceeds {MAX_TOTAL} bytes")
                data = tf.extractfile(m).read()
                if len(data) != m.size:
                    raise Rejected(f"{name}: truncated ({len(data)} of {m.size} bytes)")
                files[name] = data
    except tarfile.TarError as e:
        raise Rejected(f"unreadable or truncated archive: {e}") from e
    return files


def check_manifest(files):
    raw = files.get("MANIFEST.sha256")
    if raw is None:
        raise Rejected("missing MANIFEST.sha256")
    listed = {}
    for line in raw.decode("utf-8", "replace").splitlines():
        m = re.fullmatch(r"([0-9a-f]{64})  (?:\./)?(\S+)", line)
        if not m or m.group(2) in listed:
            raise Rejected(f"bad manifest line {line!r}")
        listed[m.group(2)] = m.group(1)
    present = set(files) - {"MANIFEST.sha256"}
    if set(listed) != present:
        raise Rejected(f"manifest mismatch: missing {sorted(set(listed) - present)}, unlisted {sorted(present - set(listed))}")
    for name, digest in listed.items():
        if hashlib.sha256(files[name]).hexdigest() != digest:
            raise Rejected(f"{name}: sha256 does not match the manifest")


def check_required(files, mode):
    if "isolation.txt" not in files:
        raise Rejected("missing isolation.txt")
    if b"isolation self-check failed" in files["isolation.txt"]:
        return "aborted by the isolation self-check"
    codes = files.get("exit-codes.txt")
    if codes is None:
        raise Rejected("missing exit-codes.txt")
    names = [line.split(" exit=")[0] for line in codes.decode("utf-8", "replace").splitlines() if line]
    if names != STEPS[mode]:
        raise Rejected(f"exit-codes.txt lists {names}, expected {STEPS[mode]} for mode {mode}")
    missing = [n + ".log" for n in names if n + ".log" not in files]
    if "mutation" in names and not any(re.fullmatch(r"mutation/%s/summary\.txt" % RUN_DIR, f) for f in files):
        missing.append("mutation/<run>/summary.txt")
    if missing:
        raise Rejected(f"missing required evidence {missing}")
    return "complete"


def do_import(archive, dest, staging, mode):
    dest, staging = pathlib.Path(dest), pathlib.Path(staging)
    files = read_archive(archive)
    check_manifest(files)
    status = check_required(files, mode)
    staging.mkdir()  # fails if it exists
    for name, data in files.items():
        target = staging / name
        target.parent.mkdir(parents=True, exist_ok=True)
        with open(target, "xb") as f:
            f.write(data)
    shutil.copytree(staging, dest)  # fails if dest exists
    return status, len(files), sum(map(len, files.values()))


def do_capture(cmd, out, err, max_out, max_err, timeout):
    # Open both destinations before starting CMD: a destination that cannot be
    # opened fails the capture without running anything.
    sinks = []
    try:
        for path in (out, err):
            sinks.append(open(path, "xb"))
    except OSError as e:
        for f in sinks:
            f.close()
        print(f"capture: cannot open output: {e}", file=sys.stderr)
        return 92
    try:
        p = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    except OSError as e:
        for f in sinks:
            f.close()
        print(f"capture: cannot start command: {e}", file=sys.stderr)
        return 92
    overflow, errors = [], []

    # pump runs in a thread; every exception is handed back through errors and
    # kills CMD, so a stream that stops being drained cannot block it forever.
    def pump(src, f, limit, label):
        n = 0
        try:
            while chunk := src.read(65536):
                if n + len(chunk) > limit:
                    f.write(chunk[: limit - n])
                    overflow.append(label)
                    p.kill()
                    break
                f.write(chunk)
                n += len(chunk)
            f.flush()
        except BaseException as e:  # noqa: BLE001 - any failure must reach the caller
            errors.append(f"{label}: {e!r}")
            p.kill()
        finally:
            try:
                f.close()
            except BaseException as e:  # noqa: BLE001
                errors.append(f"{label} close: {e!r}")
                p.kill()

    threads = [threading.Thread(target=pump, args=a) for a in ((p.stdout, sinks[0], max_out, "stdout"), (p.stderr, sinks[1], max_err, "stderr"))]
    for t in threads:
        t.start()
    timed_out = False
    try:
        rc = p.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        timed_out = True
        p.kill()
        rc = p.wait()
    for t in threads:
        t.join()
    p.stdout.close()
    p.stderr.close()
    if errors:
        print(f"capture: saving output failed ({'; '.join(errors)}); command killed", file=sys.stderr)
        return 92
    if overflow:
        print(f"capture: {', '.join(overflow)} exceeded its limit; command killed", file=sys.stderr)
        return 90
    if timed_out:
        print(f"capture: command exceeded {timeout}s; killed", file=sys.stderr)
        return 91
    return rc


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)
    c = sub.add_parser("capture")
    c.add_argument("--out", required=True)
    c.add_argument("--err", required=True)
    c.add_argument("--max-out", type=int, required=True)
    c.add_argument("--max-err", type=int, required=True)
    c.add_argument("--timeout", type=int, required=True)
    c.add_argument("argv", nargs=argparse.REMAINDER)
    i = sub.add_parser("import")
    i.add_argument("--archive", required=True)
    i.add_argument("--dest", required=True)
    i.add_argument("--staging", required=True)
    i.add_argument("--mode", choices=sorted(STEPS), required=True)
    a = ap.parse_args()
    if a.cmd == "capture":
        argv = a.argv[1:] if a.argv[:1] == ["--"] else a.argv
        sys.exit(do_capture(argv, a.out, a.err, a.max_out, a.max_err, a.timeout))
    try:
        status, count, size = do_import(a.archive, a.dest, a.staging, a.mode)
    except (Rejected, OSError) as e:
        print(f"import rejected: {e}")
        sys.exit(2)
    print(f"import ok: {status}; {count} files, {size} bytes")


if __name__ == "__main__":
    main()
