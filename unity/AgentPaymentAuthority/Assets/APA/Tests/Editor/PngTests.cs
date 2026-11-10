// The builder's own PNG reader (T2): it cuts the town's pieces from sheets
// taller than Texture2D.LoadImage takes in batch mode, so it must read what
// Unity writes, pixel for pixel, and refuse what it cannot read.
using System.IO;
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

    // Aaron 11/10 (the food trucks at half their size): a cut wider than the importer's maxTextureSize was
    // imported scaled down, and TownView draws a texture at 32 px a unit of its imported width.
    [Test]
    public void TheTownsCutsKeepTheirSize()
    {
        int n = 0;
        foreach (var line in File.ReadAllLines("limezu-assets.txt"))
        {
            var p = "Assets/LimeZu/_cut/" + line.Split('\t')[0] + ".png"; // what ShopBuilder cuts a town key to, and TownView draws
            if (!line.StartsWith("x-") || !File.Exists(p)) continue;
            n++;
            var b = File.ReadAllBytes(p);
            int Be(int o) => (b[o] << 24) | (b[o + 1] << 16) | (b[o + 2] << 8) | b[o + 3];
            var t = UnityEditor.AssetDatabase.LoadAssetAtPath<Texture2D>(p);
            Assert.AreEqual((Be(16), Be(20)), (t.width, t.height), p); // IHDR's width and height
        }
        if (n == 0) Assert.Ignore("no LimeZu cuts here");
    }

    [Test]
    public void RefusesATruncatedFile()
    {
        var t = new Texture2D(16, 16, TextureFormat.RGBA32, false);
        var png = t.EncodeToPNG();
        Assert.Catch<System.Exception>(() => ShopBuilder.ReadPng(png[..(png.Length / 2)]));
    }

    // The animated sheets are 8-bit palette PNGs (Aaron 10/28: doors, the fountain): Unity writes none,
    // so a 3 x 2 one by hand, its rows filtered Sub and Up a byte a pixel, its last colour without tRNS.
    [Test]
    public void ReadsAPalettePng()
    {
        var png = new MemoryStream();
        png.Write(new byte[] { 137, 80, 78, 71, 13, 10, 26, 10 });
        void Chunk(string type, byte[] data)
        {
            png.Write(new[] { (byte)(data.Length >> 24), (byte)(data.Length >> 16), (byte)(data.Length >> 8), (byte)data.Length });
            png.Write(System.Text.Encoding.ASCII.GetBytes(type));
            png.Write(data);
            png.Write(new byte[4]); // the reader keeps no CRC
        }
        Chunk("IHDR", new byte[] { 0, 0, 0, 3, 0, 0, 0, 2, 8, 3, 0, 0, 0 });
        Chunk("PLTE", new byte[] { 10, 20, 30, 40, 50, 60, 70, 80, 90 });
        Chunk("tRNS", new byte[] { 0, 128 });
        var z = new MemoryStream();
        z.Write(new byte[] { 0x78, 0x9C });
        using (var d = new System.IO.Compression.DeflateStream(z, System.IO.Compression.CompressionMode.Compress, true))
            d.Write(new byte[] { 1, 0, 1, 1, 2, 2, 0, 254 }); // Sub: 0 1 2; Up: 2 1 0
        z.Write(new byte[4]);
        Chunk("IDAT", z.ToArray());
        Chunk("IEND", new byte[0]);
        var (w, h, rgba) = ShopBuilder.ReadPng(png.ToArray());
        Assert.AreEqual((3, 2), (w, h));
        var colour = new[] { new Color32(10, 20, 30, 0), new Color32(40, 50, 60, 128), new Color32(70, 80, 90, 255) };
        int[] want = { 0, 1, 2, 2, 1, 0 };
        for (int i = 0; i < 6; i++) Assert.AreEqual(colour[want[i]], new Color32(rgba[i * 4], rgba[i * 4 + 1], rgba[i * 4 + 2], rgba[i * 4 + 3]), "pixel " + i);
    }
}
