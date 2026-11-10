#!/usr/bin/env python3
"""The demo video's two cuts (docs/demo-video-script.md §3, §4, §7): the simulation that film.mjs filmed, a real
Base Sepolia clip of 11/6 (rec1) and title cards, cut row by row as the script's tables time them; each row's
narration as subtitles (Chinese over English) in a band under the picture, also as two SRT files.

    python3 -I scripts/video/cut.py --cut 90|180|addon|transit [--out artifacts/video]

--cut addon: the hotel's and the post office's add-on clips (apa-addon-hotel, apa-addon-post), from addon-present/.
--cut transit: the ATM and the subway's gate (apa-addon-transit), from transit-present/ (film.mjs --stage transit).

Inputs (artifacts/video/, git-ignored): full-present4/ and full-show5/ (full.mp4, marks.json: film.mjs --stage full,
--view present and show), fooled3/fooled.mp4 (--stage fooled; 11/10, the town with its homes, food trucks and hospital done), rec1/rec1-cforce.mov (the 11/6 recording: only the
browser's page is cropped from it, never the screen around it) and checker-rec1.json (verify-demo on that run's
evidence pack). The simulation's frames carry their own label (film.mjs); the real clip gets its own here.
This ffmpeg has no drawtext and no libass: every text is a PNG drawn here and overlaid.
"""
import argparse
import json
import os
import re
import subprocess
import sys

from PIL import Image, ImageDraw, ImageFont

ROOT = os.path.normpath(os.path.join(os.path.dirname(__file__), "../.."))
V = os.path.join(ROOT, "artifacts/video")
FONT = "/System/Library/Fonts/PingFang.ttc"
W, H, VW, VH = 1920, 1080, 1728, 972  # the frame; the picture's area above the 108 px subtitle band
INK, BG, ACCENT, SOFT = (243, 237, 226), (24, 22, 18), (226, 106, 59), (170, 160, 145)
REAL = "Base Sepolia 實跑（11/6）：真實 Agent，本人以裝置＋語音核准"


def font(size):
    return ImageFont.truetype(FONT, size)


def wrap(d, text, f, width):
    """Lines of text that fit width (CJK breaks anywhere; a space-separated run stays whole)."""
    lines, cur = [], ""
    for ch in text:
        if d.textlength(cur + ch, font=f) > width and cur:
            lines.append(cur)
            cur = ch.lstrip()
        else:
            cur += ch
    return lines + ([cur] if cur else [])


def fit(d, text, size, least, width):
    """The largest font from size down to least that fits text in width (a long narration shrinks, never cut)."""
    while size > least and d.textlength(text, font=font(size)) > width:
        size -= 1
    return font(size)


def band(zh, en, path):
    im = Image.new("RGB", (W, H - VH), (0, 0, 0))
    d = ImageDraw.Draw(im)
    d.text((W // 2, 30), zh, font=fit(d, zh, 34, 22, W - 120), fill=INK, anchor="mm")
    d.text((W // 2, 78), en, font=fit(d, en, 22, 16, W - 120), fill=SOFT, anchor="mm")
    im.save(path)


def tag(text, path):
    f = font(24)
    w = int(ImageDraw.Draw(Image.new("RGB", (1, 1))).textlength(text, font=f)) + 28
    im = Image.new("RGBA", (w, 44), (0, 0, 0, 150))
    ImageDraw.Draw(im).text((14, 6), text, font=f, fill=(255, 255, 255, 255))
    im.save(path)


def card(path, title, lines=(), small=(), kind="title"):
    im = Image.new("RGB", (VW, VH), BG)
    d = ImageDraw.Draw(im)
    if kind == "terminal":
        im.paste((12, 12, 14), (0, 0, VW, VH))
        y = 70
        for ln in wrap(d, title, font(28), VW - 160):
            d.text((80, y), ln, font=font(28), fill=SOFT)
            y += 42
        y += 40
        for ln in lines:
            ok = ln.endswith("PASS") or "PASS (exit 0)" in ln
            d.text((80, y), ln, font=font(30), fill=(120, 220, 140) if ok else INK)
            y += 48
        for s in small:
            d.text((80, VH - 90), s, font=font(24), fill=SOFT)
        im.save(path)
        return
    ft = font(64)
    y = VH // 2 - 60 * (len(lines) + 1)
    for ln in wrap(d, title, ft, VW - 240):
        d.text((VW // 2, y), ln, font=ft, fill=INK, anchor="mm")
        y += 86
    y += 20
    for ln in lines:
        for part in wrap(d, ln, font(40), VW - 240):
            d.text((VW // 2, y), part, font=font(40), fill=ACCENT, anchor="mm")
            y += 60
    y += 30
    for s in small:
        for part in wrap(d, s, font(26), VW - 300):
            d.text((VW // 2, y), part, font=font(26), fill=SOFT, anchor="mm")
            y += 40
    im.save(path)


def ff(*args):
    subprocess.run(["ffmpeg", "-v", "error", "-y", *args], check=True)


def segment(i, part, dur, zh, en, work):
    """One part of a row as a 1920x1080 30 fps file: the picture in its area, the row's subtitles under it."""
    out = os.path.join(work, "seg-%03d.mp4" % i)
    bp = os.path.join(work, "band-%03d.png" % i)
    band(zh, en, bp)
    enc = ["-c:v", "libx264", "-pix_fmt", "yuv420p", "-r", "30", "-an", out]
    # The picture over the band, stacked (overlaying the band's PNG made 1081 rows, which H.264 refuses).
    stack = "format=yuv420p[v];[1:v]format=yuv420p[b];[v][b]vstack=inputs=2:shortest=1"
    if part["kind"] == "card":
        ff("-loop", "1", "-t", "%.3f" % dur, "-i", part["png"], "-loop", "1", "-i", bp, "-filter_complex",
           "[0:v]pad=%d:%d:%d:0:black,%s" % (W, VH, (W - VW) // 2, stack), "-t", "%.3f" % dur, *enc)
        return out
    src, start, length = part["src"], part["start"], part["len"]
    speed = length / dur
    crop = part.get("crop")  # x, y, w, h of the browser's page in the real recording
    pre = ("crop=%d:%d:%d:%d," % (crop[2], crop[3], crop[0], crop[1])) if crop else ""
    inputs = ["-ss", "%.3f" % start, "-t", "%.3f" % length, "-i", src, "-loop", "1", "-i", bp]
    chain = "[0:v]%ssetpts=(PTS-STARTPTS)/%.4f,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black" % (pre, speed, VW, VH, W, VH)
    if part.get("tag"):
        tp = os.path.join(work, "tag-%03d.png" % i)
        tag(part["tag"], tp)
        inputs += ["-i", tp]
        chain += "[c];[c][2:v]overlay=%d:%d" % ((W - VW) // 2 + 28, 24)  # top left: the simulation's own label is bottom left
    ff(*inputs, "-filter_complex", chain + "," + stack, "-t", "%.3f" % dur, *enc)
    return out


def srt_time(t):
    ms = int(round(t * 1000))
    return "%02d:%02d:%02d,%03d" % (ms // 3600000, ms // 60000 % 60, ms // 1000 % 60, ms % 1000)


def render(rows, work, name):
    """rows as one file and its two SRTs: each row's parts in turn, its narration under all of them."""
    segs, zh_srt, en_srt, t = [], [], [], 0.0
    for k, (zh, en, parts) in enumerate(rows):
        row = sum(d for _, d in parts)
        zh_srt.append("%d\n%s --> %s\n%s\n" % (k + 1, srt_time(t), srt_time(t + row), zh))
        en_srt.append("%d\n%s --> %s\n%s\n" % (k + 1, srt_time(t), srt_time(t + row), en))
        for part, d in parts:
            segs.append(segment(len(segs), part, d, zh, en, work))
        t += row
    lst = os.path.join(work, "segments.txt")
    open(lst, "w").write("".join("file '%s'\n" % s for s in segs))
    ff("-f", "concat", "-safe", "0", "-i", lst, "-c", "copy", name + ".mp4")
    open(name + ".zh.srt", "w", encoding="utf-8").write("\n".join(zh_srt))
    open(name + ".en.srt", "w", encoding="utf-8").write("\n".join(en_srt))
    print("cut: %s.mp4 (%.1f s, %d segments), %s.zh.srt, %s.en.srt" % (name, t, len(segs), name, name))


def addon(out):
    """The two add-on clips (docs/commercial-post-hotel.md N6): the hotel and the post office, each on its own,
    from film.mjs --stage full --view present --venues hotel,post (addon-present2/: the post office's refund, 11/8)."""
    P = os.path.join(V, "addon-present2/full.mp4")
    if not os.path.exists(P):
        sys.exit("cut: missing " + P + " (film.mjs --stage full --view present --venues hotel,post --out " + os.path.dirname(P) + ")")
    m = json.load(open(os.path.join(V, "addon-present2/marks.json")))

    def clip(start, length):
        return dict(kind="clip", src=P, start=start, len=length)

    for venue, title, lead, rows in [
        ("hotel", "加演：飯店", "訂房頁的收款人被換掉", [
            ("家長的 AI 訂首爾的飯店。訂房頁的付款對象被換成另一個地址，就像假的訂房網站。",
             "The parent's AI books a hotel in Seoul. The booking page's payee has been swapped, like a fake booking site.",
             [(clip(m["hotel-start"] - 1.0, HOTEL[0]), HOTEL[1])]),
            ("收款人不在允許的商店名單，付款前就被拒絕，錢沒有動。孩子的早餐券在範圍內，照常付款。",
             "The payee is not on the allowed list, so it is refused before paying: no money moves. The child's breakfast voucher goes through.",
             [(clip(m["hotel-start"] - 1.0 + HOTEL[0], HOTEL[2]), HOTEL[3])]),
        ]),
        ("post", "加演：郵局", "付了錢，卻沒拿到東西", [
            ("家長的 AI 付郵資，寄包裹給外婆。付款完成了，郵局卻沒有交出寄件單。",
             "The parent's AI pays the postage for a parcel to grandma. The payment goes through, but no label is issued.",
             [(clip(m["post-start"] - 1.0, POST[0]), POST[1])]),
            ("系統以鏈上的結果為準：記下「已付款、交付失敗」，不會再付一次。",
             "The chain decides: paid, not delivered, and never paid twice.",
             [(clip(m["post-start"] - 1.0 + POST[0], POST[2]), POST[3])]),
            ("該不該退由人判斷：主持人按「商店退款」，錢退回客人的錢包，鏈上確認後才記下。",
             "A person decides: the host asks the shop to refund, and it is recorded once the chain shows the money back.",
             [(clip(m["post-start"] - 1.0 + POST[0] + POST[2], POST[4]), POST[5])]),
        ]),
    ]:
        work = os.path.join(out, "cut-addon-%s-work" % venue)
        os.makedirs(work, exist_ok=True)
        p = os.path.join(work, "title.png")
        card(p, title, [lead], ["示意動畫：fake Agent・本機測試鏈"])
        rows[0][2].insert(0, (dict(kind="card", png=p), 3.0))
        render(rows, work, os.path.join(out, "apa-addon-%s" % venue))


def transit(out):
    """The ATM and the subway's gate (docs/commercial-atm.md, docs/commercial-transit.md): one add-on clip, cut at the
    moments film.mjs --stage transit marked (no seconds read off by hand); the walks faster, the answers at speed."""
    P = os.path.join(V, "transit-present/full.mp4")
    if not os.path.exists(P):
        sys.exit("cut: missing " + P + " (film.mjs --stage transit --out " + os.path.dirname(P) + ")")
    m = json.load(open(os.path.join(V, "transit-present/marks.json")))

    def span(a, b, speed=1.0):  # the film from mark a to mark b ("name", "name+s", "name-s"), shown speed times as fast
        def t(k):
            name, off = re.match(r"^([a-z0-9-]+?)([+-][0-9.]+)?$", k).groups()
            return m[name] + float(off or 0)
        return (dict(kind="clip", src=P, start=t(a), len=t(b) - t(a)), (t(b) - t(a)) / speed)

    rows = [
        ("錢包裡的測試 USDC 從哪來？家長走到街上的 ATM：本機鏈當場鑄造，每人每天最多 20；testnet 則到 Circle 的 faucet 領。",
         "Where does the test USDC come from? The parent stops at the street's ATM: minted on the local chain, at most 20 a day; on the testnet, Circle's faucet.",
         [span("start", "atm", 2.0), span("atm", "atm+4.5")]),
        ("第一次進站，閘門關著：家長在裝置上確認一份捷運委託，每趟最多 0.02，總共 0.20。",
         "At the first gate, the parent confirms a transit delegation on the device: at most 0.02 a ride, 0.20 in all.",
         # the device's confirmation is instant here (a virtual authenticator): the gate's words slowed to be read
         [span("stairs", "opening-0.5", 1.5), span("opening-0.5", "ride1+0.5", 0.35)]),
        ("Policy 一核准，閘門就開、扣 0.02；鏈上結算在背景完成，人不必停下來付錢。",
         "The gate opens on the Policy's approval and takes 0.02; settlement completes on chain in the background, with no stop to pay.",
         [span("ride1+0.5", "ride1+3"), span("ride1+3", "out", 2.0)]),
        ("到了海灘站，出站不收費。",
         "At the beach, going out is free.",
         [span("out", "in-go", 2.0)]),
        ("再進站，不用再確認：第二趟自動扣款。超過額度或次數，閘門就不開。",
         "Going in again needs no confirmation: the second fare is taken at once. Past its budget or its rate, the gate stays shut.",
         [span("in-go", "ride2"), span("ride2", "end")]),
    ]
    work = os.path.join(out, "cut-addon-transit-work")
    os.makedirs(work, exist_ok=True)
    p = os.path.join(work, "title.png")
    card(p, "加演：地下鐵", ["邊走邊付：過閘門自動扣款"], ["示意動畫：fake Agent・本機測試鏈"])
    rows[0][2].insert(0, (dict(kind="card", png=p), 3.0))
    render(rows, work, os.path.join(out, "apa-addon-transit"))


# The add-ons' rows, in film seconds from a second before each venue's start: each row's film and its length on
# screen (faster when shorter), read off addon-present2/full.mp4 (11/8): the hotel's Deny shows 8.5 s in, the
# child's receipt 20.9 s; the post office's failed delivery 20.4 s in, the refund pressed 26.7 s in and recorded
# 0.4 s later (a local chain), the film ending 6 s after.
HOTEL = (8.5, 8.5, 16.7, 13.0)
POST = (18.8, 12.5, 7.7, 8.0, 6.3, 7.5)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--cut", choices=["90", "180", "addon", "transit"], required=True)
    ap.add_argument("--out", default=V)
    a = ap.parse_args()
    if a.cut == "addon":
        return addon(a.out)
    if a.cut == "transit":
        return transit(a.out)
    work = os.path.join(a.out, "cut-%s-work" % a.cut)
    os.makedirs(work, exist_ok=True)
    P = os.path.join(V, "full-present4/full.mp4")  # the town as of 11/10 (Aaron: 主片請再重拍); 11/9's: -present3, -show3, fooled2; 11/6's: -present2, -show2, fooled
    S = os.path.join(V, "full-show5/full.mp4")  # its tour waited 20 s: the food court and the east end (film.mjs)
    F = os.path.join(V, "fooled3/fooled.mp4")
    R = os.path.join(V, "rec1/rec1-cforce.mov")
    for f in (P, S, F, R, os.path.join(V, "checker-rec1.json")):
        if not os.path.exists(f):
            sys.exit("cut: missing " + f + " (film.mjs, the 11/6 recording, verify-demo)")
    pm = json.load(open(os.path.join(V, "full-present4/marks.json")))
    sm = json.load(open(os.path.join(V, "full-show5/marks.json")))
    rep = json.load(open(os.path.join(V, "checker-rec1.json")))
    PAGE = (0, 176, 3840, 1984)  # the browser's page in rec1-cforce.mov (its tabs, address bar and bookmarks above y 176)

    def clip(src, start, length, **kw):
        return dict(kind="clip", src=src, start=start, len=length, **kw)

    FOOD = 13.0  # S's tour this far in: the hotel leaving, the food court's trucks, clerks and customers, then the east end (Aaron 11/10)

    def png(name, *args, **kw):
        p = os.path.join(work, name + ".png")
        card(p, *args, **kw)
        return dict(kind="card", png=p)

    fears = png("fears", "讓 AI 替我花錢，我最怕三件事", ["被騙著亂花", "個資被拿走", "出了事說不清楚"])
    credential = png("credential", "家長已完成身分驗證，拿到憑證", [], ["eKYC 是下一期；這裡的憑證由測試發行者直接發"])
    claims = ["%s %s" % (k, v) for k, v in sorted(rep["claims"].items(), key=lambda kv: int(kv[0][1:]))]
    checker = png("checker", "$ go run ./cmd/verify-demo --mode e1a --run-dir artifacts/e1a/recording-rec1 --rpc https://sepolia.base.org",
                  ["   ".join(claims[:5]), "   ".join(claims[5:]), "facilitator %s" % rep["facilitator"], "settlements %d（送出者：商店）" % len(rep["settlements"]),
                   "agents " + "、".join("ERC-8004 #" + x["agent_registry_ref"].split("/")[-1] for x in rep["agents"]),
                   "verify-demo: %s: PASS (exit 0)" % rep["run_id"]],
                  ["11/6 Base Sepolia 實跑那一場的證據包，checker 向鏈重算的輸出（照錄）"], kind="terminal")
    ending = png("ending", "x402 處理報價、付款與結算；這一層證明這筆錢是為了哪個任務、依哪份委託、經過哪條規則與哪一次本人核准才付出去的。",
                 ["Agent Payment Authority"], ["x402 moves the money. This layer proves who allowed it, for what, and within which limits."])
    if a.cut == "90":
        rows = [
            ("AI 已經可以替我們買東西了。可是讓 AI 替我花錢，我最怕三件事：被騙著亂花、個資被拿走、出了事說不清楚。",
             "AI can shop for us now. But letting it spend my money, I worry about three things.",
             [(fears, 3.5), (clip(P, pm["tour"], 4.0), 3.0), (clip(S, sm["tour"] + FOOD, 4.5), 3.5)]),
            ("AI 只能提出要買什麼。家長先在自己的裝置上設好：這次最多花 1.5，超過 0.1 都要問我。",
             "The AI can only propose. The parent sets the limits on their own device: at most 1.50, over 0.10 comes back to them.",
             [(credential, 4.0), (clip(P, pm["books-start"] - 1.0, 8.0), 8.0)]),
            ("書店的頁面藏了一句話，叫 AI 順便買進階版。真的 AI 沒上當；就算被騙了，也花不出預算外的錢。",
             "The shop's page hides an instruction to buy a premium edition. The real AI ignored it; a fooled one still can't overspend.",
             [(clip(P, pm["books-ask"] - 0.5, 8.5), 8.5), (clip(F, 2.0, 9.5, tag="模擬：照腳本相信「系統通知」的 Agent"), 9.5)]),
            ("孩子的 AI 想訂 0.42 的生日蛋糕，超過範圍，交給家長核准。這間店，什麼個資都看不到。",
             "The child's AI wants a 0.42 birthday cake: over the limit, so the parent approves. The shop sees no personal data.",
             [(clip(P, pm["cafe-start"] + 1.5, 14.0), 14.0)]),
            ("便利商店賣酒，只問一件事：滿 18 歲了嗎？孩子的 AI 想買，付款前就被擋下，錢沒有動。",
             "At the corner store, beer needs one fact: over 18? The child's AI is stopped before any money moves.",
             [(clip(P, pm["mart-start"], 20.0), 12.0)]),
            ("每一步都留下證據：零知識證明預留預算，結算記在鏈上，交付留下收據。",
             "Every step leaves evidence: a zero-knowledge proof reserves the budget, settlement lands on chain, delivery leaves a receipt.",
             [(clip(S, sm["books-done"] - 6.0, 14.0), 14.0)]),
            ("出了事，任何人都能自己查證。", "And if anything goes wrong, anyone can check for themselves.",
             [(checker, 6.0), (ending, 4.0)]),
        ]
    else:
        roles = png("roles", "誰握有什麼權力", ["AI：只能提出", "替它跑的程式：沒有私鑰", "簽名的鎖（Signer）：只簽本人同意過的範圍", "查帳的人：只讀證據和鏈"])
        limits = png("limits", "照實說", ["Base Sepolia 測試網、測試幣", "被騙的 Agent 是模擬", "憑證由測試發行者發；eKYC 是下一期"],
                     ["其餘畫面是自動產生的示意動畫：fake Agent、本機測試鏈；AI 的鏈上身分由平台代為註冊"])
        rows = [
            ("讓 AI 替我花錢，最怕被騙著亂花、個資被拿走、出了事說不清楚。",
             "Letting AI spend for me, I fear being tricked, losing my data, and not knowing what happened.",
             [(fears, 4.0), (clip(P, pm["tour"], 8.5), 9.0), (clip(S, sm["tour"] + FOOD - 2.0, 7.0), 7.0)]),
            ("AI 只能提出；替它跑的程式沒有私鑰；簽名在另一個系統帳號裡，只簽本人同意過的範圍；查帳的人只讀證據和鏈。",
             "The AI proposes; the program running it holds no key; signing lives apart, within the person's limits; the auditor reads only evidence and chain.",
             [(roles, 20.0)]),
            ("憑證證明「我是誰」；委託寫下能花多少、超過多少要問。孩子的委託，由家長在自己的裝置上確認。",
             "The credential says who I am; the mandate says how much, and when to ask. The child's is confirmed on the parent's device.",
             [(credential, 5.0), (clip(P, pm["books-start"] - 1.0, 15.0), 15.0)]),
            ("書店只知道你在哪個國家；超過範圍由本人核准，還要念出這一筆的金額；誘導文字騙不倒真實的 AI，騙倒了也花不出預算外的錢。",
             "The bookshop learns only your country; above the limit the person approves and reads the amount aloud; a fooled AI still can't overspend.",
             [(clip(P, pm["books-ask"] - 1.0, 22.0), 22.0), (clip(R, 334.0, 12.0, crop=PAGE, tag=REAL), 12.0),
              (clip(F, 1.0, 11.0, tag="模擬：照腳本相信「系統通知」的 Agent"), 11.0)]),
            ("買蛋糕不需要知道你是誰。孩子的大額購買，由家長核准。",
             "Buying a cake needs nothing about you. The child's bigger purchase goes to the parent.",
             [(clip(P, pm["cafe-start"], 25.0), 25.0)]),
            ("同一份憑證，在這裡只說「滿不滿 18 歲」，不給生日。孩子的 AI 買酒，付款前就被擋下。",
             "The same credential says only over 18 or not, never a birth date. The child's AI is stopped before paying.",
             [(clip(P, pm["mart-start"], 20.0), 20.0)]),
            ("查帳不用相信這個系統：只讀證據、向鏈重算，十項全部通過。",
             "The check trusts none of this system: it reads the evidence, recomputes against the chain, and all ten pass.",
             [(clip(S, sm["books-done"] - 4.0, 10.0), 10.0), (checker, 8.0)]),
            ("x402 處理付款；這一層證明這筆錢是為了哪個任務、經過誰的同意才付出去的。",
             "x402 moves the money; this layer proves who allowed it, for what, and within which limits.",
             [(limits, 7.0), (ending, 5.0)]),
        ]
    render(rows, work, os.path.join(a.out, "apa-demo-%ss" % a.cut))


if __name__ == "__main__":
    main()
