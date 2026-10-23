#!/usr/bin/env python3
"""The Unity scene's Chinese font (docs/commercial-gap2.md GD3-B).

Noto Sans TC (SIL OFL 1.1) cut down to the characters the scene can show:
printable ASCII, common full-width punctuation, and every other character in
the scene's scripts and room layouts. Run it again whenever a label changes;
LayoutTests fails on a character the font does not have.

    python3 scripts/unity/font-subset.py --src NotoSansTC[wght].ttf

Needs fontTools (pip install fonttools). The full font is not kept in the
repository; the subset and the licence are, under Assets/APA/Resources.
The source used (Aaron approved the download, 10/23):
    https://raw.githubusercontent.com/google/fonts/main/ofl/notosanstc/NotoSansTC%5Bwght%5D.ttf
    Version 2.004, sha256 864727d210d54f2537bbe23b3a839436c3992af72de9322af5270897246bd44f
    and OFL.txt beside it (copied to Resources/NotoSansTC-OFL.txt).
"""
import argparse
import glob
import os

from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib import instancer

ROOT = os.path.normpath(os.path.join(os.path.dirname(__file__), "../.."))
APA = os.path.join(ROOT, "unity/AgentPaymentAuthority/Assets/APA")
OUT = os.path.join(APA, "Resources/NotoSansTC-APA.ttf")
# CHARS: the characters the saved font maps, one string; the Unity tests read
# it (Font.HasCharacter says yes to any character of a dynamic font).
CHARS = os.path.join(APA, "Resources/NotoSansTC-APA-chars.txt")
PUNCT = "，。、：；？！「」『』（）・…—～×　"


def characters():
    chars = {chr(c) for c in range(0x20, 0x7F)} | set(PUNCT)
    for path in glob.glob(os.path.join(APA, "Scripts/*.cs")) + glob.glob(os.path.join(APA, "Resources/Layouts/*.json")):
        with open(path, encoding="utf-8") as f:
            chars |= {c for c in f.read() if ord(c) > 0x7F}
    return "".join(sorted(chars))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--src", required=True, help="the full Noto Sans TC font (variable or static)")
    a = ap.parse_args()
    font = TTFont(a.src)
    if "fvar" in font:
        font = instancer.instantiateVariableFont(font, {"wght": 400}, updateFontNames=True)  # one static weight: Regular
    text = characters()
    opts = subset.Options()
    opts.name_IDs = ["*"]  # keep the licence description and URL (13, 14) in the file
    sub = subset.Subsetter(opts)
    sub.populate(text=text)
    sub.subset(font)
    cmap = font.getBestCmap()
    missing = [c for c in text if ord(c) not in cmap]
    if missing:
        raise SystemExit("not in the source font: " + "".join(missing))
    font.save(OUT)
    have = "".join(sorted(chr(c) for c in TTFont(OUT).getBestCmap() if c >= 0x20))
    with open(CHARS, "w", encoding="utf-8") as f:
        f.write(have + "\n")
    print(f"{OUT}: {len(text)} characters, {os.path.getsize(OUT)} bytes; {CHARS}: {len(have)}")


if __name__ == "__main__":
    main()
