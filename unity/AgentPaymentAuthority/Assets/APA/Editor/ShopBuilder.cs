// ShopBuilder: rebuilds Assets/Scenes/Shop.unity from limezu-assets.txt, so
// the scene never depends on one machine (docs/demo-ui.md section 2).
// Batch use (paths relative to the project):
//   Unity -batchmode -nographics -projectPath unity/AgentPaymentAuthority -executeMethod APA.EditorTools.ShopBuilder.Build -quit
//   Unity -batchmode -nographics -projectPath unity/AgentPaymentAuthority -buildTarget WebGL -executeMethod APA.EditorTools.ShopBuilder.BuildWebGL -quit
// Every manifest image goes into ShopView by key, with its rect; the rooms
// themselves are the layout files in Assets/APA/Resources/Layouts (UX3), read
// at run time. Missing LimeZu files leave their entries empty; ShopView then
// uses placeholders and says "LimeZu pending".
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
            cam.backgroundColor = new Color(0.12f, 0.12f, 0.16f);

            var shop = new GameObject("Shop").AddComponent<APA.ShopView>();
            imported.Clear();
            shop.customerA = Load(m, "customer-a");
            shop.customerB = Load(m, "customer-b");
            var tex = new List<Texture2D>();
            var rects = new List<RectInt>();
            var keys = new List<string>();
            foreach (var (key, _, rect) in m)
            {
                if (key == "license" || key.StartsWith("customer-")) continue;
                tex.Add(Load(m, key)); rects.Add(rect); keys.Add(key);
            }
            shop.art = tex.ToArray();
            shop.artRects = rects.ToArray();
            shop.artKeys = keys.ToArray();

            var bridge = new GameObject("Bridge").AddComponent<APA.ShopBridge>();
            bridge.view = shop;

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
