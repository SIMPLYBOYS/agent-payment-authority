"""testnet-preflight.py against a fake relayer socket and a fake node.

    python3 -I -m unittest discover -s scripts/demo -p 'test_testnet_preflight.py'

Needs local sockets (outside a sandbox that forbids them).
"""
import json
import os
import socketserver
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
GWEI = 10**9


class UnixServer(socketserver.ThreadingUnixStreamServer):
    def get_request(self):
        r, _ = super().get_request()
        return r, ("local", 0)


def run(health, usdc, gas, price=GWEI // 100):
    """The script's exit code and output with these answers."""
    class H(BaseHTTPRequestHandler):
        def log_message(self, *a):
            pass

        def reply(self, obj):
            b = json.dumps(obj).encode()
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b)

        def do_GET(self):
            self.reply(health)

        def do_POST(self):
            q = json.loads(self.rfile.read(int(self.headers["content-length"])))
            v = {"eth_call": usdc, "eth_getBalance": gas, "eth_gasPrice": price}[q["method"]]
            self.reply({"jsonrpc": "2.0", "id": 1, "result": hex(v)})

    d = tempfile.mkdtemp(dir="/tmp" if len(tempfile.gettempdir()) > 60 else None)  # a unix socket's path is short
    os.makedirs(d + "/host")
    node = HTTPServer(("127.0.0.1", 0), H)
    with open(d + "/host/payments.json", "w") as f:
        json.dump({"rpc": "http://127.0.0.1:%d" % node.server_address[1], "token": "0x" + "4" * 40, "pay_to": "0x" + "3" * 40}, f)
    with open(d + "/host/registry.json", "w") as f:
        json.dump({"wallets": [{"wallet_id": "wallet-a", "from": "0x" + "a" * 40}, {"wallet_id": "wallet-b", "from": "0x" + "b" * 40}]}, f)
    relayer = UnixServer(d + "/r.sock", H)
    for s in (node, relayer):
        threading.Thread(target=s.serve_forever, daemon=True).start()
    try:
        r = subprocess.run([sys.executable, "-I", os.path.join(HERE, "testnet-preflight.py"), d + "/r.sock", d + "/host"], capture_output=True, text=True, timeout=30)
        return r.returncode, r.stdout
    finally:
        node.shutdown()
        relayer.shutdown()


READY = {"state": "ready", "daily_tx_cap": 12, "sent_today": 8, "gas": "0x" + "1" * 40, "issuer": "0x" + "2" * 40}


class Preflight(unittest.TestCase):
    def test_enough(self):
        code, out = run(READY, 19920000, 9 * 10**13)
        self.assertEqual(code, 0, out)
        self.assertIn("relayer 今天已送 8／12 筆", out)

    def test_short_says_each(self):
        code, out = run({**READY, "sent_today": 12}, 300000, 1000)
        self.assertEqual(code, 1, out)
        self.assertIn("不夠：relayer 今天只剩 0 筆", out)
        self.assertIn("不夠：wallet-b 只有 0.30 test USDC", out)
        self.assertIn("不夠：shop 的 gas", out)

    def test_gas_need_follows_the_price(self):
        # 2 transactions x 500k gas x 2 x price: 2e15 wei at 1 gwei, so 9e13 is short then.
        self.assertEqual(run(READY, 19920000, 9 * 10**13, price=GWEI)[0], 1)

    def test_an_old_relayer_is_never_a_clean_pass(self):
        old = {k: v for k, v in READY.items() if k != "sent_today"}
        code, out = run(old, 19920000, 9 * 10**13)
        self.assertEqual(code, 2, out)
        self.assertIn("未檢查", out)

    def test_nothing_there(self):
        r = subprocess.run([sys.executable, "-I", os.path.join(HERE, "testnet-preflight.py"), "/nonexistent.sock", "/nonexistent"], capture_output=True, text=True)
        self.assertEqual(r.returncode, 2, r.stdout)


if __name__ == "__main__":
    unittest.main()
