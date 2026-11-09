// EditMode checks of every room layout (UX3, docs/commercial-scenes.md):
// a new scene is a new file in Resources/Layouts, and these hold for it
// without new code. The PlayMode ShopSceneTests walk each room's corridor.
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text.RegularExpressions;
using APA;
using APA.EditorTools;
using NUnit.Framework;
using UnityEngine;

public class LayoutTests
{
    static List<(string name, Layout l)> All() =>
        Resources.LoadAll<TextAsset>("Layouts").Select(t => (t.name, JsonUtility.FromJson<Layout>(t.text))).ToList();

    static bool HasCut(string key) => File.Exists(Path.Combine(Application.dataPath, "LimeZu/_cut", key + ".png")); // without the LimeZu files: nothing to measure

    // A town piece's size in tiles, from its cut PNG's header (32 px a tile); 1 x 1 without the LimeZu files.
    static Vector2 CutSize(string key)
    {
        var f = Path.Combine(Application.dataPath, "LimeZu/_cut", key + ".png");
        if (!File.Exists(f)) return Vector2.one;
        var b = File.ReadAllBytes(f);
        int At(int o) => b[o] << 24 | b[o + 1] << 16 | b[o + 2] << 8 | b[o + 3];
        return new Vector2(At(16), At(20)) / 32f;
    }

    // Composited as drawn: no half-clear pixel of a venue's piece or shut door lies over a half-clear one drawn before it,
    // a shadow drawn twice (review of 67461a1: the mart's door carried the facade's shadow again, a darker strip; their
    // values match, alpha over them doesn't; over an opaque roof it is shading, as the cafe's AC's); and a shut door is its
    // front exactly, but for the mart's, which adds an awning (Aaron 10/28).
    [Test]
    public void NoDoorwayDoublesItsFacadesShadow()
    {
        var town = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        int checkedDoors = 0;
        foreach (var v in town.venues)
        {
            if (v.doorway == null || string.IsNullOrEmpty(v.doorway.key)) continue;
            var door = Cut(v.doorway.key);
            if (door.rgba == null) continue; // no LimeZu files here: nothing drawn to compare
            int fw = door.w / Mathf.Max(1, v.doorway.frames), x0 = Px(v.doorway.x), y0 = Px(v.doorway.y);
            var pieces = v.exterior.Select(e => (e, c: Cut(e.key))).ToList();
            for (int i = 1; i < pieces.Count; i++)
            {
                var (pe, pc) = pieces[i];
                if (pc.rgba == null) continue;
                int bw = pe.w > 0 ? Px(pe.w) : pc.w, bh = pe.w > 0 && pe.h > 0 ? Px(pe.h) : pc.h;
                for (int ly = 0; ly < bh; ly++)
                    for (int lx = 0; lx < bw; lx++)
                    {
                        float a = Pixel(pc, lx % pc.w, pc.h - 1 - ly % pc.h).w;
                        if (a <= 0f || a >= 1f) continue;
                        int X = Px(pe.x) + lx, Y = Px(pe.y) + ly;
                        var under = Vector4.zero;
                        for (int k = 0; k < i; k++) under = Over(At(pieces[k].c, pieces[k].e, X, Y), under);
                        Assert.IsFalse(under.w > 0f && under.w < 1f, v.layout + ": " + pe.key + "'s half-clear pixel over a half-clear one at " + X + "," + Y);
                    }
            }
            for (int y = 0; y < door.h; y++)
                for (int x = 0; x < fw; x++)
                {
                    int X = x0 + x, Y = y0 + door.h - 1 - y; // the venue's pixels, y up
                    var under = Vector4.zero;
                    foreach (var (e, c) in pieces) under = Over(At(c, e, X, Y), under);
                    float a = Pixel(door, x, y).w;
                    Assert.IsFalse(a > 0f && a < 1f && under.w > 0f && under.w < 1f, v.layout + ": its shut door's half-clear pixel over a half-clear one at " + X + "," + Y);
                    if (v.layout != "mart") Assert.Less((Over(Pixel(door, x, y), under) - under).magnitude, 2f / 255f, v.layout + ": its shut door changes the front at " + X + "," + Y);
                }
            checkedDoors++;
        }
        if (checkedDoors == 0) Assert.Ignore("no LimeZu files: nothing to composite");
    }

    static int Px(float tiles) => Mathf.RoundToInt(tiles * 32f);
    static (int w, int h, byte[] rgba) Cut(string key)
    {
        var f = Path.Combine(Application.dataPath, "LimeZu/_cut", key + ".png");
        return File.Exists(f) ? ShopBuilder.ReadPng(File.ReadAllBytes(f)) : (0, 0, null);
    }
    // Pixel: premultiplied, rows from the top; At: a piece's pixel at a venue pixel (y up), a tiled one repeating over its w x h.
    static Vector4 Pixel((int w, int h, byte[] rgba) c, int x, int y)
    {
        int i = (y * c.w + x) * 4;
        float a = c.rgba[i + 3] / 255f;
        return new Vector4(c.rgba[i] / 255f * a, c.rgba[i + 1] / 255f * a, c.rgba[i + 2] / 255f * a, a);
    }
    static Vector4 At((int w, int h, byte[] rgba) c, Town.Piece e, int X, int Y)
    {
        if (c.rgba == null) return Vector4.zero;
        int lx = X - Px(e.x), ly = Y - Px(e.y), bw = e.w > 0 ? Px(e.w) : c.w, bh = e.w > 0 && e.h > 0 ? Px(e.h) : c.h;
        if (lx < 0 || ly < 0 || lx >= bw || ly >= bh) return Vector4.zero;
        return Pixel(c, lx % c.w, c.h - 1 - ly % c.h);
    }
    static Vector4 Over(Vector4 src, Vector4 dst) => src + dst * (1f - src.w);

    static string chars;
    // InFont: every character of a text the scene shows is in its font
    // (GD3-B: a subset, scripts/unity/font-subset.py; a missing one draws
    // nothing). The script writes the characters the font maps beside it:
    // Font.HasCharacter says yes to any character of a dynamic font.
    public static void InFont(string s)
    {
        Assert.NotNull(Resources.Load<Font>(ShopView.FontName), "the scene's font " + ShopView.FontName);
        chars = chars ?? Resources.Load<TextAsset>(ShopView.FontName + "-chars")?.text;
        Assert.NotNull(chars, "the font's character list (scripts/unity/font-subset.py)");
        foreach (var ch in s ?? "") Assert.IsTrue(chars.IndexOf(ch) >= 0, "not in the font: '" + ch + "' in " + s + " (run scripts/unity/font-subset.py)");
    }

    // Every character of the scene's scripts and rooms is in the font, so a
    // label changed without the subset made again fails here.
    [Test]
    public void EveryCharacterTheSceneCanShowIsInTheFont()
    {
        var files = Directory.GetFiles(Path.Combine(Application.dataPath, "APA/Scripts"), "*.cs").Concat(Directory.GetFiles(Path.Combine(Application.dataPath, "APA/Resources/Layouts"), "*.json"))
            .Concat(Directory.GetFiles(Path.Combine(Application.dataPath, "APA/Resources/Town"), "*.json")).ToList();
        Assert.IsNotEmpty(files);
        foreach (var f in files) InFont(new string(File.ReadAllText(f).Where(c => c > 0x7F).Distinct().ToArray()));
        InFont(new string(Enumerable.Range(0x20, 0x5F).Select(i => (char)i).ToArray())); // printable ASCII too
    }

    [Test]
    public void EveryLayoutIsWhole()
    {
        var keys = new HashSet<string>(File.ReadAllLines("limezu-assets.txt").Where(l => !l.StartsWith("#") && l.Trim() != "").Select(l => l.Split('\t')[0]));
        var all = All();
        CollectionAssert.IsSupersetOf(all.Select(x => x.name), new[] { ShopView.DefaultLayout, "cafe", "mart", "post", "hotel" });
        foreach (var (name, l) in all)
        {
            Assert.AreEqual(name, l.key, "a layout's key is its file name");
            Assert.IsTrue(Regex.IsMatch(l.key, "^[a-z0-9-]{1,32}$"), name);
            Assert.IsTrue(!string.IsNullOrEmpty(l.title), name + ": a title");
            InFont(l.title);
            Assert.AreEqual(1f, l.floorTint.a, name + " floor tint");
            Assert.AreEqual(1f, l.wallTint.a, name + " wall tint");
            CollectionAssert.Contains(keys, l.floor, name);
            CollectionAssert.Contains(keys, l.wall, name);
            foreach (var p in l.props) CollectionAssert.Contains(keys, p.key, name + ": prop not in limezu-assets.txt");
            foreach (var n in l.notes) InFont(n.text);
            Assert.IsTrue(l.notes.Any(n => n.text.Contains("付款後才交付")), name + ": the delivery zone says it waits for payment (UX2-D4)");
            // The corridor: outside, the door row is x 1.5; inside, each
            // joined pair shares an x or a y, and every point is in the room.
            var pts = new[] { l.door, l.aisle, l.shelf, l.bend, l.front, l.counter };
            Assert.AreEqual(1.5f, l.door.x, name + " door");
            Assert.AreEqual(1.5f, l.aisle.x, name + " aisle (joined to the door row)");
            for (int i = 1; i < pts.Length; i++)
                Assert.IsTrue(pts[i - 1].x == pts[i].x || pts[i - 1].y == pts[i].y, name + ": " + pts[i - 1] + " -> " + pts[i] + " is not horizontal or vertical");
            foreach (var p in pts.Append(l.pickup).Append(new Vector2(l.bend.x, l.pickupRow != 0f ? l.pickupRow : l.bend.y))) Assert.IsTrue(p.x > 0f && p.x < l.size.x && p.y > 0f && p.y < l.FloorTop, name + ": " + p + " outside the room");
            if (!string.IsNullOrEmpty(l.backdrop))
            {
                CollectionAssert.Contains(keys, l.backdrop, name + ": its backdrop not in limezu-assets.txt");
                // Its walls (interiors v2): A comes in along y 3.0 and B along y 0.6 (ShopView.Point), both through the entrance.
                Assert.IsTrue(l.entrance.x <= 0.6f && l.entrance.y >= 3f + ShopView.FeetHigh && l.entrance.y < l.FloorTop, name + ": the customers come in through its entrance " + l.entrance);
            }
            Assert.AreNotEqual(l.counter, l.pickup, name + ": the delivery zone is its own place (gap review B6)");
        }
        // The town too: a key missing from the manifest just doesn't draw (review of 7c71cfd).
        var town = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        var pieces = town.ground.Concat(town.props).Concat(town.venues.SelectMany(v => v.exterior)).Concat(town.venues.Where(v => v.doorway != null && !string.IsNullOrEmpty(v.doorway.key)).Select(v => v.doorway)).Select(p => p.key)
            .Concat(town.walkers.Concat(town.inside.SelectMany(g => g.people)).Select(w => w.key))
            .Concat(town.traffic != null ? town.traffic.cars.Select(c => c.key).Concat(town.traffic.lights) : Enumerable.Empty<string>())
            .Concat(town.traffic != null ? town.traffic.cars.Where(c => !string.IsNullOrEmpty(c.doors)).Select(c => c.doors) : Enumerable.Empty<string>()) // a bus's doors (Aaron 11/6)
            .Concat(town.traffic != null && town.traffic.visit.from > 0f ? town.traffic.cars.Where(c => !c.vertical && !c.passes && c.speed < 0f && c.y == town.traffic.visit.lane).SelectMany(c => new[] { c.key + "-tilt-up", c.key + "-tilt-down" }).Append(town.traffic.visit.attendant) : Enumerable.Empty<string>()); // the gas station's callers turn their noses; its attendant (V5)
        foreach (var k in pieces) CollectionAssert.Contains(keys, k, "town: " + k + " not in limezu-assets.txt");
        // Traffic (review of 54435f5): a crossing walker makes it over within the red; the cars of a lane start apart.
        if (town.traffic != null && town.traffic.cycle.Length == 3)
        {
            var t = town.traffic;
            foreach (var w in town.walkers)
                for (int i = 1; i < w.path.Length; i++)
                {
                    Vector2 a = w.path[i - 1], b = w.path[i];
                    if (a.x == b.x && a.x >= t.from && a.x <= t.to && Mathf.Min(a.y, b.y) <= t.bottom && Mathf.Max(a.y, b.y) >= t.top)
                        Assert.Less((b - a).magnitude / w.speed + 0.5f, t.cycle[2], "town: " + w.key + " can't cross within the red");
                    // Over the north-south road at the junction (V5): within the east-west green, the other road's red.
                    var j = t.junction;
                    if (j.w > 0f && a.y == b.y && a.y >= j.y && a.y <= j.y + j.h && Mathf.Min(a.x, b.x) <= j.x && Mathf.Max(a.x, b.x) >= j.x + j.w)
                        Assert.Less((b - a).magnitude / w.speed + 0.5f, t.cycle[0], "town: " + w.key + " can't cross the junction within the green");
                }
            float Length(Town.Car c) => c.vertical ? CutSize(c.key).y : CutSize(c.key).x / Mathf.Max(1, c.frames); // along its road
            foreach (var a in t.cars)
                foreach (var b in t.cars)
                    if (a != b && a.vertical == b.vertical && (a.vertical ? a.x == b.x : a.y == b.y))
                        Assert.GreaterOrEqual(a.vertical ? Mathf.Abs(a.y - b.y) : Mathf.Abs(a.x - b.x), (Length(a) + Length(b)) / 2f + 0.8f, "town: " + a.key + " and " + b.key + " start on each other");
            foreach (var c in t.cars)
                if (HasCut(c.key)) Assert.AreEqual(0, Mathf.RoundToInt(CutSize(c.key).x * 32f) % Mathf.Max(1, c.frames), "town: " + c.key + "'s " + c.frames + " frames don't split its width");
            // None starts on the crossing or the junction (Aaron 11/6: a bus begun over its line drove on into the red).
            foreach (var c in t.cars.Where(c => !c.vertical))
            {
                float half = Length(c) / 2f;
                Assert.IsFalse(c.x + half > t.from - 0.3f && c.x - half < t.to + 0.3f, "town: " + c.key + " starts on the crossing at " + c.x);
                Assert.IsFalse(t.junction.w > 0f && c.x + half > t.junction.x - 0.3f && c.x - half < t.junction.x + t.junction.w + 0.3f, "town: " + c.key + " starts on the junction at " + c.x);
            }
            foreach (var c in t.cars.Where(c => !string.IsNullOrEmpty(c.doors)))
                if (HasCut(c.doors)) Assert.AreEqual(0, Mathf.RoundToInt(CutSize(c.doors).x * 32f) % Mathf.Max(1, c.doorFrames), "town: " + c.doors + "'s " + c.doorFrames + " frames don't split its width");
        }
        // A prop over a building never takes one of its orders (review of 3fa65e3: a SALE rack tied with the wall behind it).
        foreach (var v in town.venues)
        {
            float bottom = v.exterior.Min(e => e.y), top = v.exterior.Max(e => e.y + (e.h > 0f ? e.h : CutSize(e.key).y)), right = v.exterior.Max(e => e.x + (e.w > 0f ? e.w : CutSize(e.key).x));
            int from = TownView.Order(v.y + bottom), to = from + v.exterior.Length; // its pieces, then its doorway
            foreach (var p in town.props)
            {
                var s = CutSize(p.key);
                s.x /= Mathf.Max(1, p.frames);
                bool over = p.x + s.x / 2f > v.x && p.x - s.x / 2f < v.x + right && p.y + s.y > v.y + bottom && p.y < v.y + top;
                int o = TownView.Order(p.y);
                Assert.IsFalse(over && o >= from && o <= to, "town: " + p.key + " at " + p.x + "," + p.y + " draws at " + o + ", among " + v.layout + "'s " + from + ".." + to);
            }
        }
        // A sheet of frames splits evenly, and is never also tiled (Frames() makes no tiled sprite).
        foreach (var p in town.props.Concat(town.venues.Select(v => v.doorway)).Where(p => p != null && p.frames > 1))
        {
            Assert.AreEqual(0f, p.w, "town: " + p.key + " has frames and is tiled");
            if (HasCut(p.key)) Assert.AreEqual(0, Mathf.RoundToInt(CutSize(p.key).x * 32f) % p.frames, "town: " + p.key + "'s " + p.frames + " frames don't split its width");
        }
        // Nothing stands on a doorstep (Aaron 10/28: flower boxes in front of the doors): the two
        // tiles in front of a door, 2 either side of it, where A and B stand and people go in and out.
        foreach (var v in town.venues)
            foreach (var p in town.props)
            {
                float d = v.x + v.door.x;
                Assert.IsFalse(Mathf.Abs(p.x - d) < 2f && p.y > v.y - 2f && p.y < v.y, "town: " + p.key + " at " + p.x + "," + p.y + " stands at the door of " + v.layout);
                // and no wide or tall piece standing beside it reaches over the door, A or B (door ± 1.2, its step)
                var s = CutSize(p.key);
                s.x /= Mathf.Max(1, p.frames); // a sheet: one frame's width
                Assert.IsFalse(p.x - s.x / 2f < d + 1.2f && p.x + s.x / 2f > d - 1.2f && p.y < v.y && p.y + s.y > v.y - 1.5f, "town: " + p.key + " at " + p.x + "," + p.y + " covers the door of " + v.layout);
            }
        // A lane past a door runs a tile below it: a passer-by there draws over A and B, but only to their feet (review of 10feaa0).
        foreach (var v in town.venues)
            foreach (var w in town.walkers)
                for (int i = 1; i < w.path.Length; i++)
                {
                    Vector2 a = w.path[i - 1], b = w.path[i];
                    float d = v.x + v.door.x, door = v.y + v.door.y;
                    if (a.y != b.y || Mathf.Max(a.x, b.x) < d - 1.2f || Mathf.Min(a.x, b.x) > d + 1.2f) continue;
                    Assert.IsFalse(a.y > door - 0.9f && a.y < v.y, "town: " + w.key + "'s lane at y " + a.y + " passes over A and B at the door of " + v.layout);
                }
    }

    // The game's pavement (Aaron 10/29): on the map (the camera follows the customer since town v2 V4), off the
    // buildings, over the road only at the crossing; the start on it, every door on it, and every door reachable from the start in half-tile steps.
    [Test]
    public void TheGameWalksThePavementToEveryDoor()
    {
        var town = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        Assert.IsNotEmpty(town.walk);
        bool On(Vector2 p) => town.walk.Any(a => a.Holds(p));
        var t = town.traffic;
        foreach (var a in town.walk)
        {
            Assert.IsTrue((a.x >= town.x0 && a.x + a.w <= town.w || town.apart.Any(s => a.x >= s.x && a.x + a.w <= s.y)) && a.y >= 0f && a.y + a.h <= town.h, "walk " + a.x + "," + a.y + " off the map");
            bool road = a.x < town.w && a.y < t.top && a.y + a.h > t.bottom; // the town's road (the beach apart has none)
            Assert.IsFalse(road && (a.x < t.from || a.x + a.w > t.to), "walk " + a.x + "," + a.y + " on the road off the crossing");
            foreach (var v in town.venues)
            {
                float bottom = v.y + v.exterior.Min(e => e.y), top = v.y + v.exterior.Max(e => e.y + (e.h > 0f ? e.h : CutSize(e.key).y)), right = v.exterior.Max(e => e.x + (e.w > 0f ? e.w : CutSize(e.key).x));
                Assert.IsFalse(a.x < v.x + right && a.x + a.w > v.x && a.y + a.h > bottom && a.y < top, "walk " + a.x + "," + a.y + " reaches into " + v.layout);
            }
        }
        Assert.IsTrue(On(town.start), "the start is on the pavement");
        // Every door, and the step one comes back out on (TownView: 0.8 below it).
        var doors = town.venues.Select(v => new Vector2(v.x, v.y) + v.door).ToList();
        foreach (var d in doors) { Assert.IsTrue(On(d), "door " + d + " off the pavement"); Assert.IsTrue(On(d - new Vector2(0f, 0.8f)), "the step below door " + d); }
        // Reachable: the areas' cells, flooded from the start's (the door's own cell may be the pavement's edge).
        const float c = TownView.Cell;
        var seen = new HashSet<Vector2Int>();
        var todo = new Queue<Vector2Int>();
        var first = new Vector2Int((int)(town.start.x / c), (int)(town.start.y / c));
        seen.Add(first); todo.Enqueue(first);
        while (todo.Count > 0)
        {
            var at = todo.Dequeue();
            foreach (var n in new[] { at + Vector2Int.right, at + Vector2Int.left, at + Vector2Int.up, at + Vector2Int.down })
                if (!seen.Contains(n) && On(new Vector2((n.x + 0.5f) * c, (n.y + 0.5f) * c))) { seen.Add(n); todo.Enqueue(n); }
        }
        foreach (var d in doors)
        {
            var cell = new Vector2Int((int)(d.x / c), (int)(d.y / c));
            Assert.IsTrue(seen.Contains(cell) || seen.Contains(cell + Vector2Int.down) || seen.Contains(cell + Vector2Int.up), "door " + d + " unreachable from the start");
        }
        // Both ends of the street, north and south (Aaron 11/8: past the hospital's door; the map had widened, the pavement not),
        // and up round the hospital (its west side, the passage east of it) and the fire station (its east side, behind it).
        var far = new List<Vector2>();
        foreach (var x in new[] { town.x0 + 1.5f, town.w - 1.5f }) foreach (var y in new[] { 25.6f, 16.8f }) far.Add(new Vector2(x, y));
        far.AddRange(new[] { new Vector2(-50f, 46f), new Vector2(-12.5f, 46f), new Vector2(135f, 40f), new Vector2(122f, 46.5f) });
        foreach (var p in far)
            Assert.IsTrue(seen.Contains(new Vector2Int(Mathf.FloorToInt(p.x / c), Mathf.FloorToInt(p.y / c))), "the pavement at " + p + " unreachable from the start");
        Assert.AreEqual(0, town.subway.Length % 2, "the subway's stairs in pairs");
        foreach (var st in town.subway)
        {
            Assert.IsTrue(On(st.at) && On(st.step), "the stairs at " + st.at + " and their step on the walk");
            Assert.Greater((st.step - st.at).magnitude, TownView.DoorReach, "the step " + st.step + " out of the stairs' reach");
        }
        if (town.subway.Length > 0)
            Assert.IsTrue(seen.Contains(new Vector2Int(Mathf.FloorToInt(town.subway[0].at.x / c), Mathf.FloorToInt(town.subway[0].at.y / c))), "the town's subway stairs unreachable from the start");
    }

    // Every scene the host's UX3 catalog names has its room.
    [Test]
    public void EveryCatalogSceneHasALayout()
    {
        var catalog = File.ReadAllText("../../testdata/demo/scenarios-ux3.json");
        var scenes = Regex.Matches(catalog, "\"scene\":\\s*\"([a-z0-9-]+)\"").Cast<Match>().Select(m => m.Groups[1].Value).ToList();
        Assert.IsNotEmpty(scenes);
        var names = All().Select(x => x.name).ToList();
        foreach (var s in scenes) CollectionAssert.Contains(names, s);
    }

    // A timed loop (Aaron 11/9, the billboards): each frame for its time, the times cycled over the frames, round again.
    [Test]
    public void TimedLoopsHoldEachFrameForItsTime()
    {
        var times = new[] { 4f, 0.1f };
        Assert.AreEqual(0, TownView.FrameAt(3.9f, 4, times), "the ad holds");
        Assert.AreEqual(1, TownView.FrameAt(4.05f, 4, times), "then flicks");
        Assert.AreEqual(2, TownView.FrameAt(4.15f, 4, times), "the next holds");
        Assert.AreEqual(3, TownView.FrameAt(8.15f, 4, times));
        Assert.AreEqual(0, TownView.FrameAt(8.25f, 4, times), "round again");
        Assert.AreEqual(3, TownView.FrameAt(0.4f, 6, null), "untimed: 8 a second");
    }
}
