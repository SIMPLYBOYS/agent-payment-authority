#!/usr/bin/env python3
"""The food trucks with a clerk who moves in each window (Aaron 11/9: 餐車內應該都要有會動的店員).

Each truck comes open (its _1) and shuttered (_2): the pixels that differ are its window. A frame is the open truck
with a premade character's idle frame drawn only inside the window (its head a pixel under the window's top),
then the window's last rows (the counter, the bottles on it) drawn again over the clerk, who so stands behind
them. (The trucks' _Props_1 are their windows' insides, not a counter.) The frames: facing the customers, turning
back to cook, facing them, looking along the counter each way; at 0.15 s a frame (town.json "times").
Assets/LimeZu/_cut/x-food-<truck>.png, one truck a frame, side by side.

    LIMEZU_DIR=var/limezu-both python3 scripts/unity/trucks.py

LIMEZU_DIR holds Modern_Exteriors_v42.3/ and Modern_Interiors_v41.4/. Not in git (LimeZu's licence), as the kiosk.
"""
import os
import sys

from PIL import Image, ImageChops

sys.path.insert(0, os.path.dirname(__file__))
import rooms  # noqa: E402

V = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/ME_Theme_Sorter_32x32/10_Vehicles_Singles_32x32/ME_Singles_Vehicles_32x32_"
CHAR = "Modern_Interiors_v41.4/2_Characters/Character_Generator/0_Premade_Characters/32x32/Premade_Character_32x32_%02d.png"
# key: (the truck's file, its premade clerk, pixels right of the window's middle (the pizza's window takes in its
# menu), pixels up (a premade's eyes are some 21 px under its hair's top: in the tacos' and the ice cream's low
# windows the face over the counter, the hair's top behind the awning))
TRUCKS = {"x-food-tacos": ("Tacos_Truck", 19, 0, 12), "x-food-icecream": ("Ice_Cream_Truck", 12, 0, 6), "x-food-pizza": ("Pizza_Tuk_Tuk", 7, 10, 0),
          "x-food-hotdog": ("Hot_Dog_Cart", 14, 0, 0)}
# A premade sheet's idle row (y 64): right 0-5, up 6-11, left 12-17, down 18-23.
DOWN, UP, LEFT, RIGHT = range(18, 24), range(6, 12), range(12, 18), range(0, 6)
SEQUENCE = [*DOWN, *UP, *DOWN, *LEFT, *DOWN, *RIGHT]
WIDEST = 8192  # Unity takes no wider texture here (ShopBuilder's cut failed at 48 frames of 224 px)
FRONT = 3  # the window's last rows, over the clerk


def filled(mask):
    """The window row by row from its first pixel to its last: where the open and the shut truck happen to match
    (a shelf the same grey as the shutter) is window too."""
    out = Image.new("L", mask.size)
    for y in range(mask.height):
        xs = [x for x in range(mask.width) if mask.getpixel((x, y))]
        if xs:
            out.paste(255, (xs[0], y, xs[-1] + 1, y + 1))
    return out


def main():
    pack = os.environ.get("LIMEZU_DIR")
    if not pack or not os.path.isfile(os.path.join(pack, V + "Tacos_Truck_1.png")):
        raise SystemExit("set LIMEZU_DIR to the folder that holds Modern_Exteriors_v42.3/ and Modern_Interiors_v41.4/")
    os.makedirs(rooms.OUT, exist_ok=True)
    for key, (name, who, dx, up) in TRUCKS.items():
        open_ = Image.open(os.path.join(pack, V + name + "_1.png")).convert("RGBA")
        shut = Image.open(os.path.join(pack, V + name + "_2.png")).convert("RGBA")
        assert open_.size == shut.size, name
        window = filled(ImageChops.difference(open_, shut).convert("L").point(lambda v: 255 if v else 0))
        x0, y0, x1, y1 = window.getbbox()
        front = Image.new("L", open_.size)
        front.paste(window.crop((0, y1 - FRONT, open_.width, y1)), (0, y1 - FRONT))
        sheet = Image.open(os.path.join(pack, CHAR % who)).convert("RGBA")
        head = sheet.crop((DOWN[0] * 32, 64, DOWN[0] * 32 + 32, 128)).getbbox()[1]  # its first row with anything
        left, top = (x0 + x1) // 2 - 16 + dx, y0 + 1 - head - up
        assert open_.width * len(SEQUENCE) <= WIDEST, name + ": too many frames for one texture"
        strip = Image.new("RGBA", (open_.width * len(SEQUENCE), open_.height))
        for i, k in enumerate(SEQUENCE):
            body = Image.new("RGBA", open_.size)
            body.alpha_composite(sheet.crop((k * 32, 64, k * 32 + 32, 128)), (left, top))
            frame = open_.copy()
            inside = Image.new("RGBA", open_.size)
            inside.paste(body, (0, 0), window)  # only what the window shows
            frame.alpha_composite(inside)
            counter = Image.new("RGBA", open_.size)
            counter.paste(open_, (0, 0), front)  # the counter again, over the clerk
            frame.alpha_composite(counter)
            strip.alpha_composite(frame, (i * open_.width, 0))
        out = os.path.join(rooms.OUT, key + ".png")
        strip.save(out)
        print("trucks: %s %s window %s, %d frames" % (out, open_.size, (x0, y0, x1, y1), len(SEQUENCE)))


if __name__ == "__main__":
    main()
