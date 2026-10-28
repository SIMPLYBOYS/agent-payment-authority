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

    // A town piece's size in tiles, from its cut PNG's header (32 px a tile); 1 x 1 without the LimeZu files.
    static Vector2 CutSize(string key)
    {
        var f = Path.Combine(Application.dataPath, "LimeZu/_cut", key + ".png");
        if (!File.Exists(f)) return Vector2.one;
        var b = File.ReadAllBytes(f);
        int At(int o) => b[o] << 24 | b[o + 1] << 16 | b[o + 2] << 8 | b[o + 3];
        return new Vector2(At(16), At(20)) / 32f;
    }

    // A doorway's shut frame lies over its facade without changing a pixel, composited as drawn (review of 67461a1:
    // the mart's door carried the facade's half-clear shadow again, a darker strip; their values match, alpha over them doesn't).
    [Test]
    public void EachDoorwayShutIsItsFacade()
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
            for (int y = 0; y < door.h; y++)
                for (int x = 0; x < fw; x++)
                {
                    int X = x0 + x, Y = y0 + door.h - 1 - y; // the venue's pixels, y up
                    var under = Vector4.zero;
                    foreach (var (e, c) in pieces) under = Over(At(c, e, X, Y), under);
                    Assert.Less((Over(Pixel(door, x, y), under) - under).magnitude, 2f / 255f, v.layout + ": its shut door changes the facade at " + X + "," + Y);
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
        CollectionAssert.IsSupersetOf(all.Select(x => x.name), new[] { ShopView.DefaultLayout, "cafe", "mart" });
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
            foreach (var p in pts.Append(l.pickup).Append(new Vector2(l.bend.x, l.pickupRow != 0f ? l.pickupRow : l.bend.y))) Assert.IsTrue(p.x > 0f && p.x < 12f && p.y > 0f && p.y < 7f, name + ": " + p + " outside the room");
            Assert.AreNotEqual(l.counter, l.pickup, name + ": the delivery zone is its own place (gap review B6)");
        }
        // The town too: a key missing from the manifest just doesn't draw (review of 7c71cfd).
        var town = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        var pieces = town.ground.Concat(town.props).Concat(town.venues.SelectMany(v => v.exterior)).Concat(town.venues.Where(v => v.doorway != null && !string.IsNullOrEmpty(v.doorway.key)).Select(v => v.doorway)).Select(p => p.key)
            .Concat(town.walkers.Concat(town.inside.SelectMany(g => g.people)).Select(w => w.key))
            .Concat(town.traffic != null ? town.traffic.cars.Select(c => c.key).Concat(town.traffic.lights) : Enumerable.Empty<string>());
        foreach (var k in pieces) CollectionAssert.Contains(keys, k, "town: " + k + " not in limezu-assets.txt");
        // A sheet of frames splits evenly, and is never also tiled (Frames() makes no tiled sprite).
        foreach (var p in town.props.Concat(town.venues.Select(v => v.doorway)).Where(p => p != null && p.frames > 1))
        {
            Assert.AreEqual(0f, p.w, "town: " + p.key + " has frames and is tiled");
            Assert.AreEqual(0, Mathf.RoundToInt(CutSize(p.key).x * 32f) % p.frames, "town: " + p.key + "'s " + p.frames + " frames don't split its width");
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
}
