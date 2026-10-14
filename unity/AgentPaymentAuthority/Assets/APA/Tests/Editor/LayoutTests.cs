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

    static bool Ascii(string s) => s != null && s.All(c => c >= 32 && c < 127);

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
            Assert.IsTrue(Ascii(l.title) && l.title != "", name + ": the title is ASCII (the scene's font)");
            Assert.AreEqual(1f, l.floorTint.a, name + " floor tint");
            Assert.AreEqual(1f, l.wallTint.a, name + " wall tint");
            CollectionAssert.Contains(keys, l.floor, name);
            CollectionAssert.Contains(keys, l.wall, name);
            foreach (var p in l.props) CollectionAssert.Contains(keys, p.key, name + ": prop not in limezu-assets.txt");
            foreach (var n in l.notes) Assert.IsTrue(Ascii(n.text), name + ": " + n.text);
            Assert.IsTrue(l.notes.Any(n => n.text.Contains("after payment")), name + ": the delivery zone says it waits for payment (UX2-D4)");
            // The corridor: outside, the door row is x 1.5; inside, each
            // joined pair shares an x or a y, and every point is in the room.
            var pts = new[] { l.door, l.aisle, l.shelf, l.bend, l.front, l.counter };
            Assert.AreEqual(1.5f, l.door.x, name + " door");
            Assert.AreEqual(1.5f, l.aisle.x, name + " aisle (joined to the door row)");
            for (int i = 1; i < pts.Length; i++)
                Assert.IsTrue(pts[i - 1].x == pts[i].x || pts[i - 1].y == pts[i].y, name + ": " + pts[i - 1] + " -> " + pts[i] + " is not horizontal or vertical");
            foreach (var p in pts) Assert.IsTrue(p.x > 0f && p.x < 12f && p.y > 0f && p.y < 7f, name + ": " + p + " outside the room");
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
