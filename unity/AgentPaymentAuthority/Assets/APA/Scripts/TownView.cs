// TownView (T2, docs/commercial-town.md): the town around the shop. Its
// ground, props, walking passers-by and the venues' outsides are
// Resources/Town/town.json. The room ShopView shows is a place of its own,
// away from the map (Aaron 10/27, option A: enter a shop through a
// transition, leave the same way). Two views: the town (a click on a venue
// is reported, the page decides; customers A and B, while they have a task,
// stand at their venue's door) and a venue (the room as before). Between
// them a circle closes on the door being entered or left and opens on the
// other side. The camera is always at a whole number of screen pixels per
// art pixel: the town at the largest its frame fits, a venue at the largest
// its room fits (web/pixelscale.js; a canvas smaller than the room shows it
// at a fraction, as before). It shows; it never decides.
using System.Collections.Generic;
using UnityEngine;

namespace APA
{
    // Town: Resources/Town/town.json. Units are 32 px tiles, y up; a piece
    // with w (and h) repeats its sprite over that box from its bottom left,
    // in whole repeats. Ground and outsides are anchored at their bottom
    // left, props and walkers at their bottom centre (as a room's props).
    [System.Serializable]
    public class Town
    {
        public string key;
        public int w, h;               // the map
        public Vector2 focus, frame;   // the town view: its centre, and the box it must show whole
        public Piece[] ground = new Piece[0], props = new Piece[0];
        public Walker[] walkers = new Walker[0];
        public Inside[] inside = new Inside[0];
        public Venue[] venues = new Venue[0];

        [System.Serializable] public class Piece { public string key; public float x, y, w, h; }
        // A venue: the room (layout) it holds, its number and name on the
        // map, its outside, and its door (from its origin): where the
        // transition closes and A and B stand.
        [System.Serializable] public class Venue { public string layout, number, name; public float x, y; public Vector2 door; public Piece[] exterior = new Piece[0]; }
        // A passer-by (illustrative): a premade character's walk row (24 frames of 32 x
        // 64: right, up, left, down), along a path of horizontal and vertical
        // legs; a path that ends where it starts loops, any other goes back.
        // A path of one point is a pose there: its piece's frames (sitting,
        // reading, a clerk standing) play in a loop.
        [System.Serializable] public class Walker { public string key; public float speed = 1.2f; public Vector2[] path = new Vector2[0]; }
        // The people in a shop (Aaron 10/27: a clerk, other customers; illustrative,
        // never A or B), in its room's coordinates, shown with that room only.
        [System.Serializable] public class Inside { public string layout; public Walker[] people = new Walker[0]; }
    }

    public class TownView : MonoBehaviour
    {
        // Assigned by ShopBuilder: the town's pieces, each its own file
        // (cut at build time); null when not imported (the town then draws
        // nothing of that piece).
        public Texture2D[] art = new Texture2D[0];
        public string[] artKeys = new string[0];
        public ShopView shop;

        public const string Accent = "#e0582f"; // the page's accent (web/style.css), for the venue numbers
        public const int PPU = ShopView.PPU;
        public const float RoomW = 15f, RoomH = 10f, RoomCX = 5f, RoomCY = 4.5f; // what a venue view shows of its room (ShopBuilder's old camera)
        public static readonly Vector2 Interior = new Vector2(120f, 0f); // where the room stands, away from the map
        public static readonly Vector2 RoomDoor = new Vector2(1.5f, 1.2f); // the room's door (every layout's), from the room's origin
        public const float Half = 0.35f; // seconds for the circle to close, and again to open

        Town town;
        Camera cam;
        readonly Dictionary<string, (Town.Venue v, float top)> venues = new Dictionary<string, (Town.Venue, float)>();
        readonly List<(SpriteRenderer plate, TextMesh text)> plates = new List<(SpriteRenderer, TextMesh)>();
        public bool HasTown => town != null && System.Array.Exists(art, t => t != null); // a build without the pack has no town (the room keeps its old camera)
        public string Mode { get; private set; } = "venue"; // venue | town: what the page asked for
        string shown = "venue";                               // what the camera shows (they differ during a transition)
        public System.Action<string> Clicked; // town view: a click on a venue, with its layout
        Rect safe;                            // the part of the canvas the page leaves uncovered, CSS pixels from its top left; empty: all of it
        Vector2Int screen;
        public float Scale { get; private set; } = 1f; // screen pixels per art pixel now (tests)

        // The transition: the screen goes dark but for a circle, closing on
        // one side and opening on the other. A ring (dark outside a clear
        // circle) and four dark bands around it, plain sprites: the Web build
        // has no SpriteMask (it came back null there, in a scene or not).
        readonly SpriteRenderer[] iris = new SpriteRenderer[5]; // ring, top, bottom, left, right
        Vector2 irisAt;
        int phase;    // 0 none, 1 closing, 2 opening
        float radius; // the circle's radius now (world units)
        public bool Moving => phase != 0;
        public bool Opening => phase == 2; // tests
        public bool CoverOn => iris[0] != null && iris[0].enabled; // tests

        void Awake()
        {
            cam = Camera.main;
            var file = Resources.Load<TextAsset>("Town/town");
            town = file != null ? JsonUtility.FromJson<Town>(file.text) : null;
            if (shop != null) shop.transform.position = Interior;
            Backdrop();
            if (town == null) return;
            for (int i = 0; i < town.ground.Length; i++) Piece(transform, town.ground[i], -1000 + i, Vector2.zero); // in file order: a later piece over an earlier one
            foreach (var p in town.props) Piece(transform, p, Order(p.y), new Vector2(0.5f, 0f));
            foreach (var v in town.venues)
            {
                var root = new GameObject("venue-" + v.layout).transform;
                root.SetParent(transform, false);
                root.localPosition = new Vector2(v.x, v.y);
                // By depth, as everything on the street: a building by its bottom row, its pieces in file order within it.
                float bottom = float.MaxValue;
                foreach (var e in v.exterior) bottom = Mathf.Min(bottom, e.y);
                for (int i = 0; i < v.exterior.Length; i++) Piece(root, v.exterior[i], Order(v.y + bottom) + i, Vector2.zero);
                // The building's top, from the data (a repeated piece's h, else its file's height): the pill sits just above it.
                float top = 0f;
                foreach (var e in v.exterior) top = Mathf.Max(top, e.y + (e.h > 0f ? e.h : Height(e.key)));
                venues[v.layout] = (v, top);
                Pill(root, v, top);
            }
            foreach (var w in town.walkers) MakeWalker(w, transform, false);
            foreach (var g in town.inside)
            {
                var room = new GameObject("inside-" + g.layout).transform;
                room.SetParent(transform, false);
                room.localPosition = Interior;
                foreach (var w in g.people) MakeWalker(w, room, true);
                insides[g.layout] = room.gameObject;
            }
            foreach (var c in new[] { "consumer-a", "consumer-b" }) MakeStand(c);
        }

        // Order: drawn by depth (lower is nearer), in steps of 1/20 tile; a building's pieces take the next
        // few orders (Awake), so anyone 0.75 tile in front of its bottom row is drawn over it. Labels: 3000+.
        public static int Order(float y) => 2000 - Mathf.RoundToInt(y * 20f);
        const int Labels = 3000, Transition = 5000;

        // Backdrop: the room's surroundings, where it stands away from the
        // map: dark, as a game's inside is, and the pavement outside its door
        // where a customer waits and leaves to.
        void Backdrop()
        {
            var dark = new GameObject("interior-backdrop").AddComponent<SpriteRenderer>();
            dark.transform.SetParent(transform, false);
            dark.sprite = Pixel();
            dark.color = new Color(0.12f, 0.11f, 0.1f);
            dark.sortingOrder = -1100;
            dark.transform.localPosition = Interior + new Vector2(6f, 4.5f);
            dark.transform.localScale = new Vector3(44f, 30f, 1f); // a venue view is at most 30 x 20 units; the town's widest view stops short of it
            Piece(transform, new Town.Piece { key = "x-pave", x = Interior.x - 4f, y = Interior.y, w = 4f, h = 8f }, -1050, Vector2.zero);
        }

        static Sprite pixel, circle, rounded;
        static Sprite Pixel()
        {
            if (pixel != null) return pixel;
            var t = new Texture2D(1, 1) { filterMode = FilterMode.Point };
            t.SetPixel(0, 0, Color.white);
            t.Apply();
            return pixel = UnityEngine.Sprite.Create(t, new Rect(0, 0, 1, 1), new Vector2(0.5f, 0.5f), 1);
        }

        // Ring: a square 2 units wide at scale 1, white outside a clear circle that touches its sides; chunky at the edge (point filtered).
        static Sprite Ring()
        {
            if (circle != null) return circle;
            const int n = 128;
            var t = new Texture2D(n, n) { filterMode = FilterMode.Point, wrapMode = TextureWrapMode.Clamp };
            for (int y = 0; y < n; y++)
                for (int x = 0; x < n; x++)
                {
                    float dx = x + 0.5f - n / 2f, dy = y + 0.5f - n / 2f;
                    t.SetPixel(x, y, dx * dx + dy * dy <= n * n / 4f ? Color.clear : Color.white);
                }
            t.Apply();
            return circle = UnityEngine.Sprite.Create(t, new Rect(0, 0, n, n), new Vector2(0.5f, 0.5f), n / 2f, 0, SpriteMeshType.FullRect);
        }

        Texture2D Tex(string key)
        {
            int i = System.Array.IndexOf(artKeys, key);
            return i >= 0 && i < art.Length ? art[i] : null;
        }

        // Sprite: a piece's whole file, anchored at pivot; FullRect so it can repeat.
        Sprite Sprite(string key, Vector2 pivot)
        {
            var tex = Tex(key);
            return tex == null ? null : UnityEngine.Sprite.Create(tex, new Rect(0, 0, tex.width, tex.height), pivot, PPU, 0, SpriteMeshType.FullRect);
        }

        float Height(string key) => Tex(key) != null ? Tex(key).height / (float)PPU : 0f;

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
                r.sprite = UnityEngine.Sprite.Create(s.texture, s.rect, new Vector2(0.5f, 0.5f), PPU, 0, SpriteMeshType.FullRect);
            }
            return r;
        }

        TextMesh Words(Transform parent, string name, Vector2 at, float size, Color color, int order)
        {
            var go = new GameObject(name);
            go.transform.SetParent(parent, false);
            go.transform.localPosition = at;
            var text = go.AddComponent<TextMesh>();
            var font = Resources.Load<Font>(ShopView.FontName) ?? Resources.GetBuiltinResource<Font>("LegacyRuntime.ttf");
            text.font = font;
            text.GetComponent<MeshRenderer>().material = font.material;
            text.GetComponent<MeshRenderer>().sortingOrder = order + 1;
            text.richText = true;
            text.fontSize = 48;
            text.characterSize = size;
            text.anchor = TextAnchor.MiddleCenter;
            text.color = color;
            return text;
        }

        SpriteRenderer Plate(Transform parent, TextMesh text, Sprite s, Color color, int order)
        {
            var plate = new GameObject(text.name + "-plate").AddComponent<SpriteRenderer>();
            plate.transform.SetParent(parent, false);
            plate.sprite = s;
            plate.color = color;
            if (s == Rounded()) plate.drawMode = SpriteDrawMode.Sliced;
            plate.sortingOrder = order;
            plates.Add((plate, text));
            return plate;
        }

        static readonly Color Ink = new Color(0.114f, 0.102f, 0.082f);

        // Pill: the venue's number (in the accent) and name on a light plate
        // over its roof, as the page's venue list numbers them.
        void Pill(Transform root, Town.Venue v, float top)
        {
            var text = Words(root, "pill", new Vector2(6f, top + 0.5f), 0.085f, Ink, Labels);
            text.text = "<color=" + Accent + ">" + v.number + "</color>  " + v.name;
            Plate(root, text, Rounded(), Color.white, Labels);
        }

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

        // ---- passers-by (illustrative) ----
        class WalkerState { public Town.Walker w; public SpriteRenderer r; public Sprite[] frames; public bool room; public int leg; public int dir = 1; public float tick; public int frame; }
        readonly List<WalkerState> walkers = new List<WalkerState>();
        readonly Dictionary<string, GameObject> insides = new Dictionary<string, GameObject>();
        public IEnumerable<Vector2> WalkerPositions { get { foreach (var s in walkers) if (!s.room) yield return s.r.transform.localPosition; } } // tests
        // The people shown in the room now, in its coordinates (tests).
        public IEnumerable<(string key, Vector2 at)> PeopleInside { get { foreach (var s in walkers) if (s.room && s.r.gameObject.activeInHierarchy) yield return (s.w.key, s.r.transform.localPosition); } }

        void MakeWalker(Town.Walker w, Transform parent, bool room)
        {
            var tex = Tex(w.key);
            if (tex == null || w.path.Length < 1) return;
            var frames = new Sprite[tex.width / 32];
            for (int i = 0; i < frames.Length; i++) frames[i] = UnityEngine.Sprite.Create(tex, new Rect(i * 32, 0, 32, tex.height), new Vector2(0.5f, 0f), PPU);
            var r = new GameObject(w.key).AddComponent<SpriteRenderer>();
            r.transform.SetParent(parent, false);
            r.transform.localPosition = w.path[0];
            r.sprite = frames[0];
            var s = new WalkerState { w = w, r = r, frames = frames, room = room };
            s.r.sortingOrder = Depth(s, w.path[0].y);
            walkers.Add(s);
        }

        // Depth: on the street, the town's scale; in a room, the room's own (ShopView: a prop at y draws at
        // -(y * 10)), just in front of a prop on the same row; always under A and B (50).
        static int Depth(WalkerState s, float y) => s.room ? -(int)(y * 10f) + 1 : Order(y);

        void Walk(WalkerState s, float dt)
        {
            var path = s.w.path;
            if (path.Length == 1)
            {
                // A pose: its frames in a loop, in place.
                s.tick += dt;
                if (s.tick > 0.18f) { s.tick = 0f; s.frame = (s.frame + 1) % s.frames.Length; s.r.sprite = s.frames[s.frame]; }
                return;
            }
            bool loop = path[0] == path[path.Length - 1];
            var p = (Vector2)s.r.transform.localPosition;
            float go = s.w.speed * dt;
            Vector2 d = Vector2.zero;
            for (int guard = 0; go > 0f && guard < 8; guard++)
            {
                int next = s.leg + s.dir;
                var to = path[next];
                d = to - p;
                if (d.magnitude > go) { p += d.normalized * go; go = 0f; break; }
                go -= d.magnitude;
                p = to;
                s.leg = next;
                if (loop && s.leg == path.Length - 1) { s.leg = 0; p = path[0]; }
                else if (!loop && (s.leg == path.Length - 1 || s.leg == 0)) s.dir = -s.dir;
            }
            s.r.transform.localPosition = p;
            s.r.sortingOrder = Depth(s, p.y);
            if (d.sqrMagnitude > 1e-6f)
            {
                int facing = Mathf.Abs(d.x) >= Mathf.Abs(d.y) ? (d.x > 0 ? 0 : 2) : (d.y > 0 ? 1 : 3); // right, up, left, down
                s.tick += dt;
                if (s.tick > 0.1f) { s.tick = 0f; s.frame = (s.frame + 1) % 6; }
                int i = facing * 6 + s.frame;
                if (i < s.frames.Length) s.r.sprite = s.frames[i];
            }
        }

        // ---- A and B at their venue's door, while they are in it (Aaron 10/27: gone once the task ends) ----
        readonly Dictionary<string, (SpriteRenderer body, TextMesh tag, SpriteRenderer plate)> stands = new Dictionary<string, (SpriteRenderer, TextMesh, SpriteRenderer)>();

        void MakeStand(string consumer)
        {
            var sheet = shop != null ? (consumer == "consumer-a" ? shop.customerA : shop.customerB) : null;
            if (sheet == null) return;
            var body = new GameObject(consumer + "-at-door").AddComponent<SpriteRenderer>();
            body.transform.SetParent(transform, false);
            // The premade sheet's idle row, facing down (as ShopView's idleDown).
            body.sprite = UnityEngine.Sprite.Create(sheet, new Rect(18 * 32, sheet.height - 64 - 64, 32, 64), new Vector2(0.5f, 0f), PPU);
            var tag = Words(body.transform, consumer + "-door-tag", new Vector2(0f, 2.4f), 0.085f, Color.white, Labels + 10); // the pills' size: read at the town's scale // not "-tag": ShopView's own label is found by that name; over the buildings, which it overlaps at a door
            tag.text = consumer == "consumer-a" ? "客人 A" : "客人 B";
            var tint = consumer == "consumer-a" ? new Color(0.55f, 0.75f, 1f) : new Color(1f, 0.7f, 0.5f); // ShopView's colours
            var plate = Plate(body.transform, tag, Pixel(), new Color(tint.r * 0.35f, tint.g * 0.35f, tint.b * 0.35f, 0.92f), Labels + 10);
            stands[consumer] = (body, tag, plate);
        }

        public bool StandsAtDoor(string consumer) => stands.TryGetValue(consumer, out var s) && s.body.enabled; // tests
        public Vector2 StandOf(string consumer) => stands.TryGetValue(consumer, out var s) ? (Vector2)s.body.transform.localPosition : Vector2.zero; // tests

        void PlaceStands()
        {
            var key = shop != null ? shop.LayoutKey : null;
            bool there = venues.TryGetValue(key ?? "", out var e);
            foreach (var kv in stands)
            {
                bool on = there && shop.Transacting(kv.Key); // done (delivered, denied, any end): gone from the door
                kv.Value.body.enabled = kv.Value.tag.GetComponent<MeshRenderer>().enabled = kv.Value.plate.enabled = on;
                if (!on) continue;
                var at = new Vector2(e.v.x, e.v.y) + e.v.door + new Vector2(kv.Key == "consumer-a" ? -0.6f : 0.6f, 0f); // a side each, whoever else is there
                kv.Value.body.transform.localPosition = at;
                kv.Value.body.sortingOrder = Order(at.y) + 1;
            }
        }

        // ---- views ----
        public Vector2 Origin(string layout) => layout != null && venues.TryGetValue(layout, out var e) ? new Vector2(e.v.x, e.v.y) : Vector2.zero;
        public bool HasVenue(string layout) => layout != null && venues.ContainsKey(layout);
        // Door: where the circle closes or opens in the town: the current venue's door (its middle), else the town's centre.
        Vector2 Door() => venues.TryGetValue(shop != null ? shop.LayoutKey ?? "" : "", out var e) ? new Vector2(e.v.x, e.v.y) + e.v.door + new Vector2(0f, 1f) : (town != null ? town.focus : Vector2.zero);
        Vector2 Inside() => Interior + RoomDoor + new Vector2(0f, 0.8f);

        // Show: the page's view. safe is the uncovered part of the canvas in
        // CSS pixels from its top left (the page draws the canvas 1:1); empty
        // or missing: all of it. False (nothing changes) for an unknown mode
        // or a town view without a town. Another view than the one shown
        // goes through the circle; the same one only refits.
        public bool Show(string mode, Rect cssSafe)
        {
            if (mode != "venue" && (mode != "town" || !HasTown)) return false;
            safe = cssSafe.width > 0f && cssSafe.height > 0f ? cssSafe : new Rect();
            Mode = mode;
            if (mode == shown)
            {
                if (phase == 1) phase = 2; // asked back before the switch: open again where it closed
                Retarget();                // the view shown, maybe with a new uncovered part (the page measures again once the canvas fills): refit, even while the circle moves
                return true;
            }
            if (phase == 0) { Cover(true, shown == "town" ? Door() : Inside()); radius = Full(); }
            phase = 1; // while opening, it closes back from where it is
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

        // Target: the scale and camera centre of the view shown.
        void Target(out float k, out Vector2 c)
        {
            var s = Safe();
            Vector2 at;
            // The town never below one screen pixel per art pixel: a narrow window crops it rather than blur it.
            if (shown == "town") { k = Mathf.Max(1f, Mathf.Floor(Fit(s, town.frame.x, town.frame.y))); at = town.focus; }
            else { k = Fit(s, RoomW, RoomH); at = Interior + new Vector2(RoomCX, RoomCY); }
            // The target at the safe part's centre, not the canvas's.
            c = at - (s.center - new Vector2(Screen.width, Screen.height) / 2f) / (k * PPU);
        }

        void Retarget()
        {
            if (Screen.width < 1 || Screen.height < 1) return; // a hidden canvas: nothing to fit until it is back
            Target(out float k, out Vector2 c);
            Apply(k, c);
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

        // Full: a radius that clears the whole view from the circle's centre.
        float Full()
        {
            if (cam == null) return 0f;
            float h = cam.orthographicSize, w = h * cam.aspect;
            var c = (Vector2)cam.transform.position - irisAt;
            return new Vector2(Mathf.Abs(c.x) + w, Mathf.Abs(c.y) + h).magnitude + 1f;
        }

        void Cover(bool on, Vector2 at)
        {
            if (iris[0] == null)
                for (int i = 0; i < iris.Length; i++)
                {
                    iris[i] = new GameObject("transition-" + i).AddComponent<SpriteRenderer>();
                    iris[i].transform.SetParent(transform, false);
                    iris[i].sprite = i == 0 ? Ring() : Pixel();
                    iris[i].color = new Color(0.08f, 0.07f, 0.06f);
                    iris[i].sortingOrder = Transition;
                }
            foreach (var r in iris) r.enabled = on;
            irisAt = at;
        }

        // Circle: the ring at the radius (in whole steps of 4 art pixels), the bands beyond it to well past the view.
        void Circle(float r)
        {
            const float far = 200f, o = 1f / PPU; // each band reaches one art pixel into the ring's square: no hairline seam
            r = Mathf.Floor(r / (4f / PPU)) * (4f / PPU);
            iris[0].transform.position = irisAt;
            iris[0].transform.localScale = new Vector3(r, r, 1f);
            Band(iris[1], irisAt + new Vector2(0f, r - o + far / 2f), new Vector2(2f * far, far));
            Band(iris[2], irisAt - new Vector2(0f, r - o + far / 2f), new Vector2(2f * far, far));
            Band(iris[3], irisAt - new Vector2(r - o + far / 2f, 0f), new Vector2(far, 2f * r));
            Band(iris[4], irisAt + new Vector2(r - o + far / 2f, 0f), new Vector2(far, 2f * r));
        }

        static void Band(SpriteRenderer b, Vector2 at, Vector2 size)
        {
            b.transform.position = at;
            b.transform.localScale = new Vector3(size.x, size.y, 1f);
        }

        void Update()
        {
            foreach (var s in walkers) Walk(s, Time.deltaTime);
            if (!HasTown || shown != "town" || phase != 0 || Clicked == null || !Input.GetMouseButtonDown(0) || cam == null) return;
            var p = (Vector2)transform.InverseTransformPoint(cam.ScreenToWorldPoint(Input.mousePosition));
            var hit = VenueAt(p);
            if (hit != null) Clicked(hit);
        }

        // VenueAt: the venue whose outside (12 tiles wide, up to its top,
        // the pill over it) holds a point in town units.
        public string VenueAt(Vector2 p)
        {
            foreach (var kv in venues)
            {
                var o = new Vector2(kv.Value.v.x, kv.Value.v.y);
                if (p.x >= o.x && p.x <= o.x + 12f && p.y >= o.y && p.y <= o.y + kv.Value.top + 1f) return kv.Key;
            }
            return null;
        }
        public Vector2 Focus => town != null ? town.focus : Vector2.zero; // tests
        public float Top(string layout) => venues.TryGetValue(layout ?? "", out var e) ? e.top : 0f; // tests

        void LateUpdate()
        {
            if (cam == null) return;
            var now = new Vector2Int(Screen.width, Screen.height);
            if (now != screen) { screen = now; Retarget(); } // a new canvas size: at once
            if (phase != 0)
            {
                // The circle runs on the real clock, in steps of a few art pixels (chunky, as the art).
                float step = Full() / Half * Time.unscaledDeltaTime;
                if (phase == 1)
                {
                    if (shown == "town") irisAt = Door(); // the room may change after the View (the scene's message comes later): close on the door being entered
                    radius = Mathf.Max(0f, radius - step);
                    if (radius <= 0f)
                    {
                        shown = Mode; // closed: the other side
                        Retarget();
                        Cover(true, shown == "town" ? Door() : Inside());
                        phase = 2;
                    }
                }
                else
                {
                    radius += step;
                    if (radius >= Full()) { phase = 0; radius = 0f; Cover(false, Vector2.zero); }
                }
                if (phase != 0) Circle(radius);
            }
            if (stands.Count > 0) PlaceStands();
            var room = shop != null ? shop.LayoutKey : null;
            foreach (var kv in insides) if (kv.Value.activeSelf != (kv.Key == room)) kv.Value.SetActive(kv.Key == room); // the shop's own people only
            foreach (var (plate, text) in plates)
            {
                var b = text.GetComponent<MeshRenderer>().bounds;
                plate.transform.position = new Vector3(b.center.x, b.center.y, 0f);
                var size = new Vector2(b.size.x + (plate.drawMode == SpriteDrawMode.Sliced ? 0.6f : 0.2f), b.size.y + (plate.drawMode == SpriteDrawMode.Sliced ? 0.25f : 0.1f));
                if (plate.drawMode == SpriteDrawMode.Sliced) plate.size = size;
                else plate.transform.localScale = new Vector3(size.x / plate.transform.parent.lossyScale.x, size.y / plate.transform.parent.lossyScale.y, 1f);
            }
        }
    }
}
