"""Negative tests for evidence_import.py. Run from the repo root:
python3 -m unittest discover -s scripts/verify -p 'test_*.py'
"""
import errno
import hashlib
import io
import pathlib
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import evidence_import as ei  # noqa: E402

GOOD = {
    "isolation.txt": b"ok  net-external-ip: expected denied, got denied\n",
    "iso-net-external-ip.log": b"curl: connection refused\n",
    "exit-codes.txt": b"check-contracts exit=0\n",
    "check-contracts.log": b"ok\n",
}


def manifest(files):
    return "".join(f"{hashlib.sha256(d).hexdigest()}  ./{n}\n" for n, d in sorted(files.items())).encode()


def build(path, files, with_manifest=True, extra=()):
    """Writes a tar like `tar -C out -cf - .`; extra is (TarInfo, bytes|None)."""
    files = dict(files)
    if with_manifest:
        files["MANIFEST.sha256"] = manifest(files)
    with tarfile.open(path, "w:", format=tarfile.GNU_FORMAT) as tf:
        root = tarfile.TarInfo(".")
        root.type = tarfile.DIRTYPE
        tf.addfile(root)
        for name, data in files.items():
            info = tarfile.TarInfo("./" + name)
            info.size = len(data)
            tf.addfile(info, io.BytesIO(data))
        for info, data in extra:
            tf.addfile(info, io.BytesIO(data) if data is not None else None)


class ImportTest(unittest.TestCase):
    def setUp(self):
        self.tmp = pathlib.Path(tempfile.mkdtemp())
        self.archive = self.tmp / "out.tar"

    def run_import(self, mode="export-fault"):
        return ei.do_import(self.archive, self.tmp / "dest", self.tmp / "staging", mode)

    def assertRejected(self, fragment, mode="export-fault"):
        with self.assertRaises(ei.Rejected) as cm:
            self.run_import(mode)
        self.assertIn(fragment, str(cm.exception))
        self.assertFalse((self.tmp / "dest").exists(), "nothing may reach the evidence directory")

    def test_valid_archive_is_copied(self):
        build(self.archive, GOOD)
        status, count, _ = self.run_import()
        self.assertEqual((status, count), ("complete", 5))
        self.assertEqual((self.tmp / "dest" / "check-contracts.log").read_bytes(), b"ok\n")

    def test_existing_destination_is_not_overwritten(self):
        build(self.archive, GOOD)
        (self.tmp / "dest").mkdir()
        with self.assertRaises(OSError):
            self.run_import()

    def test_unsafe_paths(self):
        for name in ("../escape.log", "/etc/passwd", "a/../../b.log", "iso-x.log\\..\\y"):
            with self.subTest(name=name):
                info = tarfile.TarInfo(name)
                info.size = 1
                build(self.archive, GOOD, extra=[(info, b"x")])
                self.assertRejected("unsafe path")

    def test_links_and_special_files(self):
        cases = {
            "symlink": (tarfile.SYMTYPE, "linkname", "/etc/passwd"),
            "hardlink": (tarfile.LNKTYPE, "linkname", "isolation.txt"),
            "fifo": (tarfile.FIFOTYPE, None, None),
            "char device": (tarfile.CHRTYPE, None, None),
            "block device": (tarfile.BLKTYPE, None, None),
        }
        for label, (kind, attr, target) in cases.items():
            with self.subTest(kind=label):
                info = tarfile.TarInfo("./check.log")
                info.type = kind
                if attr:
                    setattr(info, attr, target)
                build(self.archive, GOOD, extra=[(info, None)])
                self.assertRejected("not a regular file")

    def test_unexpected_names_and_directories(self):
        info = tarfile.TarInfo("./notes.txt")
        info.size = 1
        build(self.archive, GOOD, extra=[(info, b"x")])
        self.assertRejected("not an expected evidence file")
        d = tarfile.TarInfo("./secrets")
        d.type = tarfile.DIRTYPE
        build(self.archive, GOOD, extra=[(d, None)])
        self.assertRejected("unexpected directory")

    def test_duplicate_member(self):
        info = tarfile.TarInfo("./check-contracts.log")
        info.size = 3
        build(self.archive, GOOD, extra=[(info, b"bad")])
        self.assertRejected("duplicate member")

    def test_manifest_problems(self):
        build(self.archive, GOOD, with_manifest=False)
        self.assertRejected("missing MANIFEST.sha256")
        tampered = dict(GOOD)
        files = dict(tampered, **{"MANIFEST.sha256": manifest(GOOD)})
        files["check-contracts.log"] = b"changed after the manifest\n"
        build(self.archive, files, with_manifest=False)
        self.assertRejected("does not match the manifest")
        files = dict(GOOD, **{"MANIFEST.sha256": manifest(GOOD), "check.log": b"unlisted\n"})
        build(self.archive, files, with_manifest=False)
        self.assertRejected("manifest mismatch")

    def test_missing_required_evidence(self):
        without = {k: v for k, v in GOOD.items() if k != "exit-codes.txt"}
        build(self.archive, without)
        self.assertRejected("missing exit-codes.txt")
        without = {k: v for k, v in GOOD.items() if k != "check-contracts.log"}
        build(self.archive, without)
        self.assertRejected("missing required evidence")
        build(self.archive, GOOD)
        self.assertRejected("expected", mode="full")  # full mode needs every step

    def test_packing_failure_produces_no_archive(self):
        self.archive.write_bytes(b"")
        self.assertRejected("")
        self.archive.write_bytes(b"not a tar archive at all")
        self.assertRejected("")

    def test_truncated_archive(self):
        big = dict(GOOD, **{"check-contracts.log": b"x" * 20000})
        build(self.archive, big)
        raw = self.archive.read_bytes()
        for cut in (len(raw) // 2, 700, 1536):  # mid-data and member boundaries
            with self.subTest(cut=cut):
                self.archive.write_bytes(raw[:cut])
                with self.assertRaises(ei.Rejected):
                    self.run_import()
                self.assertFalse((self.tmp / "dest").exists())

    def test_size_and_count_limits(self):
        old = ei.MAX_FILE, ei.MAX_TOTAL, ei.MAX_FILES
        try:
            ei.MAX_FILE = 100
            build(self.archive, dict(GOOD, **{"check-contracts.log": b"x" * 101}))
            self.assertRejected("exceeds 100")
            ei.MAX_FILE, ei.MAX_TOTAL = old[0], 150
            build(self.archive, dict(GOOD, **{"check-contracts.log": b"x" * 100}))
            self.assertRejected("archive content exceeds")
            ei.MAX_TOTAL, ei.MAX_FILES = old[1], 3
            build(self.archive, GOOD)
            self.assertRejected("more than 3 files")
        finally:
            ei.MAX_FILE, ei.MAX_TOTAL, ei.MAX_FILES = old

    def test_aborted_run_keeps_its_evidence(self):
        files = {"isolation.txt": b"UNEXPECTED  x\nisolation self-check failed; no checks were run\n"}
        build(self.archive, files)
        status, count, _ = self.run_import(mode="full")
        self.assertEqual((status, count), ("aborted by the isolation self-check", 2))


class BrokenSink:
    """Stand-in for a destination file whose write, flush or close fails."""

    def __init__(self, real, fail):
        self.real, self.fail = real, fail

    def write(self, data):
        if self.fail == "write":
            raise OSError(errno.ENOSPC, "No space left on device")
        return self.real.write(data)

    def flush(self):
        if self.fail == "flush":
            raise OSError(errno.EIO, "Input/output error")
        self.real.flush()

    def close(self):
        self.real.close()
        if self.fail == "close":
            raise OSError(errno.EIO, "Input/output error")


def broken_open(target, fail):
    """An open() whose file named `target` fails at `fail`; others are real."""
    real_open = open

    def fake(path, mode="r", *args, **kwargs):
        f = real_open(path, mode, *args, **kwargs)
        return BrokenSink(f, fail) if pathlib.Path(path).name == target else f

    return fake


# The child writes to both streams and exits 0, so only a capture I/O error
# can make the result non-zero.
BOTH_STREAMS_OK = "import sys; sys.stdout.write('123'); sys.stderr.write('log'); sys.exit(0)"


class CaptureTest(unittest.TestCase):
    def setUp(self):
        self.tmp = pathlib.Path(tempfile.mkdtemp())

    def capture(self, code, max_out=1000, max_err=1000, timeout=30):
        return ei.do_capture([sys.executable, "-c", code], self.tmp / "out", self.tmp / "err", max_out, max_err, timeout)

    def test_passes_exit_code_and_output(self):
        rc = self.capture("import sys; sys.stdout.write('hello'); sys.exit(3)")
        self.assertEqual(rc, 3)
        self.assertEqual((self.tmp / "out").read_bytes(), b"hello")

    def test_oversized_stdout_is_cut_and_fails(self):
        rc = self.capture("import sys\nwhile True: sys.stdout.write('x' * 65536)")
        self.assertEqual(rc, 90)
        self.assertEqual((self.tmp / "out").stat().st_size, 1000)

    def test_oversized_stderr_fails(self):
        self.assertEqual(self.capture("import sys\nwhile True: sys.stderr.write('x' * 65536)"), 90)

    def test_timeout_fails(self):
        self.assertEqual(self.capture("import time; time.sleep(30)", timeout=1), 91)

    def test_destination_cannot_be_opened(self):
        # Codex's reproduction: stderr's directory is missing, the child exits
        # 0 and stdout would be saved; the capture must still fail.
        for stream in ("out", "err"):
            with self.subTest(stream=stream):
                paths = {"out": self.tmp / f"out-{stream}", "err": self.tmp / f"err-{stream}"}
                paths[stream] = self.tmp / "missing" / stream
                rc = ei.do_capture([sys.executable, "-c", BOTH_STREAMS_OK], paths["out"], paths["err"], 1000, 1000, 30)
                self.assertEqual(rc, 92)

    def test_write_flush_close_failures(self):
        for stream in ("out", "err"):
            for fail in ("write", "flush", "close"):
                with self.subTest(stream=stream, fail=fail):
                    tmp = pathlib.Path(tempfile.mkdtemp())
                    with mock.patch.object(ei, "open", broken_open(stream, fail), create=True):
                        rc = ei.do_capture([sys.executable, "-c", BOTH_STREAMS_OK], tmp / "out", tmp / "err", 1000, 1000, 30)
                    self.assertEqual(rc, 92)
                    other = "err" if stream == "out" else "out"
                    self.assertEqual((tmp / other).read_bytes(), b"log" if other == "err" else b"123",
                                     "the other stream is saved normally")

    def test_failing_stream_does_not_leave_the_child_blocked(self):
        # The child floods stderr beyond the pipe buffer; saving stderr fails at
        # once, so nothing drains it. The child must be killed, not time out.
        flood = "import sys\nfor _ in range(200): sys.stderr.write('x' * 65536)\nsys.exit(0)"
        with mock.patch.object(ei, "open", broken_open("err", "write"), create=True):
            rc = self.capture(flood, max_err=64 * 1024 * 1024, timeout=30)
        self.assertEqual(rc, 92)


if __name__ == "__main__":
    unittest.main()
