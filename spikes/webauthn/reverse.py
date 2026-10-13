"""Reverse checks for the A1 spike: each mutation drops one rule from main.go
and the targeted tests must fail. Run it on a copy, never on the tracked
files: cp -R spikes/webauthn/. COPY_DIR/ && python3 spikes/webauthn/reverse.py COPY_DIR"""
import subprocess, sys

d = sys.argv[1]
p = d + "/main.go"
src = open(p).read()
NEVER = "len(page) < 0 && "  # keeps the condition compiling
muts = {
    "used check": ("if p.used {", "if " + NEVER + "p.used {"),
    "expiry": ("if !s.now().Before(", "if " + NEVER + "!s.now().Before("),
    "counter": ("if cred.Authenticator.CloneWarning {", "if " + NEVER + "cred.Authenticator.CloneWarning {"),
    "BE fixed": ("if stored.Flags.BackupEligible !=", "if " + NEVER + "stored.Flags.BackupEligible !="),
    "UV forced": ("session.UserVerification = protocol.VerificationRequired", "session.UserVerification = p.session.UserVerification"),
    "suspect at finish": ("if s.suspect[string(stored.ID)] {", "if " + NEVER + "s.suspect[string(stored.ID)] {"),
    "suspect excluded": ("if !s.suspect[string(c.ID)] {", "if true {"),
    "host check": ("if !s.hosts[r.Host] {", "if " + NEVER + "!s.hosts[r.Host] {"),
    "origin check": ("!s.hosts[o[7:]] ||", "false ||"),
    "one finish per begin": ("s.reg = nil // one attempt per begin", "_ = s.reg"),
    "challenge from record": ("webauthn.WithChallenge(rec.challenge())", "webauthn.WithChallenge(rnd)"),
    "raw credential ID hidden": ("res.Credential = string(parsed.AuthenticatorAttachment), credTag(parsed.RawID)",
                                 "res.Credential = string(parsed.AuthenticatorAttachment), base64.RawURLEncoding.EncodeToString(parsed.RawID)"),
    "fmt none": ('if cred.AttestationFormat != "none" {', "if " + NEVER + 'cred.AttestationFormat != "none" {'),
}
caught = 0
try:
    for name, (a, b) in muts.items():
        assert src.count(a) == 1, name
        open(p, "w").write(src.replace(a, b))
        r = subprocess.run(["go", "test", "-count=1", "./..."], cwd=d, capture_output=True, text=True)
        out = r.stdout + r.stderr
        status = "BUILD-FAIL" if "[build failed]" in out or "setup failed" in out else ("caught" if r.returncode else "NOT caught")
        caught += status == "caught"
        print(f"{name}: {status}")
finally:
    open(p, "w").write(src)
print(f"{caught}/{len(muts)}")
