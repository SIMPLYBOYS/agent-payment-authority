#!/usr/bin/env python3
"""The bookshop's sign (Aaron 11/17: 為何書店外觀上的招牌會是這樣？). The bookshop wears the pack's office building
(there is no bookshop building in it), whose sign row reads LIME CORP with a lemon: LimeZu's own name. This redraws
that row's panel in the pack's own pixels and colours: BOOK in the yellow of LIME, STORE in the blue of CORP, an
open book where the lemon was. Assets/LimeZu/_cut/x-books-sign.png, the town's x-office-sign-rest with the new words
(the same rect of the same example, so it sits where that piece sat).

    LIMEZU_DIR=var/limezu-both python3 scripts/unity/books_sign.py

The letters keep the sign's grid: one art pixel is 2x2 image pixels; strokes two art pixels thick; 8 rows high, an
outline over each glyph, a shadow right of and under it; rows banded as the pack's letters are. Not in git (LimeZu's
licence), as the other cuts.
"""
import os

from PIL import Image

ROOT = os.path.normpath(os.path.join(os.path.dirname(__file__), "../.."))
OUT = os.path.join(ROOT, "unity/AgentPaymentAuthority/Assets/LimeZu/_cut/x-books-sign.png")
EXAMPLE = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/ME_Theme_Sorter_32x32/16_Office_Singles_32x32/ME_Singles_Office_32x32_Example_1.png"
SIGN = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/Modern_Exteriors_Complete_Singles_32x32/ME_Singles_Office_32x32_Building_Sign_1.png"
REST = (32, 262, 342, 112)  # x-office-sign-rest's rect in the example (limezu-assets.txt)
PANEL = (28, 8, 198, 32)    # the sign's panel in the single: 85 x 12 art pixels, a border round 83 x 10

C = {"0": (58, 58, 80), "1": (86, 89, 114), "2": (108, 110, 133), "3": (248, 248, 248), "4": (255, 245, 154),
     "5": (248, 210, 57), "6": (226, 242, 243), "7": (222, 112, 16), "a": (255, 229, 123), "b": (242, 178, 43),
     "f": (186, 210, 224)}

G = {
    "B": ["####.", "#####", "##.##", "####.", "#####", "##.##", "#####", "####."],
    "O": [".###.", "#####", "##.##", "##.##", "##.##", "##.##", "#####", ".###."],
    "K": ["##.##", "##.##", "####.", "###..", "###..", "####.", "##.##", "##.##"],
    "S": [".####", "#####", "##...", "####.", ".####", "...##", "#####", "####."],
    "T": ["######", "######", "..##..", "..##..", "..##..", "..##..", "..##..", "..##.."],
    "R": ["####.", "#####", "##.##", "#####", "####.", "##.##", "##.##", "##.##"],
    "E": ["#####", "#####", "##...", "####.", "####.", "##...", "#####", "#####"],
}
BOOK = [".000.000.", "033303330", "066606660", "033303330", "066606660", "000070000", ".7777777."]  # rows 4-10


def colour(word_blue, row, first, second):
    """A glyph pixel's colour by its row (0-7) and place in its run: the pack's banding (LIME yellow, CORP blue)."""
    if word_blue:
        return "3" if row < 2 and (first or second) else "6" if row < 5 else "f"
    if row == 0:
        return "3" if first else "4"
    if row == 1:
        return "4" if first else "a"
    return "5" if row < 5 else "b"


def main():
    pack = os.environ.get("LIMEZU_DIR")
    if not pack or not os.path.isfile(os.path.join(pack, EXAMPLE)):
        raise SystemExit("set LIMEZU_DIR to the folder that holds Modern_Exteriors_v42.3/")
    x, y, w, h = REST
    rest = Image.open(os.path.join(pack, EXAMPLE)).convert("RGBA").crop((x, y, x + w, y + h))
    panel = Image.open(os.path.join(pack, SIGN)).convert("RGBA").crop(PANEL)
    at = find(rest, panel)
    W, H = panel.width // 2, panel.height // 2
    grid = [["2"] * W for _ in range(H)]
    glyph = [[None] * W for _ in range(H)]  # the colour of a glyph or icon pixel, else None
    # the words: BOOK (yellow) and STORE (blue), then the book; centred in the panel's inside (columns 1..W-2)
    words = [("BOOK", False), ("STORE", True)]
    width = sum(sum(len(G[ch][0]) for ch in wd) + 2 * (len(wd) - 1) for wd, _ in words) + 4 + 3 + len(BOOK[0])
    cx = 1 + (W - 2 - width) // 2
    for wd, blue in words:
        for i, ch in enumerate(wd):
            rows = G[ch]
            for r, line in enumerate(rows):
                for c, v in enumerate(line):
                    if v == "#":
                        first = c == 0 or line[c - 1] != "#"
                        second = c >= 1 and line[c - 1] == "#" and (c == 1 or line[c - 2] != "#")
                        glyph[3 + r][cx + c] = colour(blue, r, first, second)
            cx += len(rows[0]) + 2
        cx += 2
    cx += 1
    for r, line in enumerate(BOOK):
        for c, v in enumerate(line):
            if v != ".":
                glyph[4 + r][cx + c] = v
    # outline over a letter, shadow right of and under it (the book draws its own outline)
    for r in range(1, H - 1):
        for c in range(1, W - 1):
            if glyph[r][c]:
                grid[r][c] = glyph[r][c]
            elif r + 1 < H - 1 and glyph[r + 1][c] and glyph[r + 1][c] not in "07" and r >= 2:
                grid[r][c] = "0"
            elif glyph[r][c - 1] or glyph[r - 1][c]:
                grid[r][c] = "1"
    # the panel's border stays the pack's; its inside is redrawn
    out = rest.copy()
    for r in range(1, H - 1):
        for c in range(1, W - 1):
            out.paste(C[grid[r][c]] + (255,), (at[0] + 2 * c, at[1] + 2 * r, at[0] + 2 * c + 2, at[1] + 2 * r + 2))
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    out.save(OUT)
    print(OUT, out.size, "panel at", at)


def find(img, part):
    """Where part sits in img, pixel for pixel."""
    a, b = img.tobytes(), part.tobytes()
    rw, pw = img.width * 4, part.width * 4
    first = b[:pw]
    for y in range(img.height - part.height + 1):
        row = a[y * rw:(y + 1) * rw]
        x = row.find(first)
        while x >= 0:
            if x % 4 == 0 and all(a[(y + k) * rw + x:(y + k) * rw + x + pw] == b[k * pw:(k + 1) * pw] for k in range(part.height)):
                return x // 4, y
            x = row.find(first, x + 1)
    raise SystemExit("the sign's panel is not in the example")


if __name__ == "__main__":
    main()
