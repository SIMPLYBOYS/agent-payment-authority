#!/usr/bin/env python3
"""A shop's mock (docs/commercial-interiors-v2.md 7): its layout drawn as Unity draws it, in seconds, to look at
before Unity's ten-minute build. It reads the same files (Layouts/<key>.json, town.json's "inside" people,
limezu-assets.txt) with the same rules: 32 px a tile, y up, a prop's bottom centre at (x, y), lift, under; the
order backdrop, rugs, props and people by y, signs, A and B, notes. The shell is rooms.py's.

    LIMEZU_DIR=/path/to/limezu python3 scripts/unity/mock.py OUT_DIR post hotel

LIMEZU_DIR: folders (":" between them) holding Modern_Interiors_v41.4/ (and Modern_Exteriors_v42.3/); a file
already imported into Assets/LimeZu/ is read from there. Writes OUT_DIR/mock-<key>.png at 2x (not in git: LimeZu's
art), with the pavement to the left. It prints what the Unity tests would refuse, with their rules
(ShopSceneTests, TownTests): A's and B's feet on a prop's drawn pixels along their corridor; a person's body over
A's or B's along it; a person's face under A's or B's label where they wait (the labels' boxes are estimates).

    python3 scripts/unity/mock.py OUT_DIR town:78,0,100,20

draws a part of the town (x0,y0,x1,y1 in tiles) as TownView does (Aaron 11/7: the camping area as food trucks): the
ground in file order, tiled from its bottom left; the props by their bottom centre and the venues' pieces by their
bottom left, by depth (Order: lower y in front); the passers-by where their paths start; no cars. OUT_DIR/mock-town.png.
"""
import json
import os
import sys

from PIL import Image, ImageDraw, ImageFont

sys.path.insert(0, os.path.dirname(__file__))
import rooms  # noqa: E402

PROJ = os.path.join(rooms.ROOT, "unity/AgentPaymentAuthority")
PAD, S = 2.5, 2  # the pavement's tiles left of the room; the scale
FONT = os.path.join(rooms.ROOT, "var/fonts/NotoSansTC[wght].ttf")  # the scene's font in full (font-subset.py --src)
# ShopView: the corridor's points and lines, B's step aside; ShopSceneTests' feet; TownTests' body and face.
POINTS = ["out", "outRow", "door", "aisle", "shelf", "bend", "front", "counter", "pickupUp", "pickupRow", "pickup"]
LINES = [(0, 1), (1, 2), (2, 3), (1, 3), (3, 4), (4, 5), (5, 6), (6, 7), (5, 8), (8, 9), (9, 10)]


def pack_dirs():
    return [os.path.join(PROJ, "Assets/LimeZu")] + [d for d in os.environ.get("LIMEZU_DIR", "").split(":") if d]


def manifest():
    m = {}
    for line in open(os.path.join(PROJ, "limezu-assets.txt"), encoding="utf-8"):
        if line.startswith("#") or not line.strip():
            continue
        key, rel, rect = line.rstrip("\n").split("\t")
        m[key] = (rel, rect)
    return m


def art(m, key):
    rel, rect = m[key]
    for d in pack_dirs():
        if os.path.isfile(os.path.join(d, rel)):
            im = Image.open(os.path.join(d, rel)).convert("RGBA")
            break
    else:
        raise SystemExit("not found in LIMEZU_DIR or Assets/LimeZu: %s %s" % (key, rel))
    if rect not in ("-", "sheet"):
        x, y, w, h = map(int, rect.split(";")[0].split(","))  # frames cut one by one: the first
        im = im.crop((x, y, x + w, y + h))
    return im


def order(y):
    return 2000 - round(y * 20)  # TownView.Order


def render_town(out_dir, m, town, x0, y0, x1, y1):
    W, H = int((x1 - x0) * 32), int((y1 - y0) * 32)
    im = Image.new("RGBA", (W, H), (40, 40, 50, 255))
    px = lambda x, y: (int(round((x - x0) * 32)), int(round((y1 - y) * 32)))
    draws = []  # (order, seq, image, left, top)

    def piece(p, ox, oy, o, seq, centre):
        s = art(m, p["key"])
        if p.get("frames", 0) > 1:
            s = s.crop((0, 0, s.width // p["frames"], s.height))
        if p.get("w", 0) > 0:  # tiled from its bottom left over w x h
            tw, th = int(p["w"] * 32), int((p.get("h") or s.height / 32) * 32)
            t = Image.new("RGBA", (tw, th))
            for yy in range(th - s.height, -s.height, -s.height):
                for xx in range(0, tw, s.width):
                    t.alpha_composite(s, (xx, yy))
            s = t
        x, y = px(ox + p["x"], oy + p["y"])
        draws.append((o, seq, s, x - (s.width // 2 if centre else 0), y - s.height))

    for i, g in enumerate(town["ground"]):
        piece(g, 0, 0, -1000 + i, i, False)
    for i, p in enumerate(town["props"]):
        piece(p, 0, 0, order(p["y"]), 1000 + i, True)
    for v in town["venues"]:
        bottom = min(e["y"] for e in v["exterior"])
        for i, e in enumerate(v["exterior"]):
            piece(e, v["x"], v["y"], order(v["y"] + bottom) + i, 2000 + i, False)
        if v.get("doorway", {}).get("key"):
            piece(v["doorway"], v["x"], v["y"], order(v["y"] + bottom) + len(v["exterior"]), 3000, False)
    for j, w in enumerate(town["walkers"]):
        s = art(m, w["key"]); s = s.crop((0, 0, 32, s.height))
        x, y = px(w["path"][0]["x"], w["path"][0]["y"])
        draws.append((order(w["path"][0]["y"]), 4000 + j, s, x - 16, y - s.height))
    for o, _, s, x, y in sorted(draws, key=lambda d: (d[0], d[1])):
        if x < W and y < H and x + s.width > 0 and y + s.height > 0:
            im.alpha_composite(s, (x, y)) if x >= 0 and y >= 0 else im.paste(s, (x, y), s)
    out = os.path.join(out_dir, "mock-town.png")
    im.resize((W * S, H * S), Image.NEAREST).convert("RGB").save(out)
    print(out)


def P(l, who, n):
    """ShopView.P, in version 2 (two customers)."""
    row = 0.6 if who == "b" else 3.0
    v = lambda k: (l[k]["x"], l[k]["y"])
    r = l.get("pickupRow") or l["bend"]["y"]
    p = [(-1.3, row), (1.5, row), v("door"), v("aisle"), v("shelf"), v("bend"), v("front"), v("counter"),
         (l["bend"]["x"], r), (l["pickup"]["x"], r), v("pickup")][n]
    if who == "b" and n >= 3:
        p = (p[0], p[1] - 0.45)
    if who == "b" and n >= 4:
        p = (p[0] - 1.0, p[1])
    return p


def samples(path):
    if len(path) == 1:
        return list(path)
    out = []
    for (ax, ay), (bx, by) in zip(path, path[1:]):
        n = max(1, int(((bx - ax) ** 2 + (by - ay) ** 2) ** 0.5 / 0.1))
        out += [(ax + (bx - ax) * i / n, ay + (by - ay) * i / n) for i in range(n)]
    return out + [path[-1]]


def overlaps(a, b):
    return a[0] < b[2] and b[0] < a[2] and a[1] < b[3] and b[1] < a[3]


def render(key, out_dir, m, town):
    l = json.load(open(os.path.join(rooms.APA, "Resources/Layouts/%s.json" % key), encoding="utf-8"))
    w, h = int(l["size"]["x"]), int(l["size"]["y"])
    e = l.get("entrance", {"x": 0.5, "y": 4.5})
    pack = next(d for d in pack_dirs()[1:] if os.path.isdir(os.path.join(d, rooms.BUILDER)))
    shell = rooms.build(dict(l["shell"], size=(w, h), entrance=(e["x"], e["y"])), pack)
    im = Image.new("RGBA", (int((w + PAD) * 32), h * 32), (40, 40, 50, 255))
    pave = art(m, "x-pave")
    for yy in range(0, im.height, pave.height):
        for xx in range(0, int(PAD * 32), pave.width):
            im.alpha_composite(pave.crop((0, 0, min(pave.width, int(PAD * 32) - xx), pave.height)), (xx, yy))
    im.alpha_composite(shell, (int(PAD * 32), 0))
    px = lambda x, y: (int(round((x + PAD) * 32)), int(round((h - 0.5 - y) * 32)))
    draws, problems = [], []  # (order, seq, image, x, y)
    rects = []
    for i, p in enumerate(l["props"]):
        s = art(m, p["key"])
        lift = p.get("lift", 0)
        draws.append((-95 if p.get("under") else -int(p["y"] * 10), i, s, p["x"], p["y"] + lift))
        if not p.get("under"):
            bb = s.getbbox() or (0, 0, s.width, s.height)
            x0 = p["x"] - s.width / 64
            rects.append((p["key"], (x0 + bb[0] / 32, p["y"] + lift + (s.height - bb[3]) / 32, x0 + bb[2] / 32, p["y"] + lift + (s.height - bb[1]) / 32)))
    people = next((g["people"] for g in town["inside"] if g["layout"] == key), [])
    bodies = []
    for j, wk in enumerate(people):
        sheet = art(m, wk["key"])
        path = [(q["x"], q["y"]) for q in wk["path"]]
        waits = wk.get("wait", [])
        k = next((i for i, t in enumerate(waits) if t > 0), 0)  # drawn where it first stands, facing up; a pose at its point
        f = 6 if len(path) > 1 and waits and waits[k] > 0 else 0
        draws.append((-int(path[k][1] * 10) + 1, 1000 + j, sheet.crop((f * 32, 0, f * 32 + 32, sheet.height)), *path[k]))
        bodies += [(wk["key"], (x - 0.45, y, x + 0.45, y + 1.4), (x - 0.45, y, x + 0.45, y + 1.1)) for x, y in samples(path)]
    a_at, b_at = P(l, "a", 4), P(l, "b", 7)  # A at the shelf, B at the counter
    face = 0 if l.get("facing", "right") == "right" else 6
    for who, at, f in (("customer-a", a_at, 6), ("customer-b", b_at, face)):
        sheet = art(m, who)
        draws.append((50, 2000, sheet.crop((f * 32, 64, f * 32 + 32, 128)), *at))
    for order, _, s, x, y in sorted(draws, key=lambda d: (d[0], d[1])):
        cx, cy = px(x, y)
        im.alpha_composite(s, (cx - s.width // 2, cy - s.height))
    big = im.resize((im.width * S, im.height * S), Image.NEAREST)
    d = ImageDraw.Draw(big)
    font = lambda size: ImageFont.truetype(FONT, size) if os.path.exists(FONT) else ImageFont.load_default()

    def plate(text, x, y, fill, ink, size=22, anchor="mm"):
        cx, cy = px(x, y)
        bb = d.textbbox((cx * S, cy * S), text, font=font(size), anchor=anchor)
        d.rectangle((bb[0] - 6, bb[1] - 4, bb[2] + 6, bb[3] + 4), fill=fill)
        d.text((cx * S, cy * S), text, font=font(size), fill=ink, anchor=anchor)

    for g in l.get("signs", []):
        plate(g["text"], g["x"], g["y"], (150, 97, 61, 255), (255, 245, 214, 255), 16)
    for n in l.get("notes", []):
        plate(n["text"], n["x"], n["y"], (20, 20, 31, 200), (255, 240, 199, 255))
    plate("客人 A", a_at[0], a_at[1] + 2.1, (217, 119, 6, 255), (255, 255, 255, 255), 18)
    plate("客人 B", b_at[0], b_at[1] - 0.35, (37, 99, 235, 255), (255, 255, 255, 255), 18)
    out = os.path.join(out_dir, "mock-%s.png" % key)
    big.convert("RGB").save(out)
    # The tests' rules.
    for who in ("a", "b"):
        for a, b in LINES:
            for x, y in samples([P(l, who, a), P(l, who, b)]):
                feet, body = (x - 14 / 32, y, x + 14 / 32, y + 8 / 32), (x - 0.45, y, x + 0.45, y + 1.4)
                problems += ["%s's feet on %s at %.2f,%.2f (%s-%s)" % (who.upper(), k, x, y, POINTS[a], POINTS[b]) for k, r in rects if overlaps(feet, r)]
                problems += ["%s over %s at %.2f,%.2f" % (k, who.upper(), x, y) for k, r, _ in bodies if overlaps(body, r)]
    for who in ("a", "b"):
        for n in (4, 7, 10):  # the shelf, the counter, the delivery zone
            x, y = P(l, who, n)
            # ShopView.ShowLabel: two lines from 0.4 beside the feet, rightwards on the room's left half (x < 6),
            # leftwards on its right; A's over its head (from 2.05 up), B's under its feet (from -0.1 down);
            # with its plate, as wide as the test's label there: 選好商品，送出請購 at the shelf (3.0 tiles), up to
            # ten characters at the counter and the delivery zone (3.4)
            wd = 3.0 if n == 4 else 3.4
            x0 = x - 0.5 if x < 6 else x + 0.5 - wd
            label = (x0, y + 1.95, x0 + wd, y + 3.0) if who == "a" else (x0, y - 1.15, x0 + wd, y - 0.05)
            problems += ["%s's face under %s's label at %s" % (k, who.upper(), POINTS[n]) for k, _, fc in bodies if overlaps(fc, label)]
    if not any("付款後才交付" in n["text"] for n in l.get("notes", [])):
        problems.append("no note says 付款後才交付")
    seen = []
    for p in problems:
        if p.split(" at ")[0] not in seen:
            seen.append(p.split(" at ")[0])
            print("  " + p)
    print(out, "ok" if not problems else "%d problem(s)" % len(seen))
    return not problems


def main():
    if len(sys.argv) < 3:
        raise SystemExit(__doc__)
    out_dir, keys = sys.argv[1], sys.argv[2:]
    os.makedirs(out_dir, exist_ok=True)
    m = manifest()
    town = json.load(open(os.path.join(rooms.APA, "Resources/Town/town.json"), encoding="utf-8"))
    if keys[0].startswith("town:"):
        render_town(out_dir, m, town, *map(float, keys[0][5:].split(",")))
        return
    ok = [render(k, out_dir, m, town) for k in keys]
    sys.exit(0 if all(ok) else 1)


if __name__ == "__main__":
    main()
