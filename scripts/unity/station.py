#!/usr/bin/env python3
"""The subway's two stations (Aaron 11/8: down the stairs one sees a station, as LimeZu's example; the train goes to
the beach's station, and one walks out of it onto the beach), each as one image: Assets/LimeZu/_cut/x-station.png
under the coffee and tea kiosk, x-station-beach.png under the beach; and a dark tile round them (x-dark.png).

The shell is rooms.py's (LimeZu's 3D walls, recoloured grey, a wallpaper strip on the back wall), w x h tiles,
covering room y -0.5..h-0.5. As LimeZu's example lays its upper platform (Aaron 11/8: the car shown open, one walks
in by its door, which is on its south side): the track along the back wall, running out through both side walls (the
tunnels); the platform's edge under it, a yellow line on it; the lilac tiles of the platform and of the concourse
below; graffiti and posters on the back wall, over the train. What one walks round (the train, the gates, the machines, the seats, the stairs up) are
the town's props, so that they sort with the walked customer (town.json, the station's part).

    LIMEZU_DIR=var/limezu-both python3 scripts/unity/station.py

LIMEZU_DIR holds Modern_Exteriors_v42.3/ and Modern_Interiors_v41.4/. Not in git (LimeZu's licence), as the rooms.
"""
import os
import sys

from PIL import Image

sys.path.insert(0, os.path.dirname(__file__))
import rooms  # noqa: E402

W, H = 38, 22           # tiles; the town places it with its bottom left at (270, 8.5)
TRACK = (14.6, 18.5)    # room y: the track bed along the back wall, and the side walls open there
EDGE = 13.6             # room y: the platform's edge, one tile under the track
SUB = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/ME_Theme_Sorter_32x32/20_Subway_and_Train_Station_Singles_32x32/ME_Singles_Subway_and_Train_Station_32x32_"
# on the back wall, per station: (piece, room x of its left, rows down from the wall's top); the stairs up stand at x 17.5..20.5
WALLS = {
    "x-station": [("Meh_Graffiti", 2.5, 0.6), ("Three_Posters", 8, 1.2), ("Frog_Clean_Graffiti", 11.5, 0.2), ("Poster_1_And_2", 14, 0.8),
                  ("Hew_Graffiti", 21.5, 0.6), ("Blue_Writing_Graffiti", 27, 0.9), ("Writing_Graffiti_1", 31, 0.1)],
    "x-station-beach": [("Pink_Cloud_Graffiti_1", 3, 0.8), ("Flower_Graffiti", 7.5, 0.4), ("Three_Posters", 12, 1.2), ("Smile_Graffiti", 15, 1.0),
                        ("Poster_Truth_2", 22, 0.7), ("Green_FNS_Writing_Graffiti", 26.5, 0.8), ("Pink_Tentacle_Graffiti_1", 32, 0.6)],
}


def main():
    pack = os.environ.get("LIMEZU_DIR")
    if not pack or not os.path.isdir(os.path.join(pack, rooms.BUILDER)):
        raise SystemExit("set LIMEZU_DIR to the folder that holds Modern_Exteriors_v42.3/ and Modern_Interiors_v41.4/")
    load = lambda name: Image.open(os.path.join(pack, SUB + name + ".png")).convert("RGBA")
    spec = {"size": (W, H), "floor": [256, 1024], "wall": [0, 960], "wall_keep": (14, 8), "shadow": 34,
            "colours": {"75,153,156": [124, 124, 138], "76,137,157": [104, 104, 118], "81,128,153": [70, 70, 84]},
            "entrance": (TRACK[0], 17.4)}  # rooms.py draws a pane and a shadow over the opening: room for them; the bed covers both
    for key, wall in WALLS.items():
        out = os.path.join(rooms.OUT, key + ".png")
        station(spec, pack, load, wall).save(out)
        print(out)
    Image.new("RGBA", (32, 32), (46, 46, 58, 255)).save(os.path.join(rooms.OUT, "x-dark.png"))


def station(spec, pack, load, wall):
    im = rooms.build(spec, pack)
    row = lambda y: int(round((H - 0.5 - y) * 32))  # room y to the image's pixel row
    # the platform and the concourse: lilac tiles
    tile = load("Lilac_Tile_1_Vers_1")
    top, bottom = row(EDGE), row(0.5)
    for py in range(top, bottom, 32):
        for px in range(32, (W - 1) * 32 - 16, 32):
            im.alpha_composite(tile, (px, py))
    # the yellow edge, then the track bed and its rails right across, the side walls open there
    # the edge: concrete, a dark lip where it drops to the track, a yellow line along it
    e0 = row(EDGE + 1)
    im.paste((150, 150, 164, 255), (32, e0, (W - 1) * 32, e0 + 32))
    im.paste((92, 92, 106, 255), (32, e0, (W - 1) * 32, e0 + 4))
    im.paste((246, 190, 52, 255), (32, e0 + 20, (W - 1) * 32, e0 + 25))
    bed = Image.new("RGBA", (W * 32, row(TRACK[0]) - row(TRACK[1])), (54, 54, 66, 255))
    im.paste(bed, (0, row(TRACK[1])))  # right across: through both side walls
    rail = load("Grey_Rail_Modular_Horizontal")
    for px in range(0, W * 32, rail.width):
        im.alpha_composite(rail, (px, row(TRACK[1]) + (bed.height - rail.height) // 2))
    for name, x, down in wall:
        im.alpha_composite(load(name), (int(x * 32), int(16 + down * 32)))
    return im


if __name__ == "__main__":
    main()
