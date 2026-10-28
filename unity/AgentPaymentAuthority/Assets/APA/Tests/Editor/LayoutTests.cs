// EditMode checks of every room layout (UX3, docs/commercial-scenes.md):
// a new scene is a new file in Resources/Layouts, and these hold for it
// without new code. The PlayMode ShopSceneTests walk each room's corridor.
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Text.RegularExpressions;
using APA;
using NUnit.Framework;
using UnityEngine;

public class LayoutTests
{
    static List<(string name, Layout l)> All() =>
        Resources.LoadAll<TextAsset>("Layouts").Select(t => (t.name, JsonUtility.FromJson<Layout>(t.text))).ToList();

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
        var pieces = town.ground.Concat(town.props).Concat(town.venues.SelectMany(v => v.exterior)).Select(p => p.key)
            .Concat(town.walkers.Concat(town.inside.SelectMany(g => g.people)).Select(w => w.key));
        foreach (var k in pieces) CollectionAssert.Contains(keys, k, "town: " + k + " not in limezu-assets.txt");
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
