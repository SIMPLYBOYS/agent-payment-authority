#!/usr/bin/env python3
"""A shorter solar panel for the homes' roofs (Aaron 11/9: "太陽能板並不匹配這個房型，明顯長了一截").

LimeZu's Solar_Panel (64 x 96) is three rows of cells, as deep as a home's whole roof, so it stood over the
parapet. This keeps its frame and two of its rows: the middle row and the divider under it (rows 40-61) are taken
out, 74 px high. Assets/LimeZu/_cut/x-solar.png.

    LIMEZU_DIR=var/limezu-both python3 scripts/unity/solar.py

Not in git (LimeZu's licence), as the rooms and the billboards.
"""
import os
import sys

from PIL import Image

sys.path.insert(0, os.path.dirname(__file__))
import rooms  # noqa: E402

SRC = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/ME_Theme_Sorter_32x32/3_City_Props_Singles_32x32/ME_Singles_City_Props_32x32_Solar_Panel.png"
CUT = (40, 62)  # the middle row of cells and the divider under it


def main():
    pack = os.environ.get("LIMEZU_DIR")
    if not pack:
        raise SystemExit("set LIMEZU_DIR to the folder that holds Modern_Exteriors_v42.3/")
    im = Image.open(os.path.join(pack, SRC)).convert("RGBA")
    assert im.size == (64, 96), im.size
    out = Image.new("RGBA", (im.width, im.height - (CUT[1] - CUT[0])))
    out.paste(im.crop((0, 0, im.width, CUT[0])), (0, 0))
    out.paste(im.crop((0, CUT[1], im.width, im.height)), (0, CUT[0]))
    os.makedirs(rooms.OUT, exist_ok=True)
    path = os.path.join(rooms.OUT, "x-solar.png")
    out.save(path)
    print("solar: " + path)


if __name__ == "__main__":
    main()
