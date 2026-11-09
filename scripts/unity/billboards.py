#!/usr/bin/env python3
"""The street's two billboards, turning (Aaron 11/9: "可以參考 limezu 的範例，這個大型看板是可以動態變換內容的").

LimeZu's animated billboard (Animated_sheets_32x32/Billboard_32x32_1.png, 16 frames of 160 x 128) turns from
one ad to another through louvres: the ad, three frames of louvres closing over it, a blank, three opening on the
next. How much each of its pixels is louvre in each frame (between the ad under it and the louvres shut) is
measured from it and laid over the singles' ads (Billboard_1..8, 160 x 192 with the post), so each billboard turns through three of them: 8 frames
an ad, Assets/LimeZu/_cut/x-billboard-a.png and x-billboard-b.png. The town plays them with "times" (an ad
holds, the louvres flick).

    LIMEZU_DIR=var/limezu-both python3 scripts/unity/billboards.py

Not in git (LimeZu's licence), as the rooms and the stations.
"""
import os
import sys

from PIL import Image

sys.path.insert(0, os.path.dirname(__file__))
import rooms  # noqa: E402

ANIM = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/Animated_32x32/Animated_sheets_32x32/Billboard_32x32_1.png"
SINGLE = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/ME_Theme_Sorter_32x32/3_City_Props_Singles_32x32/ME_Singles_City_Props_32x32_Billboard_%d.png"
# the singles' ads each turns through: 2-4 a burger, 5-7 a car (red, green, yellow), 8 a sale
BOARDS = {"x-billboard-a": (5, 3, 8), "x-billboard-b": (8, 6, 2)}
W, H, BOARD = 160, 192, 128  # a frame; the board's rows (the rest is the post)


def frame(anim, k):
    return anim.crop((k * W, 0, k * W + W, BOARD)).load()


def cover(anim, k, ad, shut):
    """How far each pixel of LimeZu's frame k is from its ad (frame `ad`) toward its louvres shut (frame `shut`), 0..1."""
    f, a, l = frame(anim, k), frame(anim, ad), frame(anim, shut)
    out = {}
    for y in range(BOARD):
        for x in range(W):
            d = [l[x, y][i] - a[x, y][i] for i in range(3)]
            n = sum(v * v for v in d)
            out[x, y] = 0.0 if n == 0 else min(1.0, max(0.0, sum((f[x, y][i] - a[x, y][i]) * d[i] for i in range(3)) / n))
    return out


def louvred(ad, anim, shut, c):
    """The ad (a single, with its post) with louvres over its board, as much as c says."""
    out, l = ad.copy(), frame(anim, shut)
    o = out.load()
    for (x, y), t in c.items():
        if t > 0:
            o[x, y] = tuple(round(o[x, y][i] * (1 - t) + l[x, y][i] * t) for i in range(3)) + (o[x, y][3],)
    return out


def main():
    pack = os.environ.get("LIMEZU_DIR")
    if not pack or not os.path.isfile(os.path.join(pack, ANIM)):
        raise SystemExit("set LIMEZU_DIR to the folder that holds Modern_Exteriors_v42.3/")
    anim = Image.open(os.path.join(pack, ANIM)).convert("RGBA")
    closing = [cover(anim, k, 0, 3) for k in (1, 2)]  # over the first ad (frame 0), shut at frame 3
    opening = [cover(anim, k, 8, 5) for k in (6, 7)]  # shut at frame 5, over the second (frame 8)
    blank = anim.crop((4 * W, 0, 5 * W, BOARD))
    shut3, shut5 = anim.crop((3 * W, 0, 4 * W, BOARD)), anim.crop((5 * W, 0, 6 * W, BOARD))
    for key, ads in BOARDS.items():
        ims = [Image.open(os.path.join(pack, SINGLE % n)).convert("RGBA") for n in ads]
        frames = []
        for i, ad in enumerate(ims):
            nxt = ims[(i + 1) % len(ims)]
            frames.append(ad)
            frames += [louvred(ad, anim, 3, c) for c in closing]
            for board in (shut3, blank, shut5):
                f = ad.copy(); f.paste(board, (0, 0)); frames.append(f)
            frames += [louvred(nxt, anim, 5, c) for c in opening]
        sheet = Image.new("RGBA", (W * len(frames), H))
        for i, f in enumerate(frames):
            sheet.paste(f, (i * W, 0))
        out = os.path.join(rooms.OUT, key + ".png")
        sheet.save(out)
        print(out, len(frames), "frames")


if __name__ == "__main__":
    main()
