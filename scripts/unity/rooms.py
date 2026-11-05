#!/usr/bin/env python3
"""A room's shell as one image (docs/commercial-interiors-v2.md): floor, walls, borders, the left wall's
entrance and the floor's shadows, in LimeZu's 3D-wall style, for every layout that has a "shell".

The structure is LimeZu's own ice cream shop (6_Home_Designs, 32x32): each of its 16 px cells is a tile of
the teal 3D wall set (Room_Builder_3d_walls, origin 256,1120). The shell keeps that structure for any size,
recolours the set (spec "colours": teal "r,g,b" -> [r,g,b]), puts a wallpaper strip of Room_Builder_Walls
on the back wall (stretched by its flat middle rows), lays a 32 px floor tile of Room_Builder_Floors, opens
the left wall where the customers come in from the pavement (the layout's "entrance", y from x to y, as ShopView
reads it), and shades the floor along the walls.

    LIMEZU_DIR=/path/to/limezu python3 scripts/unity/rooms.py

It reads LIMEZU_DIR/Modern_Interiors_v41.4 and writes Assets/LimeZu/_cut/room-<layout>.png (ignored, as
the pack is: LimeZu's licence allows use and edits, not distribution). The layout names it as its "base"
and limezu-assets.txt lists it under _cut/ (import-limezu.sh leaves those alone). Run it again whenever a
layout's shell changes. The image covers x 0..w, y -0.5..h-0.5 of the room (size [w, h] tiles): a top
border, 2.5 tiles of back wall, the floor from y 0.5 to h-3.5, a bottom border.
"""
import glob
import json
import os
import sys

from PIL import Image, ImageDraw

ROOT = os.path.normpath(os.path.join(os.path.dirname(__file__), "../.."))
APA = os.path.join(ROOT, "unity/AgentPaymentAuthority/Assets/APA")
OUT = os.path.join(ROOT, "unity/AgentPaymentAuthority/Assets/LimeZu/_cut")
BUILDER = "Modern_Interiors_v41.4/1_Interiors/32x32/Room_Bulder_subfiles_32x32/"
TEAL = (256, 1120)
OUTLINE = (58, 58, 80, 255)


def recolour(im, cmap):
    if not cmap:
        return im
    im = im.copy()
    px = im.load()
    for y in range(im.height):
        for x in range(im.width):
            c = px[x, y]
            if c[3] and c[:3] in cmap:
                px[x, y] = tuple(cmap[c[:3]]) + (c[3],)
    return im


def stretch(strip, height, keep_top, keep_bottom):
    """The strip with its middle rows stretched to height (nearest row); its top and bottom rows kept."""
    mid, m = strip.height - keep_top - keep_bottom, height - keep_top - keep_bottom
    out = Image.new("RGBA", (strip.width, height))
    out.paste(strip.crop((0, 0, strip.width, keep_top)), (0, 0))
    for r in range(m):
        src = keep_top + min(mid - 1, r * mid // m)
        out.paste(strip.crop((0, src, strip.width, src + 1)), (0, keep_top + r))
    out.paste(strip.crop((0, strip.height - keep_bottom, strip.width, strip.height)), (0, height - keep_bottom))
    return out


def build(spec, pack):
    w, h = spec["size"]
    W, H = 2 * w, 2 * h  # 16 px cells
    walls3d = Image.open(os.path.join(pack, BUILDER, "Room_Builder_3d_walls_32x32.png")).convert("RGBA")
    walls = Image.open(os.path.join(pack, BUILDER, "Room_Builder_Walls_32x32.png")).convert("RGBA")
    floors = Image.open(os.path.join(pack, BUILDER, "Room_Builder_Floors_32x32.png")).convert("RGBA")
    cmap = {tuple(map(int, k.split(","))): v for k, v in spec.get("colours", {}).items()}
    t = lambda dx, dy: recolour(walls3d.crop((TEAL[0] + dx, TEAL[1] + dy, TEAL[0] + dx + 16, TEAL[1] + dy + 16)), cmap)
    im = Image.new("RGBA", (W * 16, H * 16), (0, 0, 0, 0))
    put = lambda c, r, tile: im.alpha_composite(tile, (c * 16, r * 16))
    R, B = W - 1, H - 2  # the right border's column, the bottom border's row
    fx, fy = spec["floor"]
    fl = floors.crop((fx, fy, fx + 32, fy + 32))
    for r in range(6, B, 2):
        for c in range(0, W, 2):
            im.alpha_composite(fl, (c * 16, r * 16))
    # floor zones (a kitchen's tiles, a seating area's): "zones" [{"floor": [x, y], "rect": [x0, y0, x1, y1]}] in
    # room tiles, y up; the zone's tile is laid on the room's own 32 px grid, so its pattern lines up across zones
    for z in spec.get("zones", []):
        zx, zy = z["floor"]
        zt = floors.crop((zx, zy, zx + 32, zy + 32))
        x0, y0, x1, y1 = z["rect"]
        box = (int(x0 * 32), int((h - 0.5 - y1) * 32), int(x1 * 32), int((h - 0.5 - y0) * 32))
        tiled = Image.new("RGBA", im.size)
        for yy in range(96, B * 16, 32):
            for xx in range(0, W * 16, 32):
                tiled.alpha_composite(zt, (xx, yy))
        im.paste(tiled.crop(box), box[:2])
    # the top border; the back wall: the set's face and baseboard, under the wallpaper when there is one
    put(0, 0, t(32, 64)); put(R, 0, t(208, 64))
    for c in range(1, R): put(c, 0, t(16, 0))
    for r in range(1, 5):
        for c in range(2, R - 1): put(c, r, t(16, 16))
    for c in range(2, R - 1): put(c, 5, t(240, 112))
    if "wall" in spec:
        wx, wy = spec["wall"]
        tall = stretch(walls.crop((wx + 32, wy, wx + 64, wy + 64)), 96, *spec.get("wall_keep", (14, 8)))
        for c in range(2, R - 1, 2):
            im.alpha_composite(tall.crop((0, 0, 32 if c + 2 <= R - 1 else 16, 96)), (c * 16, 0))
    # the side walls in perspective, then their floor-height strips; the outer borders; the bottom border
    for r, dy in zip(range(1, 6), (16, 96, 112, 128, 144)):
        put(1, r, t(48, dy)); put(R - 1, r, t(192, dy if dy != 144 else 128))
    for r in range(6, B):
        put(1, r, t(240, 128)); put(R - 1, r, t(224, 0))
    for r in range(1, B):
        put(0, r, t(32, 16)); put(R, r, t(144, 16))
    put(0, B, t(64, 192)); put(R, B, t(176, 192))
    for c in range(1, R): put(c, B, t(80, 192))
    d = ImageDraw.Draw(im)
    # the entrance (y from..to in tiles): the left wall open, the floor through it, a cap and wood jambs at its
    # ends, a glass pane in the wall above it
    y0, y1 = spec.get("entrance", (0.5, 4.5))
    e0, e1 = int(round((h - 0.5 - y1) * 2)), int(round((h - 0.5 - y0) * 2)) - 1  # its cell rows, top to bottom
    for r in range(e0, e1 + 1):
        im.paste((0, 0, 0, 0), (0, r * 16, 32, r * 16 + 16))
        im.alpha_composite(fl.crop((0, (r % 2) * 16, 32, (r % 2) * 16 + 16)), (0, r * 16))
    if e1 >= B - 1:  # down to the bottom border: it starts after the opening
        im.alpha_composite(fl.crop((0, 0, 32, 16)), (0, B * 16))
        d.rectangle([32, B * 16, 33, B * 16 + 11], fill=OUTLINE)
    d.rectangle([0, e0 * 16 - 3, 31, e0 * 16 - 1], fill=OUTLINE)
    for yy in (e0 * 16, (e1 + 1) * 16 - 6):
        d.rectangle([2, yy, 13, yy + 5], fill=(150, 98, 60, 255), outline=OUTLINE)
    g0 = max(6, e0 - 6)
    d.rectangle([4, g0 * 16 + 4, 11, e0 * 16 - 8], fill=(170, 214, 230, 255))
    d.line([7, g0 * 16 + 6, 7, e0 * 16 - 10], fill=(235, 248, 252, 255))
    # shadows: along the back wall and down the side walls (the pack's floor shadow is black at about 25/255)
    a = spec.get("shadow", 28)
    sh = Image.new("RGBA", im.size, (0, 0, 0, 0))
    sd = ImageDraw.Draw(sh)
    sd.rectangle([32, 96, (R - 1) * 16 - 1, 96 + 15], fill=(0, 0, 0, a))
    sd.rectangle([32, 96 + 16, 32 + 15, e0 * 16 - 1], fill=(0, 0, 0, a))
    sd.rectangle([(R - 1) * 16 - 16, 96 + 16, (R - 1) * 16 - 1, B * 16 - 1], fill=(0, 0, 0, a // 2))
    im.alpha_composite(sh)
    return im


def main():
    pack = os.environ.get("LIMEZU_DIR")
    if not pack or not os.path.isdir(os.path.join(pack, BUILDER)):
        raise SystemExit("set LIMEZU_DIR to the folder that holds Modern_Interiors_v41.4/")
    os.makedirs(OUT, exist_ok=True)
    n = 0
    for path in sorted(glob.glob(os.path.join(APA, "Resources/Layouts/*.json"))):
        layout = json.load(open(path, encoding="utf-8"))
        if "shell" not in layout:
            continue
        z = layout["size"]  # as Unity reads it: {"x": w, "y": h}
        e = layout.get("entrance", {"x": 0.5, "y": 4.5})  # the left wall's opening (ShopView.Layout.entrance)
        spec = dict(layout["shell"], size=(int(z["x"]), int(z["y"])), entrance=(e["x"], e["y"]))
        out = os.path.join(OUT, "room-%s.png" % layout["key"])
        build(spec, pack).save(out)
        print(out, layout["size"])
        n += 1
    print("rooms: %d shell(s) written" % n)


if __name__ == "__main__":
    main()
