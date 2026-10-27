// The builder's own PNG reader (T2): it cuts the town's pieces from sheets
// taller than Texture2D.LoadImage takes in batch mode, so it must read what
// Unity writes, pixel for pixel, and refuse what it cannot read.
using APA.EditorTools;
using NUnit.Framework;
using UnityEngine;

public class PngTests
{
    [Test]
    public void ReadsWhatUnityWrites()
    {
        const int w = 37, h = 23; // odd sizes: every filter sees a partial row
        var t = new Texture2D(w, h, TextureFormat.RGBA32, false);
        var px = new Color32[w * h];
        for (int i = 0; i < px.Length; i++) px[i] = new Color32((byte)(i * 7), (byte)(i * 13 + 5), (byte)(i >> 2), (byte)(i % 3 == 0 ? 0 : 255 - i % 200));
        t.SetPixels32(px);
        t.Apply();
        var (rw, rh, rgba) = ShopBuilder.ReadPng(t.EncodeToPNG());
        Assert.AreEqual((w, h), (rw, rh));
        for (int y = 0; y < h; y++)
            for (int x = 0; x < w; x++)
            {
                var want = px[(h - 1 - y) * w + x]; // the reader's rows are from the top, a texture's from the bottom
                int i = (y * w + x) * 4;
                Assert.AreEqual(want, new Color32(rgba[i], rgba[i + 1], rgba[i + 2], rgba[i + 3]), "pixel " + x + "," + y);
            }
    }

    [Test]
    public void RefusesATruncatedFile()
    {
        var t = new Texture2D(16, 16, TextureFormat.RGBA32, false);
        var png = t.EncodeToPNG();
        Assert.Catch<System.Exception>(() => ShopBuilder.ReadPng(png[..(png.Length / 2)]));
    }
}
