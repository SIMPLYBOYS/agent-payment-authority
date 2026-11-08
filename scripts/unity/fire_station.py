#!/usr/bin/env python3
"""The fire station as LimeZu's example (Aaron 11/8), its two pieces the packs do not have whole:

- the station with a fire truck in its open left garage (Assets/LimeZu/_cut/x-fire-station-open.png): the example puts
  Fire_Truck_Hood_Building_Right over the left garage door, at (192, 288) from the building's top left. In the town a
  piece is drawn behind another whose bottom is lower, so the truck inside the station is drawn into it here.
- a firefighter (_cut/x-firefighter.png): the packs have no firefighter character, only the outfit (Modern Exteriors'
  Character_Generator_Addons, with its helmet). A body, its eyes and that outfit, the character generator's layers,
  make one in the generator's own layout (a premade sheet's): the idle row (y 64) is right 0-5, up 6-11, left 12-17,
  down 18-23; limezu-assets.txt cuts the idle frames from it (x-pose-firefighter-down, -up).

    LIMEZU_DIR=var/limezu-both python3 scripts/unity/fire_station.py

LIMEZU_DIR holds Modern_Exteriors_v42.3/ and Modern_Interiors_v41.4/. Not in git (LimeZu's licence), as the kiosk.
"""
import os

from PIL import Image

ROOT = os.path.normpath(os.path.join(os.path.dirname(__file__), "../.."))
CUT = os.path.join(ROOT, "unity/AgentPaymentAuthority/Assets/LimeZu/_cut")
STATION = "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/ME_Theme_Sorter_32x32/18_Fire_Station_Singles_32x32/ME_Singles_Fire_Station_32x32_"
TRUCK_AT = (192, 288)
GEN = "Modern_Interiors_v41.4/2_Characters/Character_Generator/"
LAYERS = [GEN + "Bodies/32x32/Body_32x32_01.png", GEN + "Eyes/32x32/Eyes_32x32_01.png",
          "Modern_Exteriors_v42.3/Modern_Exteriors_32x32/Character_Generator_Addons_32x32/Outfit_Firefighter_32x32.png"]


def main():
    pack = os.environ.get("LIMEZU_DIR")
    files = [STATION + "Building.png", STATION + "Fire_Truck_Hood_Building_Right.png"] + LAYERS
    if not pack or not all(os.path.isfile(os.path.join(pack, p)) for p in files):
        raise SystemExit("set LIMEZU_DIR to the folder that holds Modern_Exteriors_v42.3/ and Modern_Interiors_v41.4/")
    load = lambda p: Image.open(os.path.join(pack, p)).convert("RGBA")
    os.makedirs(CUT, exist_ok=True)
    station = load(files[0])
    station.alpha_composite(load(files[1]), TRUCK_AT)
    station.save(os.path.join(CUT, "x-fire-station-open.png"))
    layers = [load(p) for p in LAYERS]
    size = layers[-1].size  # the outfit's sheet: the body's is a little wider, its frames in the same places
    sheet = Image.new("RGBA", size)
    for im in layers:
        sheet.alpha_composite(im.crop((0, 0) + size))
    sheet.save(os.path.join(CUT, "x-firefighter.png"))
    print(CUT, "x-fire-station-open.png", station.size, "x-firefighter.png", sheet.size)


if __name__ == "__main__":
    main()
