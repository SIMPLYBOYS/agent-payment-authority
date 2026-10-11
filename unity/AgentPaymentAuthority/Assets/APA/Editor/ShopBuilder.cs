// ShopBuilder: rebuilds Assets/Scenes/Shop.unity from limezu-assets.txt, so
// the scene never depends on one machine (docs/demo-ui.md section 2).
// Batch use (paths relative to the project):
//   Unity -batchmode -nographics -projectPath unity/AgentPaymentAuthority -executeMethod APA.EditorTools.ShopBuilder.Build -quit
//   Unity -batchmode -nographics -projectPath unity/AgentPaymentAuthority -buildTarget WebGL -executeMethod APA.EditorTools.ShopBuilder.BuildWebGL -quit
// Missing LimeZu files leave their fields empty; ShopView then uses
// placeholders and says "LimeZu pending".
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

        // Where each prop stands (tile units; the room is 12 x 8). The cart
        // and the fruit stand keep clear of the customers' corridor
        // (ShopView.Point; docs/demo-presentation-design.md 16.3, 17).
        static readonly (string key, Vector2 at)[] Props =
        {
            ("shelf-1", new Vector2(2.5f, 4.2f)), ("shelf-2", new Vector2(3.5f, 4.2f)), ("shelf-3", new Vector2(5.5f, 4.2f)),
            ("shelf-4", new Vector2(6.5f, 4.2f)), ("fridge-1", new Vector2(9.0f, 5.3f)), ("fridge-2", new Vector2(10.2f, 5.3f)),
            ("counter", new Vector2(10.3f, 1.0f)), ("counter-2", new Vector2(10.3f, 2.4f)), ("sign-open", new Vector2(1.5f, 7.3f)),
            ("cart", new Vector2(2.8f, 0.4f)), ("fruit", new Vector2(5.0f, 0.4f)),
        };

        static Dictionary<string, string> Manifest()
        {
            var map = new Dictionary<string, string>();
            foreach (var line in File.ReadAllLines("limezu-assets.txt"))
            {
                if (line.StartsWith("#") || line.Trim() == "") continue;
                var f = line.Split('\t');
                if (f.Length >= 2) map[f[0]] = LimeZuRoot + f[1];
            }
            return map;
        }

        static Texture2D Load(Dictionary<string, string> m, string key)
        {
            if (!m.TryGetValue(key, out var path) || !File.Exists(path)) return null;
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
            // on the left, the wall at the top, nothing much beyond.
            cam.orthographicSize = 4.8f;
            cam.transform.position = new Vector3(5.1f, 4.1f, -10f);
            cam.clearFlags = CameraClearFlags.SolidColor;
            cam.backgroundColor = new Color(0.12f, 0.12f, 0.16f);

            var shop = new GameObject("Shop").AddComponent<APA.ShopView>();
            shop.floorSheet = Load(m, "floor");
            shop.wallSheet = Load(m, "wall");
            shop.customerA = Load(m, "customer-a");
            shop.customerB = Load(m, "customer-b");
            var tex = new List<Texture2D>();
            var pos = new List<Vector2>();
            var keys = new List<string>();
            foreach (var (key, at) in Props) { tex.Add(Load(m, key)); pos.Add(at); keys.Add(key); }
            shop.props = tex.ToArray();
            shop.propPositions = pos.ToArray();
            shop.propKeys = keys.ToArray();

            var bridge = new GameObject("Bridge").AddComponent<APA.ShopBridge>();
            bridge.view = shop;

            Directory.CreateDirectory("Assets/Scenes");
            EditorSceneManager.SaveScene(scene, ScenePath);
            EditorBuildSettings.scenes = new[] { new EditorBuildSettingsScene(ScenePath, true) };
            AssetDatabase.SaveAssets();
            int missing = 0;
            foreach (var t in tex) if (t == null) missing++;
            if (shop.floorSheet == null) missing++;
            if (shop.wallSheet == null) missing++;
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
