#!/usr/bin/env python3
"""The ground's shade beside a building (Aaron 11/17, the street list's item 13: LimeZu's examples lay a faint shade
by buildings; the pack has no shadow tiles, its own shadows are drawn into the art as flat, hard-edged, translucent
shapes falling right, as the coffee kiosk's). Two tiles in that manner, for town.json's ground: x-shade, a whole tile
of the shade, and x-shade-top, its lower left half (the shade's top edge sloping down to the right, the light from
the upper left). Assets/LimeZu/_cut/, not in git, as the other cuts.

    python3 scripts/unity/shade.py
"""
import os

from PIL import Image

ROOT = os.path.normpath(os.path.join(os.path.dirname(__file__), "../.."))
OUT = os.path.join(ROOT, "unity/AgentPaymentAuthority/Assets/LimeZu/_cut")
SHADE = (58, 58, 80, 100)  # the pack's own shadow colour (Kiosk_Coffee_Cup_Example's translucent pixels)


def main():
    os.makedirs(OUT, exist_ok=True)
    Image.new("RGBA", (32, 32), SHADE).save(os.path.join(OUT, "x-shade.png"))
    top = Image.new("RGBA", (32, 32), (0, 0, 0, 0))
    for y in range(32):
        for x in range(32):
            if x <= y:  # lower left of the diagonal from the top left corner to the bottom right
                top.putpixel((x, y), SHADE)
    top.save(os.path.join(OUT, "x-shade-top.png"))
    print(OUT)


if __name__ == "__main__":
    main()
