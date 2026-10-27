// ShopBuilder: rebuilds Assets/Scenes/Shop.unity from limezu-assets.txt, so
// the scene never depends on one machine (docs/demo-ui.md section 2).
// Batch use (paths relative to the project):
//   Unity -batchmode -nographics -projectPath unity/AgentPaymentAuthority -executeMethod APA.EditorTools.ShopBuilder.Build -quit
//   Unity -batchmode -nographics -projectPath unity/AgentPaymentAuthority -buildTarget WebGL -executeMethod APA.EditorTools.ShopBuilder.BuildWebGL -quit
// Every manifest image goes into ShopView by key, with its rect; the rooms
// themselves are the layout files in Assets/APA/Resources/Layouts (UX3), read
// at run time. Missing LimeZu files leave their entries empty; ShopView then
// uses placeholders and says "LimeZu pending". The town's pieces (T2,
// keys x-*, Modern Exteriors) are cut from their sheets into files of their
// own under Assets/LimeZu/_cut (ignored too), so the build carries pieces,
// not whole sheets of up to 8288 px; TownView takes them by key.
using System.Collections.Generic;
using System.IO;
using UnityEditor;
using UnityEditor.Build.Reporting;
using UnityEditor.SceneManagement;
using UnityEngine;

namespace APA.EditorTools
{
    public static class ShopBuilder
    {
        public const string ScenePath = "Assets/Scenes/Shop.unity";
        const string LimeZuRoot = "Assets/LimeZu/";
        const string CutRoot = LimeZuRoot + "_cut/";

        // Manifest: key => (path, rect), in file order. The rect is x,y,w,h
        // from the top left; "-" (the whole file) and "sheet" are width 0.
        static List<(string key, string path, RectInt rect)> Manifest()
        {
            var list = new List<(string, string, RectInt)>();
            foreach (var line in File.ReadAllLines("limezu-assets.txt"))
            {
                if (line.StartsWith("#") || line.Trim() == "") continue;
                var f = line.Split('\t');
                if (f.Length < 2) continue;
                var r = new RectInt();
                var n = f.Length > 2 ? f[2].Split(',') : new string[0];
                if (n.Length == 4) r = new RectInt(int.Parse(n[0]), int.Parse(n[1]), int.Parse(n[2]), int.Parse(n[3]));
                list.Add((f[0], LimeZuRoot + f[1], r));
            }
            return list;
        }

        static readonly HashSet<string> imported = new HashSet<string>();

        static Texture2D Load(List<(string key, string path, RectInt rect)> m, string key)
        {
            var path = m.Find(e => e.key == key).path;
            if (path == null || !File.Exists(path)) return null;
            if (!imported.Add(path)) return AssetDatabase.LoadAssetAtPath<Texture2D>(path); // one sheet, several keys
            if (AssetImporter.GetAtPath(path) is TextureImporter ti)
            {
                ti.textureType = TextureImporterType.Sprite;
                ti.spriteImportMode = SpriteImportMode.Single;
                ti.spritePixelsPerUnit = 32;
                ti.filterMode = FilterMode.Point;
                ti.textureCompression = TextureImporterCompression.Uncompressed;
                ti.mipmapEnabled = false;
                ti.npotScale = TextureImporterNPOTScale.None;
                ti.maxTextureSize = 4096;
                ti.SaveAndReimport();
            }
            return AssetDatabase.LoadAssetAtPath<Texture2D>(path);
        }

        static readonly Dictionary<string, (int w, int h, byte[] rgba)> sheets = new Dictionary<string, (int, int, byte[])>();

        // CutPiece: a town piece's rect as its own sprite file, read from the
        // sheet's bytes (no importer setting on the sheet); null when the
        // sheet was not imported.
        static Texture2D CutPiece(string key, string path, RectInt r)
        {
            if (!File.Exists(path) || r.width <= 0) return null;
            if (!sheets.TryGetValue(path, out var src)) sheets[path] = src = ReadPng(File.ReadAllBytes(path));
            if (r.x < 0 || r.y < 0 || r.x + r.width > src.w || r.y + r.height > src.h) throw new System.Exception(key + ": rect outside " + path);
            var px = new Color32[r.width * r.height];
            for (int y = 0; y < r.height; y++)
                for (int x = 0; x < r.width; x++)
                {
                    int i = ((r.y + y) * src.w + r.x + x) * 4; // rects are from the top left; a texture's rows from the bottom
                    px[(r.height - 1 - y) * r.width + x] = new Color32(src.rgba[i], src.rgba[i + 1], src.rgba[i + 2], src.rgba[i + 3]);
                }
            var t = new Texture2D(r.width, r.height, TextureFormat.RGBA32, false);
            t.SetPixels32(px);
            t.Apply();
            Directory.CreateDirectory(CutRoot);
            var cut = CutRoot + key + ".png";
            File.WriteAllBytes(cut, t.EncodeToPNG());
            AssetDatabase.ImportAsset(cut);
            imported.Remove(cut);
            var m = new List<(string, string, RectInt)> { (key, cut, new RectInt()) };
            return Load(m, key);
        }

        // ReadPng: an 8-bit RGBA, non-interlaced PNG (the LimeZu sheets) as
        // rows from the top. Texture2D.LoadImage refuses a sheet taller than
        // the batch editor's 8192 (5_Floor_Modular_Buildings is 8288).
        static (int w, int h, byte[] rgba) ReadPng(byte[] b)
        {
            int Be(int o) => (b[o] << 24) | (b[o + 1] << 16) | (b[o + 2] << 8) | b[o + 3];
            int w = 0, h = 0;
            var idat = new MemoryStream();
            for (int o = 8; o + 8 <= b.Length;)
            {
                int len = Be(o);
                string type = System.Text.Encoding.ASCII.GetString(b, o + 4, 4);
                if (type == "IHDR")
                {
                    w = Be(o + 8); h = Be(o + 12);
                    if (b[o + 16] != 8 || b[o + 17] != 6 || b[o + 20] != 0) throw new System.Exception("not an 8-bit RGBA non-interlaced PNG");
                }
                else if (type == "IDAT") idat.Write(b, o + 8, len);
                else if (type == "IEND") break;
                o += 12 + len;
            }
            int stride = w * 4;
            var raw = new byte[h * (stride + 1)];
            idat.Position = 2; // the zlib header; DeflateStream reads the rest
            using (var z = new System.IO.Compression.DeflateStream(idat, System.IO.Compression.CompressionMode.Decompress))
                for (int n = 0, got; n < raw.Length && (got = z.Read(raw, n, raw.Length - n)) > 0;) n += got;
            var px = new byte[h * stride];
            for (int y = 0; y < h; y++)
            {
                int f = raw[y * (stride + 1)], src = y * (stride + 1) + 1, dst = y * stride;
                for (int x = 0; x < stride; x++)
                {
                    int a = x >= 4 ? px[dst + x - 4] : 0, up = y > 0 ? px[dst + x - stride] : 0, c = x >= 4 && y > 0 ? px[dst + x - stride - 4] : 0;
                    int p = a + up - c, pa = System.Math.Abs(p - a), pb = System.Math.Abs(p - up), pc = System.Math.Abs(p - c);
                    int pred = f == 1 ? a : f == 2 ? up : f == 3 ? (a + up) / 2 : f == 4 ? (pa <= pb && pa <= pc ? a : pb <= pc ? up : c) : 0;
                    px[dst + x] = (byte)(raw[src + x] + pred);
                }
            }
            return (w, h, px);
        }

        public static void Build()
        {
            AssetDatabase.Refresh();
            var m = Manifest();
            var scene = EditorSceneManager.NewScene(NewSceneSetup.EmptyScene, NewSceneMode.Single);

            var cam = new GameObject("Main Camera").AddComponent<Camera>();
            cam.tag = "MainCamera";
            cam.orthographic = true;
            // UX1: the room fills the view (3:2): the sidewalk outside the door
            // on the left, the wall at the top, nothing much beyond. Exactly
            // 480 x 320 art pixels (32 a unit) with whole-pixel edges, so the
            // page draws it at a whole multiple (web/pixelscale.js; Aaron 10/23).
            cam.orthographicSize = 5f;
            cam.transform.position = new Vector3(5f, 4.5f, -10f); // half a tile around the room on every side
            cam.clearFlags = CameraClearFlags.SolidColor;
            cam.backgroundColor = new Color(0.957f, 0.937f, 0.902f); // T2: warm, as the page around it (TownView moves and sizes the camera)

            var shop = new GameObject("Shop").AddComponent<APA.ShopView>();
            imported.Clear();
            sheets.Clear();
            shop.customerA = Load(m, "customer-a");
            shop.customerB = Load(m, "customer-b");
            var tex = new List<Texture2D>();
            var rects = new List<RectInt>();
            var keys = new List<string>();
            foreach (var (key, _, rect) in m)
            {
                if (key.StartsWith("license") || key.StartsWith("customer-") || key.StartsWith("x-")) continue;
                tex.Add(Load(m, key)); rects.Add(rect); keys.Add(key);
            }
            shop.art = tex.ToArray();
            shop.artRects = rects.ToArray();
            shop.artKeys = keys.ToArray();

            var town = new GameObject("Town").AddComponent<APA.TownView>();
            town.shop = shop;
            var townTex = new List<Texture2D>();
            var townKeys = new List<string>();
            foreach (var (key, path, rect) in m)
            {
                if (!key.StartsWith("x-")) continue;
                townTex.Add(CutPiece(key, path, rect)); townKeys.Add(key);
            }
            town.art = townTex.ToArray();
            town.artKeys = townKeys.ToArray();
            tex.AddRange(townTex);

            var bridge = new GameObject("Bridge").AddComponent<APA.ShopBridge>();
            bridge.view = shop;
            bridge.town = town;

            Directory.CreateDirectory("Assets/Scenes");
            EditorSceneManager.SaveScene(scene, ScenePath);
            EditorBuildSettings.scenes = new[] { new EditorBuildSettingsScene(ScenePath, true) };
            AssetDatabase.SaveAssets();
            int missing = 0;
            foreach (var t in tex) if (t == null) missing++;
            if (shop.customerA == null) missing++;
            if (shop.customerB == null) missing++;
            Debug.Log("APA ShopBuilder: scene saved, " + (missing == 0 ? "all LimeZu files present" : missing + " LimeZu file(s) missing, placeholders used"));
        }

        // BuildWebGL rebuilds the scene and makes an uncompressed Web build in
        // Build/WebGL (ignored by git: it contains the LimeZu art).
        public static void BuildWebGL()
        {
            Build();
            PlayerSettings.productName = "AgentPaymentAuthority";
            PlayerSettings.runInBackground = true;
            PlayerSettings.WebGL.compressionFormat = WebGLCompressionFormat.Disabled;
            PlayerSettings.WebGL.decompressionFallback = false;
            PlayerSettings.WebGL.dataCaching = false;
            var report = BuildPipeline.BuildPlayer(new BuildPlayerOptions
            {
                scenes = new[] { ScenePath },
                locationPathName = "Build/WebGL",
                target = BuildTarget.WebGL,
                options = BuildOptions.None,
            });
            Debug.Log("APA ShopBuilder: WebGL build " + report.summary.result + ", " + report.summary.totalSize + " bytes");
            if (report.summary.result != BuildResult.Succeeded) throw new System.Exception("WebGL build " + report.summary.result);
        }
    }
}
