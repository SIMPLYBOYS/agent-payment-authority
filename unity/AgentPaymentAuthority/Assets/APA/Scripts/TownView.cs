// TownView (T2, docs/commercial-town.md): the town the shop stands in. Its
// ground, props and the venues' outsides are Resources/Town/town.json; the
// room ShopView shows stands inside its own venue (Aaron 10/24, option B:
// the roof lifts in place). Two views: the town (every venue roofed, a click
// on one is reported, the page decides) and a venue (its roof lifted, the
// room as before). The camera is always at a whole number of screen pixels
// per art pixel: the town at the largest its frame fits, a venue at the
// largest its room fits, and a move between them steps through the
// multiples between (web/pixelscale.js; a canvas smaller than the room
// shows it at a fraction, as before). It shows; it never decides.
using System.Collections.Generic;
using UnityEngine;

namespace APA
{
    // Town: Resources/Town/town.json. Units are 32 px tiles, y up; a piece
    // with w (and h) repeats its sprite over that box from its bottom left,
    // in whole repeats. Ground and outsides are anchored at their bottom
    // left, props at their bottom centre (as a room's props).
    [System.Serializable]
    public class Town
    {
        public string key;
        public int w, h;               // the map
        public Vector2 focus, frame;   // the town view: its centre, and the box it must show whole
        public Piece[] ground = new Piece[0], props = new Piece[0];
        public Venue[] venues = new Venue[0];

        [System.Serializable] public class Piece { public string key; public float x, y, w, h; }
        // A venue: the room (layout) it holds, its number and name on the
        // map, where its room's origin is, and its outside over the room.
        [System.Serializable] public class Venue { public string layout, number, name; public float x, y; public Piece[] exterior = new Piece[0]; }
    }

    public class TownView : MonoBehaviour
    {
        // Assigned by ShopBuilder: the town's pieces, each its own file
        // (cut from the Modern Exteriors sheets at build time); null when
        // not imported (the town then draws nothing of that piece).
        public Texture2D[] art = new Texture2D[0];
        public string[] artKeys = new string[0];
        public ShopView shop;

        public const string Accent = "#e0582f"; // the page's accent (web/style.css), for the venue numbers
        public const int PPU = ShopView.PPU;
        public const float RoomW = 15f, RoomH = 10f, RoomCX = 5f, RoomCY = 4.5f; // what a venue view shows of its room (ShopBuilder's old camera)
        public const float Step = 0.55f; // seconds for a move between the views

        Town town;
        Camera cam;
        readonly Dictionary<string, (Town.Venue v, List<SpriteRenderer> outside, TextMesh name)> venues = new Dictionary<string, (Town.Venue, List<SpriteRenderer>, TextMesh)>();
        readonly List<(SpriteRenderer plate, TextMesh text)> pills = new List<(SpriteRenderer, TextMesh)>();
        public bool HasTown => town != null && System.Array.Exists(art, t => t != null); // a build without the pack has no town (its room keeps the old camera)
        public string Mode { get; private set; } = "venue"; // venue | town
        public System.Action<string> Clicked; // town view: a click on a venue, with its layout
        string placed;                        // the layout the room was last placed for
        Rect safe;                            // the part of the canvas the page leaves uncovered, CSS pixels from its top left; empty: all of it
        Vector2Int screen;

        // The running move: from (k, c, lift) to the target over Step.
        float k0, k1, t = 1f;
        Vector2 c0, c1;
        string lifted, lowered; // the venue whose roof lifts, and the one whose roof comes back, during the move
        public float Scale { get; private set; } = 1f; // screen pixels per art pixel now (tests)
        public bool Moving => t < 1f;

        void Awake()
        {
            cam = Camera.main;
            var file = Resources.Load<TextAsset>("Town/town");
            town = file != null ? JsonUtility.FromJson<Town>(file.text) : null;
            if (town == null) return;
            for (int i = 0; i < town.ground.Length; i++) Piece(transform, town.ground[i], -1000 + i, Vector2.zero); // in file order: a later piece over an earlier one
            foreach (var p in town.props) Piece(transform, p, 100 - Mathf.RoundToInt(p.y), new Vector2(0.5f, 0f));
            foreach (var v in town.venues)
            {
                var root = new GameObject("venue-" + v.layout).transform;
                root.SetParent(transform, false);
                root.localPosition = new Vector2(v.x, v.y);
                var parts = new List<SpriteRenderer>();
                for (int i = 0; i < v.exterior.Length; i++)
                {
                    var r = Piece(root, v.exterior[i], 150 + i, Vector2.zero);
                    if (r != null) parts.Add(r);
                }
                venues[v.layout] = (v, parts, Pill(root, v, parts));
            }
        }

        // Sprite: a piece's whole file, anchored at pivot; FullRect so it can repeat.
        Sprite Sprite(string key, Vector2 pivot)
        {
            int i = System.Array.IndexOf(artKeys, key);
            var tex = i >= 0 && i < art.Length ? art[i] : null;
            return tex == null ? null : UnityEngine.Sprite.Create(tex, new Rect(0, 0, tex.width, tex.height), pivot, PPU, 0, SpriteMeshType.FullRect);
        }

        SpriteRenderer Piece(Transform parent, Town.Piece p, int order, Vector2 pivot)
        {
            var s = Sprite(p.key, pivot);
            if (s == null) return null;
            var go = new GameObject(p.key);
            go.transform.SetParent(parent, false);
            go.transform.localPosition = new Vector2(p.x, p.y);
            var r = go.AddComponent<SpriteRenderer>();
            r.sprite = s;
            r.sortingOrder = order;
            if (p.w > 0f)
            {
                // Tiled from its bottom left: the box is a whole number of repeats (town.json says so).
                r.drawMode = SpriteDrawMode.Tiled;
                r.tileMode = SpriteTileMode.Continuous;
                r.size = new Vector2(p.w, p.h > 0f ? p.h : s.bounds.size.y);
                go.transform.localPosition += (Vector3)(r.size / 2f);
                s = UnityEngine.Sprite.Create(s.texture, s.rect, new Vector2(0.5f, 0.5f), PPU, 0, SpriteMeshType.FullRect);
                r.sprite = s;
            }
            return r;
        }

        // Pill: the venue's number (in the accent) and name, on a light
        // plate over its roof, as the page's venue list numbers them. Part
        // of the outside: it lifts with the roof.
        TextMesh Pill(Transform root, Town.Venue v, List<SpriteRenderer> outside)
        {
            var go = new GameObject("pill");
            go.transform.SetParent(root, false);
            go.transform.localPosition = new Vector2(6f, 9.75f);
            var text = go.AddComponent<TextMesh>();
            var font = Resources.Load<Font>(ShopView.FontName) ?? Resources.GetBuiltinResource<Font>("LegacyRuntime.ttf");
            text.font = font;
            text.GetComponent<MeshRenderer>().material = font.material;
            text.GetComponent<MeshRenderer>().sortingOrder = 301;
            text.richText = true;
            text.fontSize = 48;
            text.characterSize = 0.1f;
            text.anchor = TextAnchor.MiddleCenter;
            text.color = Ink;
            text.text = Label(v, 1f);
            var plate = new GameObject("pill-plate").AddComponent<SpriteRenderer>();
            plate.transform.SetParent(root, false);
            plate.sprite = Rounded();
            plate.drawMode = SpriteDrawMode.Sliced;
            plate.sortingOrder = 300;
            pills.Add((plate, text));
            outside.Add(plate);
            return text;
        }

        static readonly Color Ink = new Color(0.114f, 0.102f, 0.082f);
        // Label: the pill's words at opacity a (the number's colour tag carries its own alpha).
        static string Label(Town.Venue v, float a) => "<color=" + Accent + ((int)(a * 255)).ToString("x2") + ">" + v.number + "</color>  " + v.name;

        static Sprite rounded;
        // Rounded: a light plate with round ends, sliced to any width.
        static Sprite Rounded()
        {
            if (rounded != null) return rounded;
            const int n = 16;
            var tex = new Texture2D(n, n) { filterMode = FilterMode.Point };
            var px = new Color[n * n];
            var fill = new Color(1f, 0.992f, 0.973f, 1f);
            for (int y = 0; y < n; y++)
                for (int x = 0; x < n; x++)
                {
                    float dx = Mathf.Max(0f, Mathf.Abs(x + 0.5f - n / 2f) - (n / 2f - 7f)), dy = Mathf.Max(0f, Mathf.Abs(y + 0.5f - n / 2f) - (n / 2f - 7f));
                    px[y * n + x] = dx * dx + dy * dy <= 49f ? fill : Color.clear;
                }
            tex.SetPixels(px);
            tex.Apply();
            return rounded = UnityEngine.Sprite.Create(tex, new Rect(0, 0, n, n), new Vector2(0.5f, 0.5f), PPU, 0, SpriteMeshType.FullRect, new Vector4(7, 7, 7, 7));
        }

        // Origin: where a layout's room stands; a layout with no venue stays at the town's origin.
        public Vector2 Origin(string layout) => layout != null && venues.TryGetValue(layout, out var e) ? new Vector2(e.v.x, e.v.y) : Vector2.zero;
        public bool HasVenue(string layout) => layout != null && venues.ContainsKey(layout);
        public float Alpha(string layout) => venues.TryGetValue(layout ?? "", out var e) && e.outside.Count > 0 ? e.outside[0].color.a : -1f; // tests

        // Show: the page's view. safe is the uncovered part of the canvas in
        // CSS pixels from its top left (the page draws the canvas 1:1); empty
        // or missing: all of it. False (nothing changes) for an unknown mode
        // or a town view without a town.
        public bool Show(string mode, Rect cssSafe)
        {
            if (mode != "venue" && (mode != "town" || !HasTown)) return false;
            safe = cssSafe.width > 0f && cssSafe.height > 0f ? cssSafe : new Rect();
            if (mode == Mode) { Retarget(false); return true; }
            var key = shop != null ? shop.LayoutKey : null;
            lifted = mode == "venue" ? key : null;
            lowered = mode == "town" ? key : null;
            Mode = mode;
            Retarget(true);
            return true;
        }

        Rect Safe()
        {
            var all = new Rect(0, 0, Screen.width, Screen.height);
            if (safe.width <= 0f) return all;
            // Flipped against the screen as it is now: the canvas may have been resized since the page measured (review).
            var s = new Rect(safe.x, Screen.height - safe.y - safe.height, safe.width, safe.height);
            var r = Rect.MinMaxRect(Mathf.Max(0f, s.xMin), Mathf.Max(0f, s.yMin), Mathf.Min(all.xMax, s.xMax), Mathf.Min(all.yMax, s.yMax));
            return r.width >= 1f && r.height >= 1f ? r : all;
        }

        // Fit: the largest whole multiple at which w x h tiles fit in r, or
        // the fraction when even one screen pixel per art pixel does not.
        static float Fit(Rect r, float w, float h)
        {
            float f = Mathf.Min(r.width / (w * PPU), r.height / (h * PPU));
            return f >= 1f ? Mathf.Floor(f) : f;
        }

        // Target: the scale and camera centre of the current view.
        void Target(out float k, out Vector2 c)
        {
            var s = Safe();
            Vector2 at;
            // The town never below one screen pixel per art pixel: a narrow window crops it rather than blur it.
            if (Mode == "town") { k = Mathf.Max(1f, Mathf.Floor(Fit(s, town.frame.x, town.frame.y))); at = town.focus; }
            else { k = Fit(s, RoomW, RoomH); at = Origin(shop != null ? shop.LayoutKey : null) + new Vector2(RoomCX, RoomCY); }
            // The target at the safe part's centre, not the canvas's.
            c = at - (s.center - new Vector2(Screen.width, Screen.height) / 2f) / (k * PPU);
        }

        void Retarget(bool animate)
        {
            if (Screen.width < 1 || Screen.height < 1) return; // a hidden canvas: nothing to fit until it is back
            Target(out k1, out c1);
            if (!animate || cam == null) { t = 1f; Apply(k1, c1); Lift(1f); return; }
            k0 = Scale;
            c0 = cam.transform.position;
            t = 0f;
        }

        // Snap: the camera centre such that art pixel edges fall on screen
        // pixel edges (the view's left edge, in art pixels, a multiple of 1/k).
        static float Snap(float c, float k, int screenPx)
        {
            if (k < 1f) return c;
            float half = screenPx / (2f * k);
            return (Mathf.Round((c * PPU - half) * k) / k + half) / PPU;
        }

        void Apply(float k, Vector2 c)
        {
            if (cam == null || Screen.width < 1 || Screen.height < 1) return;
            Scale = k;
            cam.orthographicSize = Screen.height / (2f * PPU * k);
            cam.transform.position = new Vector3(Snap(c.x, k, Screen.width), Snap(c.y, k, Screen.height), -10f);
        }

        // Lift: how far the move has gone (0..1): the roof of the venue being
        // entered fades out, the one being left fades back in; every other
        // roof is on in the town view and on in a venue view too.
        void Lift(float f)
        {
            foreach (var kv in venues)
            {
                float a = 1f;
                if (Mode == "venue" && kv.Key == (shop != null ? shop.LayoutKey : null)) a = kv.Key == lifted ? 1f - f : 0f;
                else if (kv.Key == lowered) a = f;
                foreach (var r in kv.Value.outside) r.color = new Color(1f, 1f, 1f, a);
                if (kv.Value.name.color.a != a) { kv.Value.name.color = new Color(Ink.r, Ink.g, Ink.b, a); kv.Value.name.text = Label(kv.Value.v, a); }
            }
        }

        void Update()
        {
            if (!HasTown || Mode != "town" || Clicked == null || !Input.GetMouseButtonDown(0) || cam == null) return;
            var p = (Vector2)transform.InverseTransformPoint(cam.ScreenToWorldPoint(Input.mousePosition));
            var hit = VenueAt(p);
            if (hit != null) Clicked(hit);
        }

        // VenueAt: the venue whose outside (its room's 12 x 9.5 tiles, the
        // pill over it) holds a point in town units.
        public string VenueAt(Vector2 p)
        {
            foreach (var kv in venues)
            {
                var o = new Vector2(kv.Value.v.x, kv.Value.v.y);
                if (p.x >= o.x && p.x <= o.x + 12f && p.y >= o.y && p.y <= o.y + 10.3f) return kv.Key;
            }
            return null;
        }

        void LateUpdate()
        {
            if (cam == null) return;
            // The room stands in its venue and is placed there at once. In a
            // venue view the camera then goes on to it (the page's View may
            // come before the scene's new room); the first placing and the
            // town view (every roof on) move nothing.
            var key = shop != null ? shop.LayoutKey : null;
            if (shop != null && key != placed)
            {
                bool first = placed == null;
                placed = key;
                shop.transform.position = Origin(key);
                lowered = null;
                lifted = Mode == "venue" && !first ? key : null;
                Retarget(lifted != null);
            }
            if (shop != null) shop.ShowWords(Mode != "town"); // the room's words would float over its roof and in the lane
            var now = new Vector2Int(Screen.width, Screen.height);
            if (now != screen) { screen = now; Retarget(false); } // a new canvas size: at once
            if (t < 1f)
            {
                t = Mathf.Min(1f, t + Time.unscaledDeltaTime / Step);
                float e = t * t * (3f - 2f * t);
                // Whole multiples on the way: the scale steps, the centre glides.
                float k = k0 >= 1f && k1 >= 1f ? Mathf.Round(Mathf.Lerp(k0, k1, e)) : Mathf.Lerp(k0, k1, e);
                Apply(k, Vector2.Lerp(c0, c1, e));
                Lift(e);
                if (t >= 1f) { lifted = lowered = null; Lift(1f); }
            }
            foreach (var (plate, text) in pills)
            {
                var b = text.GetComponent<MeshRenderer>().bounds;
                plate.transform.position = new Vector3(b.center.x, b.center.y, 0f);
                plate.size = new Vector2(b.size.x + 0.6f, b.size.y + 0.25f);
            }
        }
    }
}
