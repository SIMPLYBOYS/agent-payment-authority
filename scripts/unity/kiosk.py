#!/usr/bin/env python3
"""The coffee kiosk with a clerk who moves (Aaron 11/7: 店員似乎不會動). The pack's kiosk example has its clerk drawn
in; its pieces let one be put in: the kiosk, a character in its window, the window's counter in front. This draws a
premade character's idle frames (facing right, as the example's clerk) there, one kiosk a frame, side by side:
Assets/LimeZu/_cut/x-kiosk-clerk.png, a town prop of 6 frames (TownView plays a prop's frames in a loop).

    LIMEZU_DIR=var/limezu-both python3 scripts/unity/kiosk.py

LIMEZU_DIR holds Modern_Exteriors_v42.3/ and Modern_Interiors_v41.4/. Not in git (LimeZu's licence), as the rooms.
"""
import os

from PIL import Image

ROOT = os.path.normpath(os.path.join(os.path.dirname(__file__), "../.."))
OUT = os.path.join(ROOT, "unity/AgentPaymentAuthority/Assets/LimeZu/_cut/x-kiosk-clerk.png")
KIOSK = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/ME_Theme_Sorter_32x32/3_City_Props_Singles_32x32/ME_Singles_City_Props_32x32_Kiosk_Coffee_Cup"
CLERK = "Modern_Interiors_v41.4/2_Characters/Character_Generator/0_Premade_Characters/32x32/Premade_Character_32x32_20.png"
# Where the example has them: its clerk's frame at (80, 202) (its feet behind the counter), the counter piece at (64, 224).
AT, FRONT_AT = (80, 202), (64, 224)
IDLE_RIGHT = range(0, 6)  # a premade sheet's idle row (y 64): right 0-5, up 6-11, left 12-17, down 18-23


def main():
    pack = os.environ.get("LIMEZU_DIR")
    if not pack or not os.path.isfile(os.path.join(pack, KIOSK + ".png")):
        raise SystemExit("set LIMEZU_DIR to the folder that holds Modern_Exteriors_v42.3/ and Modern_Interiors_v41.4/")
    kiosk = Image.open(os.path.join(pack, KIOSK + ".png")).convert("RGBA")
    front = Image.open(os.path.join(pack, KIOSK + "_Hollow_2.png")).convert("RGBA")
    sheet = Image.open(os.path.join(pack, CLERK)).convert("RGBA")
    strip = Image.new("RGBA", (kiosk.width * len(IDLE_RIGHT), kiosk.height))
    for i, k in enumerate(IDLE_RIGHT):
        frame = kiosk.copy()
        frame.alpha_composite(sheet.crop((k * 32, 64, k * 32 + 32, 128)), AT)
        frame.alpha_composite(front, FRONT_AT)
        strip.alpha_composite(frame, (i * kiosk.width, 0))
    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    strip.save(OUT)
    print(OUT, strip.size, len(IDLE_RIGHT), "frames")


if __name__ == "__main__":
    main()
