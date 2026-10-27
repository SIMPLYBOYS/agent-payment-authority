// The town's outsides cover their rooms (T2, docs/commercial-town.md): with
// the roof on, no pixel of a room may show, and no street prop may stand in
// a room (it would draw over the opened room). Read from the pieces the
// builder cut (Assets/LimeZu/_cut), opaque pixels only: a sprite's rect is
// not its art (review of b31964f). Without the LimeZu files there is
// nothing to check.
using System.Collections.Generic;
using System.IO;
using APA;
using APA.EditorTools;
using NUnit.Framework;
using UnityEngine;

public class TownCoverTests
{
    const int PPU = 32, RoomW = 12 * PPU, RoomH = 304; // 12 x 9.5 tiles

    static readonly Dictionary<string, (int w, int h, byte[] rgba)> pieces = new Dictionary<string, (int, int, byte[])>();
    static (int w, int h, byte[] rgba) Piece(string key)
    {
        if (pieces.TryGetValue(key, out var p)) return p;
        var path = Path.Combine(Application.dataPath, "LimeZu/_cut", key + ".png");
        if (!File.Exists(path)) Assert.Ignore("the LimeZu pieces are not imported (" + key + ")");
        return pieces[key] = ShopBuilder.ReadPng(File.ReadAllBytes(path));
    }

    // Opaque pixels of a piece placed with its bottom left at (x, y) in pixels, y up; repeated over w x h tiles when w > 0.
    static IEnumerable<Vector2Int> Opaque(Town.Piece e, float ox, float oy)
    {
        var (w, h, rgba) = Piece(e.key);
        int bw = e.w > 0f ? Mathf.RoundToInt(e.w * PPU) : w, bh = e.w > 0f ? Mathf.RoundToInt((e.h > 0f ? e.h : h / (float)PPU) * PPU) : h;
        int x0 = Mathf.RoundToInt((e.x + ox) * PPU), y0 = Mathf.RoundToInt((e.y + oy) * PPU);
        for (int y = 0; y < bh; y++)
            for (int x = 0; x < bw; x++)
                if (rgba[(((h - 1 - y % h) * w) + x % w) * 4 + 3] > 0) yield return new Vector2Int(x0 + x, y0 + y);
    }

    static Town Load() => JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);

    [Test]
    public void EveryOutsideCoversItsRoom()
    {
        foreach (var v in Load().venues)
        {
            var covered = new bool[RoomW, RoomH];
            foreach (var e in v.exterior)
                foreach (var p in Opaque(e, 0f, 0f))
                    if (p.x >= 0 && p.x < RoomW && p.y >= 0 && p.y < RoomH) covered[p.x, p.y] = true;
            int open = 0;
            foreach (var c in covered) if (!c) open++;
            Assert.AreEqual(0, open, v.layout + ": pixels of the room that show with the roof on");
        }
    }

    [Test]
    public void NoStreetPropStandsInARoom()
    {
        var town = Load();
        foreach (var p in town.props)
        {
            var (w, _, _) = Piece(p.key);
            foreach (var v in town.venues)
            {
                int n = 0;
                foreach (var q in Opaque(p, -w / 2f / PPU, 0f)) // props are anchored at their bottom centre
                {
                    int x = q.x - Mathf.RoundToInt(v.x * PPU), y = q.y - Mathf.RoundToInt(v.y * PPU);
                    if (x >= 0 && x < RoomW && y >= 0 && y < RoomH) n++;
                }
                Assert.AreEqual(0, n, p.key + " at " + p.x + "," + p.y + " in the " + v.layout + " room");
            }
        }
    }
}
