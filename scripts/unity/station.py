#!/usr/bin/env python3
"""The subway's two stations (Aaron 11/8: down the stairs one sees a station as LimeZu's example; the train goes to
the beach's station, and one walks out of it onto the beach), each as one image: Assets/LimeZu/_cut/x-station.png
under the coffee and tea kiosk, x-station-beach.png under the beach; and a dark tile round them (x-dark.png).

The shell is rooms.py's (LimeZu's 3D walls, recoloured to the example's dark grey, a concrete wallpaper on the back
wall), W x H tiles, covering room y -0.5..H-0.5. From the top, as the example (Aaron 11/9: "整個車站還必須要在精緻一點"):
the back wall (graffiti, posters, lights, departure screens, a door, an SOS box); a narrow platform along it; its
yellow edge; the track (the train stands on it, running out through both side walls: the tunnels); the platform
one boards from, its edge and yellow line; the concourse under a wall of its own, the lifts and the stairs up in it
(Aaron 11/15: as the example's lower hall). Floors:
lilac tiles with a few of another shade, and what a station's floor has (stains, cracks, papers, a can). What one
walks round (the train, the gates, the machines, the seats, the people) are the town's props, so that they sort with
the walked customer (town.json, the stations' parts).

    LIMEZU_DIR=var/limezu-both python3 scripts/unity/station.py

LIMEZU_DIR holds Modern_Exteriors_v42.3/ and Modern_Interiors_v41.4/. Not in git (LimeZu's licence), as the rooms.
"""
import os
import random
import sys

from PIL import Image

sys.path.insert(0, os.path.dirname(__file__))
import rooms  # noqa: E402

W, H = 50, 25            # tiles (Aaron 11/15: wider, 38 before); the town places it with its bottom left at (802, 8.5): town y = room y + 9
NORTH = (18.4, 21.5)     # room y: the platform along the back wall
TRACK = (13.9, 17.9)     # the track bed; the side walls open there
EDGE = 12.9              # the boarding platform's edge, one tile
SUB = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/ME_Theme_Sorter_32x32/20_Subway_and_Train_Station_Singles_32x32/ME_Singles_Subway_and_Train_Station_32x32_"
# On the back wall, per station: (piece, room x of its left, tiles down from the wall's top).
WALLS = {
    "x-station": [("Meh_Graffiti", 2.5, 0.7), ("Control_Big_Monitor", 9.3, 0.9), ("Three_Posters", 12.8, 1.3), ("Frog_Clean_Graffiti", 17.5, 0.2),
                  ("Green_Door", 22, 1.0), ("Poster_1_And_2", 25.7, 0.8), ("Control_Big_Monitor", 31.3, 0.9), ("Hew_Graffiti", 35.2, 0.6),
                  ("SOS_Box", 41.4, 1.0), ("Writing_Graffiti_1", 43.6, 0.1), ("Blue_Writing_Graffiti", 5.5, 1.6), ("Arrow_Graffiti_3", 29.2, 1.9)],
    "x-station-beach": [("Pink_Cloud_Graffiti_1", 2, 0.8), ("Control_Big_Monitor", 8, 0.9), ("Flower_Graffiti", 11.5, 0.4), ("Smile_Graffiti", 15.8, 1.2),
                        ("Black_Double_Door", 20, 1.0), ("Three_Posters", 24.2, 1.3), ("Control_Big_Monitor", 30, 0.9), ("Poster_Truth_2", 33.8, 0.7),
                        ("Green_FNS_Writing_Graffiti", 38.2, 0.8), ("SOS_Box", 44.5, 1.0), ("Pink_Tentacle_Graffiti_1", 4.7, 1.7), ("Worm_Graffiti", 27.3, 1.9)],
}
LIGHTS = [4, 14.7, 25.3, 36, 45.3]  # room x of the wall lights, along the wall's top
# Aaron 11/15 ("地下鐵的牆面與內部的操作機台位置請參考 limezu 範例"; then "把車站加寬"): as the example's lower hall,
# the concourse under a wall of its own, its top at room y INNER (the boarding platform north of it), 3 tiles high. In
# steps, as the example's: (room x from, to, tiles forward of INNER): by the stairs; the timetable screens' part
# forward; the vending machines' alcove back; a short jamb forward; then the way up to the platform (room x 45.5-49).
# In its face: a lift, the stairs up to the street, a lift; their buttons; the route map over the ticket machines; the
# screens (the machines are props).
INNER = 9.5
STEPS = [(1, 27, 0), (27, 34, 1), (34, 44, 0), (44, 45.5, 1)]
INNER_ON = [("Elevator_Closed_Lights_Off", 6.6, 0), ("Elevator_Green_Button", 8.62, 0.9), ("Stairs_1", 9.0, 0),
            ("Elevator_Green_Button", 11.02, 0.9), ("Elevator_Closed_Lights_Off", 11.4, 0), ("Map_Monitor", 15.8, 1.0),
            ("Control_Small_Monitor", 28.3, 0.9), ("Control_Small_Monitor", 30.9, 0.9)]
INNER_LIGHTS = [3, 14, 24, 38]
TOP, LINE = (96, 96, 112, 255), (58, 58, 80, 255)  # the walls' tops and outlines (the example's dark grey)
FLOOR_BITS = ["Ground_Tile_Spot_%d" % i for i in range(1, 11)] + ["Crack_%d" % i for i in range(1, 7)] + ["Ground_Paper_1", "Ground_Paper_2", "Pressed_Can_1", "Pressed_Can_2"]


def main():
    pack = os.environ.get("LIMEZU_DIR")
    if not pack or not os.path.isdir(os.path.join(pack, rooms.BUILDER)):
        raise SystemExit("set LIMEZU_DIR to the folder that holds Modern_Exteriors_v42.3/ and Modern_Interiors_v41.4/")
    load = lambda name: Image.open(os.path.join(pack, SUB + name + ".png")).convert("RGBA")
    spec = {"size": (W, H), "floor": [256, 1024], "wall": [0, 960], "wall_keep": (14, 8), "shadow": 34,
            # the example's walls: dark grey faces, a dark grey top (not the set's white), grey for its wooden jambs
            "colours": {"75,153,156": [124, 124, 138], "76,137,157": [104, 104, 118], "81,128,153": [70, 70, 84],
                        "248,248,248": [96, 96, 112], "201,134,85": [118, 118, 132]},
            "entrance": (TRACK[0], 17.4)}  # rooms.py draws a pane and a shadow over the opening: room for them; the bed covers both
    for seed, (key, wall) in enumerate(WALLS.items()):
        out = os.path.join(rooms.OUT, key + ".png")
        station(spec, pack, load, wall, random.Random(seed + 7)).save(out)
        print(out)
    Image.new("RGBA", (32, 32), (46, 46, 58, 255)).save(os.path.join(rooms.OUT, "x-dark.png"))


def station(spec, pack, load, wall, rnd):
    im = rooms.build(spec, pack)
    row = lambda y: int(round((H - 0.5 - y) * 32))  # room y to the image's pixel row
    # the wallpaper's white top trim in the example's dark grey, as the walls' borders (Aaron 11/15: the example's walls)
    top = im.crop((0, 0, im.width, 12))
    im.paste(rooms.recolour(top, {(248, 248, 248): [96, 96, 112]}), (0, 0))
    # the inner wall is the back wall's own slice (its top, concrete face, baseboard)
    inner = im.crop((32, 0, (W - 1) * 32, 96))
    # rooms.py's glass pane over its opening (here the tunnel's, which wants none): the wall's own colour over it
    e0 = int(round((H - 0.5 - spec["entrance"][1]) * 2))
    im.paste(im.getpixel((6, row(10))), (2, max(6, e0 - 6) * 16 + 2, 14, e0 * 16 - 6))
    x0, x1 = 32, (W - 1) * 32 - 16                  # inside the side walls
    # floors: lilac tiles, now and then another shade
    tiles = [load("Lilac_Tile_1_Vers_1")] * 10 + [load("Lilac_Tile_3_Vers_1"), load("Lilac_Tile_5_Vers_1")]
    for top, bottom in ((row(NORTH[1]), row(NORTH[0])), (row(EDGE), row(0.5))):
        for py in range(top, bottom, 32):
            for px in range(x0, x1, 32):
                im.alpha_composite(tiles[rnd.randrange(len(tiles))], (px, py))
    # the track along the back wall, right across (through both side walls), its rails
    bed = Image.new("RGBA", (W * 32, row(TRACK[0]) - row(TRACK[1])), (54, 54, 66, 255))
    im.paste(bed, (0, row(TRACK[1])))
    rail = load("Grey_Rail_Modular_Horizontal")
    for px in range(0, W * 32, rail.width):
        im.alpha_composite(rail, (px, row(TRACK[1]) + (bed.height - rail.height) // 2))
    # the narrow platform's edge over the track: a yellow band, a dark lip
    n0 = row(NORTH[0])
    im.paste((150, 150, 164, 255), (x0, n0, x1, n0 + 16))
    im.paste((246, 190, 52, 255), (x0, n0 + 2, x1, n0 + 8))
    im.paste((92, 92, 106, 255), (x0, n0 + 12, x1, n0 + 16))
    # the boarding platform's edge: concrete, a dark lip where it drops to the track, a yellow line along it
    e0 = row(EDGE + 1)
    im.paste((150, 150, 164, 255), (x0, e0, x1, e0 + 32))
    im.paste((92, 92, 106, 255), (x0, e0, x1, e0 + 4))
    im.paste((246, 190, 52, 255), (x0, e0 + 20, x1, e0 + 25))
    # what a station's floor has: stains, cracks, papers, a can, here and there
    bits = [load(n) for n in FLOOR_BITS]
    for top, bottom, n in ((row(NORTH[1]) + 8, row(NORTH[0]) - 24, 9), (row(EDGE) + 8, row(0.5) - 24, 34)):
        for _ in range(n):
            b = bits[rnd.randrange(len(bits))]
            im.alpha_composite(b, (rnd.randrange(x0 + 8, x1 - 40), rnd.randrange(top, bottom)))
    # the inner wall over the floor, in its steps, its shadow on the concourse; its lights, lifts, stairs, map, screens
    inner_wall(im, inner, row, spec["shadow"])
    light = load("Led_Light_On")
    for x in INNER_LIGHTS:
        im.alpha_composite(light, (int(x * 32), row(INNER - forward(x)) + 18))
    for name, x, up in INNER_ON:
        piece = load(name)
        im.alpha_composite(piece, (int(x * 32), row(INNER - forward(x)) + 96 - int(up * 32) - piece.height))
    # the back wall: its lights along the top, then what hangs on it
    light = load("Led_Light_On")
    for x in LIGHTS:
        im.alpha_composite(light, (int(x * 32), 18))
    for name, x, down in wall:
        im.alpha_composite(load(name), (int(x * 32), int(16 + down * 32)))
    return im


def forward(x):
    """How many tiles forward of INNER the inner wall's step at room x stands."""
    return next(f for a, b, f in STEPS if a <= x < b)


def inner_wall(im, face, row, shadow):
    """The inner wall's steps: each the back wall's slice (face) at its line; a step forward has its top running back
    to INNER (the wall's thickness, seen from above); an outline where the steps meet and down the east end, which is
    capped as the side walls are (a column of the top's colour)."""
    from PIL import ImageDraw
    d = ImageDraw.Draw(im)
    t0 = row(INNER)
    for a, b, f in STEPS:
        x0, x1, w0 = int(a * 32), int(b * 32), row(INNER - f)
        im.paste(face.crop((0, 0, x1 - x0, 96)), (x0, w0))
        if f:
            d.rectangle([x0, t0, x1 - 1, w0 + 9], fill=TOP)
            d.rectangle([x0, t0, x1 - 1, t0 + 1], fill=LINE)
        im.alpha_composite(Image.new("RGBA", (x1 - x0, 16), (0, 0, 0, shadow)), (x0, w0 + 96))
    for (a, b, f), (a2, b2, f2) in zip(STEPS, STEPS[1:]):
        x = int(b * 32)
        if f != f2:  # the forward step's side
            xx = x if f2 > f else x - 2
            d.rectangle([xx, t0, xx + 1, row(INNER - max(f, f2)) + 95], fill=LINE)
    a, b, f = STEPS[-1]
    x1, foot = int(b * 32), row(INNER - f) + 95
    d.rectangle([x1 - 16, t0, x1 - 1, foot], fill=TOP)
    d.rectangle([x1 - 16, t0, x1 - 15, foot], fill=LINE)
    d.rectangle([x1 - 2, t0, x1 - 1, foot], fill=LINE)
    d.rectangle([x1 - 16, foot - 1, x1 - 1, foot], fill=LINE)


if __name__ == "__main__":
    main()
