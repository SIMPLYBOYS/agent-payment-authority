#!/usr/bin/env python3
"""The subway's two stations (Aaron 11/8: down the stairs one sees a station as LimeZu's example; the train goes to
the beach's station, and one walks out of it onto the beach), each as one image: Assets/LimeZu/_cut/x-station.png
under the coffee and tea kiosk, x-station-beach.png under the beach; and a dark tile round them (x-dark.png).

The shell is rooms.py's (LimeZu's 3D walls, recoloured to the example's dark grey, a concrete wallpaper on the back
wall), W x H tiles, covering room y -0.5..H-0.5. From the top, as the example (Aaron 11/9: "整個車站還必須要在精緻一點"):
the back wall (graffiti, posters, lights, departure screens, a door, an SOS box); a narrow platform along it; its
yellow edge; the track (the train stands on it, running out through both side walls: the tunnels); the platform
one boards from, its edge and yellow line; the gates' floor and the concourse, the stairs up at its foot. Floors:
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

W, H = 38, 25            # tiles; the town places it with its bottom left at (270, 8.5): town y = room y + 9
NORTH = (18.4, 21.5)     # room y: the platform along the back wall
TRACK = (13.9, 17.9)     # the track bed; the side walls open there
EDGE = 12.9              # the boarding platform's edge, one tile
SUB = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/ME_Theme_Sorter_32x32/20_Subway_and_Train_Station_Singles_32x32/ME_Singles_Subway_and_Train_Station_32x32_"
# On the back wall, per station: (piece, room x of its left, tiles down from the wall's top).
WALLS = {
    "x-station": [("Meh_Graffiti", 1.5, 0.7), ("Control_Big_Monitor", 7, 0.9), ("Three_Posters", 9.5, 1.3), ("Frog_Clean_Graffiti", 13, 0.2),
                  ("Green_Door", 16.5, 1.0), ("Poster_1_And_2", 19, 0.8), ("Control_Big_Monitor", 23.5, 0.9), ("Hew_Graffiti", 26, 0.6),
                  ("SOS_Box", 31.3, 1.0), ("Writing_Graffiti_1", 33, 0.1), ("Blue_Writing_Graffiti", 4, 1.6), ("Arrow_Graffiti_3", 22, 1.9)],
    "x-station-beach": [("Pink_Cloud_Graffiti_1", 1.5, 0.8), ("Control_Big_Monitor", 6, 0.9), ("Flower_Graffiti", 8.5, 0.4), ("Smile_Graffiti", 12, 1.2),
                        ("Black_Double_Door", 15, 1.0), ("Three_Posters", 18, 1.3), ("Control_Big_Monitor", 22.5, 0.9), ("Poster_Truth_2", 25, 0.7),
                        ("Green_FNS_Writing_Graffiti", 28.5, 0.8), ("SOS_Box", 33.4, 1.0), ("Pink_Tentacle_Graffiti_1", 3.5, 1.7), ("Worm_Graffiti", 20.5, 1.9)],
}
LIGHTS = [3, 11, 19, 27, 34]  # room x of the wall lights, along the wall's top
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
    # the back wall: its lights along the top, then what hangs on it
    light = load("Led_Light_On")
    for x in LIGHTS:
        im.alpha_composite(light, (int(x * 32), 18))
    for name, x, down in wall:
        im.alpha_composite(load(name), (int(x * 32), int(16 + down * 32)))
    return im


if __name__ == "__main__":
    main()
