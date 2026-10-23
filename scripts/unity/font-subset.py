#!/usr/bin/env python3
"""The Unity scene's Chinese font (docs/commercial-gap2.md GD3-B).

Noto Sans TC (SIL OFL 1.1) cut down to the characters the scene can show:
printable ASCII, common full-width punctuation, and every other character in
the scene's scripts and room layouts. Run it again whenever a label changes;
LayoutTests fails on a character the font does not have.

    python3 scripts/unity/font-subset.py --src NotoSansTC[wght].ttf

Needs fontTools (pip install fonttools). The full font is not kept in the
repository; the subset and the licence are, under Assets/APA/Resources.
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
        font = instancer.instantiateVariableFont(font, {"wght": 400})  # one static weight: Regular
    text = characters()
    sub = subset.Subsetter(subset.Options())
    sub.populate(text=text)
    sub.subset(font)
    font.save(OUT)
    print(f"{OUT}: {len(text)} characters, {os.path.getsize(OUT)} bytes")


if __name__ == "__main__":
    main()
