// ShopView: one shop room and two pixel customers, A and B. It shows a
// projection and nothing else: where a customer stands follows the phase it
// was sent, and an animation never feeds back into a decision.
// The room is a layout (UX3, docs/commercial-scenes.md): a data file in
// Resources/Layouts names its floor, wall, props, corridor points and zone
// notes, so a new scene is a new file, not new code.
// Art is LimeZu (limezu.itch.io) when the files were imported; any missing
// file is a plain placeholder, and the scene then says so.
using System.Collections.Generic;
using UnityEngine;

namespace APA
{
    // Layout: one room, as Resources/Layouts/<key>.json gives it. Units are
    // 32 px tiles; the room is size (12 x 10 unless it says: the floor to y 7, the
    // wall above it). The corridor's eight points are joined as ShopView.Lines
    // says, so each joined pair must share an x or a y.
    [System.Serializable]
    public class Layout
    {
        public string key, title;      // title: the banner's name for the room (the scene's font has its characters)
        public string floor = "floor", wall = "wall";
        // size: the room's art in tiles, x 0..w, y -0.5..h-0.5 (the town's pavement is to its left). backdrop: a
        // manifest key of the whole room as one image, drawn in place of the floor and wall tiles (a shell made
        // by scripts/unity/rooms.py from the layout's "shell", docs/commercial-interiors-v2.md): its back wall's
        // foot is at y h-3.5.
        public Vector2 size = new Vector2(12f, 10f);
        public string backdrop = "";
        // entrance: the left wall's opening, y from x to y (a backdrop's room; rooms.py draws it); the customers come in there.
        public Vector2 entrance = new Vector2(0.5f, 4.5f);
        public float FloorTop => string.IsNullOrEmpty(backdrop) ? 7f : size.y - 3.5f;
        public Color floorTint = Color.white, wallTint = Color.white;
        public Vector2 door, aisle, shelf, bend, front, counter;
        public Vector2 pickup;         // the delivery zone (gap review B6): from bend up or down to pickupRow, along it, then to pickup
        public float pickupRow;        // 0: bend's own row
        public string facing = "right"; // where a customer faces at the counter and the delivery zone: right | up (a bar along the back wall)
        public Prop[] props = new Prop[0];
        public Note[] notes = new Note[0]; // zone labels; the delivery zone's says 付款後才交付
        public Sign[] signs = new Sign[0]; // a section's small wooden sign (interiors v2): on the prop at depth, in front of it

        // lift: drawn that much higher but in front as if at y (an item on a counter);
        // under: on the floor, under every other prop (a rug).
        [System.Serializable] public class Prop { public string key; public float x, y, lift; public bool under; }
        [System.Serializable] public class Note { public string text; public float x, y; }
        [System.Serializable] public class Sign { public string text; public float x, y, depth; }
    }

    public class ShopView : MonoBehaviour
    {
        // Assigned by the editor's ShopBuilder from Assets/LimeZu: every
        // manifest image by key with its rect (x, y, w, h from the top left;
        // width 0: the whole file). An entry is null when not imported.
        public Texture2D customerA, customerB;
        public Texture2D[] art = new Texture2D[0];
        public string[] artKeys = new string[0];
        public RectInt[] artRects = new RectInt[0];

        public const string DefaultLayout = "data"; // the main scene (UX2-D1)
        public const string FontName = "NotoSansTC-APA";
        readonly Dictionary<string, Layout> layouts = new Dictionary<string, Layout>();
        Layout layout;
        GameObject room;
        readonly List<(SpriteRenderer plate, TextMesh text)> notePlates = new List<(SpriteRenderer, TextMesh)>();
        public string LayoutKey => layout != null ? layout.key : null;
        public Vector2 RoomSize => layout != null ? layout.size : new Vector2(12f, 10f);
        public Vector2 SizeOf(string key) => layouts.TryGetValue(key ?? "", out var l) ? l.size : new Vector2(12f, 10f);
        public IEnumerable<string> LayoutKeys => layouts.Keys;
        public bool HasLayout(string key) => key != null && layouts.ContainsKey(key);
        // The props that stand, as placed, for the PlayMode tests: key, where, sprite (a rug is floor: walked over).
        public readonly List<(string key, Vector2 at, Sprite sprite)> Placed = new List<(string, Vector2, Sprite)>();

        public const int PPU = 32;
        public bool UsingPlaceholders { get; private set; }
        public int AnimationsStarted { get; private set; } // tests: one per animated sequence
        [System.NonSerialized] public float Pace = 1f; // tests only: below 1 slows the walk, so the 2.5 s limit snaps it
        // Version 2 (docs/multi-agent-demo-design.md 5.3): every customer at
        // full brightness, a status line each, and B one fixed step aside
        // inside the shop so two customers never stand on one spot. Set by
        // the bridge, which resets the scene when it changes.
        [System.NonSerialized] public bool Multi;
        public const float BAside = 0.45f, BLeft = 1.0f;

        // P is where a customer stands at a point: Point, and in version 2 B
        // a little lower on the inside points and one tile to the left from
        // the shelf on, so two customers never cover each other (the
        // corridor stays horizontal and vertical).
        public Vector2 P(string consumer, int n)
        {
            var p = Point(consumer, n);
            if (Multi && consumer == "consumer-b" && n >= Aisle) p.y -= BAside;
            if (Multi && consumer == "consumer-b" && n >= Shelf) p.x -= BLeft;
            return p;
        }

        // D2 pacing (docs/demo-presentation-design.md 16.5).
        public const float Walk = 4.5f, BrowsePause = 0.6f, DenyPause = 0.8f, MaxTransition = 2.5f;

        // The corridor (16.3): fixed points joined by horizontal and vertical
        // lines that keep clear of the props. A customer only walks along it.
        // Units are 32 px tiles; the room is 12 x 8.
        public const int Out = 0, OutRow = 1, Door = 2, Aisle = 3, Shelf = 4, Bend = 5, Front = 6, Counter = 7, PickupUp = 8, PickupRow = 9, Pickup = 10;
        public static readonly int[,] Lines = { { Out, OutRow }, { OutRow, Door }, { Door, Aisle }, { OutRow, Aisle }, { Aisle, Shelf }, { Shelf, Bend }, { Bend, Front }, { Front, Counter },
            { Bend, PickupUp }, { PickupUp, PickupRow }, { PickupRow, Pickup } };
        const int PointCount = 11;

        // Each customer waits, and leaves to, its own row outside the door;
        // inside, the points are the layout's.
        public Vector2 Point(string consumer, int n)
        {
            // UX1: B's row is 0.6 (was 0.2) so its label under its feet stays
            // inside the view.
            float row = consumer == "consumer-b" ? 0.6f : 3.0f;
            switch (n)
            {
                case Out: return new Vector2(-1.3f, row);
                case OutRow: return new Vector2(1.5f, row);
                case Door: return layout.door;
                case Aisle: return layout.aisle;
                case Shelf: return layout.shelf;
                case Bend: return layout.bend;
                case Front: return layout.front;
                case PickupUp: return new Vector2(layout.bend.x, Row());
                case PickupRow: return new Vector2(layout.pickup.x, Row());
                case Pickup: return layout.pickup;
                default: return layout.counter;
            }
        }

        float Row() => layout.pickupRow != 0f ? layout.pickupRow : layout.bend.y;

        // Shortest ways between the points (Floyd-Warshall).
        void Ways(string consumer, out float[,] d, out int[,] next)
        {
            const int N = PointCount;
            d = new float[N, N];
            next = new int[N, N];
            for (int i = 0; i < N; i++) for (int j = 0; j < N; j++) { d[i, j] = i == j ? 0f : float.MaxValue; next[i, j] = j; }
            for (int e = 0; e < Lines.GetLength(0); e++)
            {
                int a = Lines[e, 0], b = Lines[e, 1];
                d[a, b] = d[b, a] = (P(consumer, a) - P(consumer, b)).magnitude;
            }
            for (int k = 0; k < N; k++) for (int i = 0; i < N; i++) for (int j = 0; j < N; j++)
                if (d[i, k] < float.MaxValue && d[k, j] < float.MaxValue && d[i, k] + d[k, j] < d[i, j]) { d[i, j] = d[i, k] + d[k, j]; next[i, j] = next[i, k]; }
        }

        // Where a phase ends (a Deny ends outside, after the counter).
        static int Station(string phase)
        {
            switch (phase)
            {
                case "entered": return Door;
                case "quoted": case "unknown": return Shelf;
                case "waiting": case "allowed_unpaid": return Counter;
                // P2: a payment in progress, or one a person handles, waits at the counter;
                // once paid the customer waits at the delivery zone (gap review B6).
                case "paying": case "confirming": case "payment_review": return Counter;
                case "paid": case "delivery_failed": return Pickup;
                default: return Out;
            }
        }

        // One leg of a route: walk to a point, then wait there. seg names the
        // label while on it: browse, counter (both （動畫）) or final.
        class Step { public Vector2 to; public int node; public string seg; public float pause; }

        class Customer
        {
            public string id, name;
            public GameObject go;
            public SpriteRenderer body;
            public TextMesh tag;
            public SpriteRenderer plate, marker, ring; // UX1: the label's backing, the colour under the feet, the selection ring
            public Sprite idleDown, idleRight, idleUp;
            public Sprite[][] walk;          // right, up, left, down
            public int facing = 3;
            public Target target = new Target(); // what it is to show (T)
            // The running transition; null once arrived (then P is T).
            public List<Step> route;
            public int at, from = Out, to = Out, frame, gen;
            public bool arrived;
            public float wait, speed, pauseScale, deadline, tick;
            public string seg = "final";
            public System.Action<int> done;
            public bool free; // walked off the corridor by the person (the game): its next run walks back to the door first
        }

        static readonly string[] Facings = { "right", "up", "left", "down" };

        readonly Dictionary<string, Customer> customers = new Dictionary<string, Customer>();
        TextMesh status, banner;
        Font font;
        Sprite pixel, disc, circle; // UX1: a white pixel, a filled ellipse and an ellipse ring
        SpriteRenderer bannerPlate;

        // UX1 selection (docs/commercial-ux1.md): which customer the page has
        // chosen (a ring under its feet), and who was clicked. Selecting is a
        // UI intent only: the page decides, Unity only shows and reports.
        public string Highlighted { get; private set; }
        [System.NonSerialized] public System.Action<string> Picked;

        void Awake()
        {
            // GD3-B: a subset of Noto Sans TC (SIL OFL 1.1) with every character the scene shows
            // (scripts/unity/font-subset.py); the built-in font has no Chinese.
            font = Resources.Load<Font>(FontName) ?? Resources.GetBuiltinResource<Font>("LegacyRuntime.ttf");
            pixel = Shape(1, 1, (x, y) => 1f);
            disc = Shape(32, 16, (x, y) => x * x + y * y <= 1f ? 1f : 0f);
            circle = Shape(48, 24, (x, y) => { float r = x * x + y * y; return r <= 1f && r >= 0.62f ? 1f : 0f; });
            foreach (var t in Resources.LoadAll<TextAsset>("Layouts"))
            {
                var l = JsonUtility.FromJson<Layout>(t.text);
                if (l != null && l.key == t.name) layouts[l.key] = l;
            }
            if (!layouts.TryGetValue(DefaultLayout, out layout)) layouts[DefaultLayout] = layout = new Layout { key = DefaultLayout, title = "" }; // never without a default room (review note)
            BuildRoom();
            customers["consumer-a"] = MakeCustomer("consumer-a", customerA, "客人 A", new Color(0.55f, 0.75f, 1f));
            customers["consumer-b"] = MakeCustomer("consumer-b", customerB, "客人 B", new Color(1f, 0.7f, 0.5f));
            // UX1: the title is small, in the top left corner of the view.
            banner = MakeText("banner", new Vector2(-2.05f, 8.8f), 0.055f, new Color(1f, 1f, 1f, 0.8f));
            banner.anchor = TextAnchor.UpperLeft;
            bannerPlate = Child(gameObject, "banner-plate", pixel, 99, new Color(0.08f, 0.08f, 0.12f, 0.85f));
            // Neutral about where the agent's actions come from: the same build
            // shows the fake demo and the history replay of a recorded real run
            // (docs/cli-poc.md 54.2); the page's header says which.
            Banner();
            status = MakeText("status", new Vector2(6f, -0.35f), 0.08f, new Color(1f, 0.95f, 0.6f));
            ResetAll();
        }

        // Payments: how the host pays (SceneRules.Paying: "" not at all, a
        // local anvil or the Base Sepolia testnet), as the last applied
        // message said; the banner says so.
        string payments = "";
        public void Payments(string mode)
        {
            if (mode == payments) return;
            payments = mode;
            Banner();
        }

        void Banner()
        {
            if (banner == null) return;
            banner.transform.localPosition = new Vector3(-2.05f, layout.size.y - 1.2f, banner.transform.localPosition.z); // the view's top left
            if (status != null) status.transform.localPosition = new Vector3(layout.size.x / 2f, -0.35f, status.transform.localPosition.z);
            banner.text = (payments == "base_sepolia" ? "Base Sepolia 測試網路付款・" : payments == "local_anvil" ? "本機測試鏈付款・" : "這場不付款・") + layout.title + (UsingPlaceholders ? "　（素材未匯入：簡易圖形）" : "　像素素材：LimeZu（limezu.itch.io）");
        }

        // UseLayout: another room. Every customer goes back outside at once,
        // as in ResetAll; false (and nothing changes) for an unknown key.
        public bool UseLayout(string key)
        {
            if (!layouts.TryGetValue(key ?? "", out var l)) return false;
            layout = l;
            BuildRoom();
            Banner();
            ResetAll();
            return true;
        }

        Sprite Cut(Texture2D t, int x, int yTop, int w, int h)
        {
            // Rects are given from the top left, as in the LimeZu sheets.
            return Sprite.Create(t, new Rect(x, t.height - yTop - h, w, h), new Vector2(0.5f, 0f), PPU);
        }

        Sprite Placeholder(Color c, int w, int h)
        {
            var t = new Texture2D(w, h) { filterMode = FilterMode.Point };
            var px = new Color[w * h];
            for (int i = 0; i < px.Length; i++) px[i] = c;
            t.SetPixels(px);
            t.Apply();
            return Sprite.Create(t, new Rect(0, 0, w, h), new Vector2(0.5f, 0f), PPU);
        }

        // Shape: a white sprite of w x h pixels, pivot in the middle; f says
        // how opaque each point is, with x and y from -1 to 1.
        Sprite Shape(int w, int h, System.Func<float, float, float> f)
        {
            var t = new Texture2D(w, h) { filterMode = FilterMode.Point };
            var px = new Color[w * h];
            for (int y = 0; y < h; y++)
                for (int x = 0; x < w; x++)
                    px[y * w + x] = new Color(1f, 1f, 1f, f((x + 0.5f) / w * 2f - 1f, (y + 0.5f) / h * 2f - 1f));
            t.SetPixels(px);
            t.Apply();
            return Sprite.Create(t, new Rect(0, 0, w, h), new Vector2(0.5f, 0.5f), Mathf.Max(w, h) == 1 ? 1 : PPU);
        }

        SpriteRenderer Put(string name, Sprite s, Vector2 at, int order)
        {
            var go = new GameObject(name);
            go.transform.SetParent(room.transform, false);
            go.transform.localPosition = at;
            var r = go.AddComponent<SpriteRenderer>();
            r.sprite = s;
            r.sortingOrder = order;
            return r;
        }

        // Art: the sprite of a manifest key, cut to its rect; a placeholder
        // of w x h when the file was not imported.
        Sprite Art(string key, Color fallback, int w, int h)
        {
            int i = System.Array.IndexOf(artKeys, key);
            var t = i >= 0 && i < art.Length ? art[i] : null;
            if (t == null) { UsingPlaceholders = true; return Placeholder(fallback, w, h); }
            var r = i < artRects.Length ? artRects[i] : default;
            return r.width > 0 ? Cut(t, r.x, r.y, r.width, r.height) : Sprite.Create(t, new Rect(0, 0, t.width, t.height), new Vector2(0.5f, 0f), PPU);
        }

        // BuildRoom: the layout's room, in place of any earlier one.
        void BuildRoom()
        {
            if (room != null) { room.SetActive(false); Destroy(room); }
            room = new GameObject("room-" + layout.key);
            room.transform.SetParent(transform, false);
            Placed.Clear();
            feetOfProps.Clear();
            notePlates.Clear();
            UsingPlaceholders = customerA == null || customerB == null;
            if (!string.IsNullOrEmpty(layout.backdrop))
                Put("backdrop", Art(layout.backdrop, new Color(0.78f, 0.8f, 0.84f), (int)(layout.size.x * PPU), (int)(layout.size.y * PPU)), new Vector2(layout.size.x / 2f, -0.5f), -100);
            else
            {
                var floor = Art(layout.floor, new Color(0.78f, 0.8f, 0.84f), 32, 32);
                var wall = Art(layout.wall, new Color(0.85f, 0.85f, 0.9f), 32, 64);
                for (int x = 0; x < 12; x++)
                {
                    for (int y = 0; y < 7; y++) Put("floor", floor, new Vector2(x + 0.5f, y), -100).color = layout.floorTint;
                    Put("wall", wall, new Vector2(x + 0.5f, 7f), -90).color = layout.wallTint;
                }
            }
            // Outside the door, where a customer waits and leaves to, is
            // TownView's pavement by the room (T2), no longer a strip of the room's.
            foreach (var p in layout.props)
            {
                var at = new Vector2(p.x, p.y);
                var s = Art(p.key, new Color(0.6f, 0.5f, 0.4f), 32, 48);
                var drawn = at + new Vector2(0f, p.lift); // an item on a counter: drawn on its top, in front of it
                Put(p.key, s, drawn, p.under ? -95 : -(int)(at.y * 10)); // a rug: over the floor (-100), under the wall (-90) and the props
                if (!p.under) Placed.Add((p.key, drawn, s));
                if (!p.under && p.lift == 0f) // its foot: its lowest rows, the width of its cut (a little in from its edges)
                    feetOfProps.Add(Rect.MinMaxRect(at.x - s.rect.width / 64f + 0.06f, at.y, at.x + s.rect.width / 64f - 0.06f, at.y + Mathf.Min(Foot, s.rect.height / 32f)));
            }
            // The zone notes, each on a dark plate (readable on a light
            // wall): over the props, under the customers' labels.
            foreach (var n in layout.notes)
            {
                var t = MakeText("note", new Vector2(n.x, n.y), 0.05f, new Color(1f, 0.94f, 0.78f, 1f));
                t.transform.SetParent(room.transform, true);
                t.text = n.text;
                t.GetComponent<MeshRenderer>().sortingOrder = 61;
                notePlates.Add((Child(room, "note-plate", pixel, 60, new Color(0.08f, 0.08f, 0.12f, 0.7f)), t));
            }
            // The sections' signs: drawn as the shelf they hang on (its depth), just in front of it, so whoever
            // walks behind that shelf is behind its sign too.
            foreach (var g in layout.signs)
            {
                int o = -(int)((g.depth != 0f ? g.depth : g.y) * 10f) + 2;
                var t = MakeText("sign", new Vector2(g.x, g.y), 0.035f, new Color(1f, 0.96f, 0.84f, 1f));
                t.transform.SetParent(room.transform, true);
                t.text = g.text;
                t.GetComponent<MeshRenderer>().sortingOrder = o + 1;
                notePlates.Add((Child(room, "sign-plate", pixel, o, new Color(0.59f, 0.38f, 0.24f, 1f)), t));
            }
        }

        Customer MakeCustomer(string id, Texture2D sheet, string label, Color tint)
        {
            var c = new Customer();
            c.go = new GameObject(id);
            c.go.transform.SetParent(transform, false);
            c.body = c.go.AddComponent<SpriteRenderer>();
            c.body.sortingOrder = 50;
            if (sheet != null)
            {
                // Premade LimeZu sheet, 32 x 64 frames: row 1 idle, row 2
                // walk; in each row right 0-5, up 6-11, left 12-17, down 18-23.
                c.idleRight = Frames(sheet, 64, 0)[0];
                c.idleUp = Frames(sheet, 64, 6)[0];
                c.idleDown = Frames(sheet, 64, 18)[0];
                c.walk = new[] { Frames(sheet, 128, 0), Frames(sheet, 128, 6), Frames(sheet, 128, 12), Frames(sheet, 128, 18) };
            }
            else
            {
                var p = new[] { Placeholder(tint, 20, 40) };
                c.idleRight = c.idleDown = c.idleUp = p[0];
                c.walk = new[] { p, p, p, p };
            }
            c.id = id;
            c.name = label;
            c.body.sprite = c.idleDown;
            c.tag = MakeText(id + "-tag", Vector2.zero, 0.065f, Color.white);
            c.tag.transform.SetParent(c.go.transform, false);
            c.tag.text = label;
            // UX1: the label on a solid plate in the customer's colour, the
            // colour again under its feet, and the selection ring.
            c.plate = Child(c.go, id + "-plate", pixel, 99, new Color(tint.r * 0.35f, tint.g * 0.35f, tint.b * 0.35f, 0.92f));
            c.marker = Child(c.go, id + "-marker", disc, 48, new Color(tint.r, tint.g, tint.b, 0.75f));
            c.marker.transform.localScale = new Vector3(1.1f, 1.1f, 1f);
            c.ring = Child(c.go, id + "-ring", circle, 49, new Color(1f, 0.86f, 0.2f, 1f));
            c.ring.enabled = false;
            return c;
        }

        SpriteRenderer Child(GameObject parent, string name, Sprite s, int order, Color color)
        {
            var go = new GameObject(name);
            go.transform.SetParent(parent.transform, false);
            var r = go.AddComponent<SpriteRenderer>();
            r.sprite = s;
            r.sortingOrder = order;
            r.color = color;
            return r;
        }

        Sprite[] Frames(Texture2D t, int yTop, int first)
        {
            var f = new Sprite[6];
            for (int i = 0; i < 6; i++) f[i] = Cut(t, (first + i) * 32, yTop, 32, 64);
            return f;
        }

        TextMesh MakeText(string name, Vector2 at, float size, Color color)
        {
            var go = new GameObject(name);
            go.transform.SetParent(transform, false);
            go.transform.localPosition = at;
            var tm = go.AddComponent<TextMesh>();
            tm.font = font;
            tm.GetComponent<MeshRenderer>().material = font.material;
            tm.characterSize = size;
            tm.fontSize = 48;
            tm.anchor = TextAnchor.MiddleCenter;
            tm.color = color;
            go.GetComponent<MeshRenderer>().sortingOrder = 100;
            return tm;
        }

        // ResetAll: a new viewer or task. Both customers go back outside at
        // once, every running transition is dropped (its done is never
        // called) and the status is cleared.
        public void ResetAll()
        {
            foreach (var c in customers.Values) Clear(c);
            if (status != null) status.text = Multi ? StatusLines() : NoTask;
            // UX1: with two customers the page's cards carry each status; the
            // text stays (tests read it) but is not drawn over the shop.
            if (status != null) status.GetComponent<MeshRenderer>().enabled = !Multi;
            Highlight(null);
        }

        // Reset: one customer only (version 2: that customer's slot changed);
        // the others keep their place, walk and done.
        public void Reset(string consumer)
        {
            if (!customers.TryGetValue(consumer, out var c)) return;
            Clear(c);
            if (status != null && Multi) status.text = StatusLines();
        }

        void Clear(Customer c)
        {
            c.route = null;
            c.done = null;
            c.seg = "final"; // a dropped walk's （動畫） label goes too (review of d7070af)
            c.target = new Target();
            c.from = c.to = Out;
            c.free = false;
            c.go.transform.localPosition = P(c.id, Out);
            c.body.color = new Color(1f, 1f, 1f, Multi ? 1f : 0.45f);
            Idle(c);
            ShowLabel(c);
        }

        // StatusLines: version 2's status, one line per customer.
        string StatusLines()
        {
            var lines = new List<string>();
            foreach (var kv in customers)
            {
                var label = LabelNow(kv.Value);
                lines.Add((kv.Key == "consumer-a" ? "A" : "B") + "：" + (label == "" ? (kv.Value.target.Phase == "empty" ? NoTask : kv.Value.target.Phase) : label));
            }
            return string.Join("\n", lines);
        }

        // For the PlayMode tests: observations only.
        // Transacting: the customer's task goes on in the shop. Inside by the
        // room's own rule (a decision at the counter, the shelf, the delivery
        // zone; a Deny, a delivery or any other end phase walks it out, as
        // does a cancelled or timed-out task), and not at a phase that ends
        // the task where it stands: an Allow that will not be paid (a host
        // that does not pay; a paying one goes on to payment) or a delivery
        // that failed (a person takes it from there). TownView stands A and
        // B at the door only while true (Aaron 10/27: gone once it ends).
        public bool Transacting(string consumer)
        {
            if (!customers.TryGetValue(consumer, out var c)) return false;
            var p = c.target.Phase;
            if (p == "empty" || p == "allowed_unpaid" || p == "delivery_failed" || c.target.End != "") return false;
            return SceneRules.Counter(p) ? p != "denied" : Station(p) != Out;
        }

        public string PhaseOf(string consumer) => customers.TryGetValue(consumer, out var c) ? c.target.Phase : null;
        public Vector2 PositionOf(string consumer) => customers.TryGetValue(consumer, out var c) ? (Vector2)c.go.transform.localPosition : Vector2.zero;
        public bool Moving(string consumer) => customers.TryGetValue(consumer, out var c) && c.route != null;
        public string LabelOf(string consumer) => customers.TryGetValue(consumer, out var c) ? LabelNow(c) : null;
        public string FacingOf(string consumer) => customers.TryGetValue(consumer, out var c) ? Facings[c.facing] : null;
        public float DeadlineOf(string consumer) => customers.TryGetValue(consumer, out var c) && c.route != null ? c.deadline : -1f;
        public string StatusText => status.text;
        public float AlphaOf(string consumer) => customers.TryGetValue(consumer, out var c) ? c.body.color.a : -1f;
        // The points still to walk, from where the customer stands.
        public List<Vector2> RouteOf(string consumer)
        {
            var pts = new List<Vector2>();
            if (!customers.TryGetValue(consumer, out var c)) return pts;
            pts.Add(c.go.transform.localPosition);
            if (c.route != null) for (int i = c.at; i < c.route.Count; i++) pts.Add(c.route[i].to);
            return pts;
        }

        // ---- the game inside (Aaron 11/5: the game walks a customer freely in a shop too) ----
        // The person walks its customer round the room while its task does not hold it (no transition running, at
        // no station of a task: before its run or after it ended); a run then walks it back to the door first.
        public const float FeetHalf = 14f / 32f, FeetHigh = 8f / 32f, Foot = 0.75f; // a LimeZu frame's feet; a prop's foot, its lowest rows
        readonly List<Rect> feetOfProps = new List<Rect>();
        public bool CanWalk(string consumer) => customers.TryGetValue(consumer, out var c) && c.route == null && !Transacting(consumer);

        // Walkable: feet at p stand on the floor, in the entrance or on the pavement outside, clear of every
        // standing prop's foot (a rug is floor; an item on a counter is the counter's).
        public bool Walkable(Vector2 p)
        {
            bool shell = !string.IsNullOrEmpty(layout.backdrop);
            float x0 = shell ? 1f : 0f, x1 = shell ? layout.size.x - 1f : 12f, y0 = shell ? 0.55f : 0.2f, y1 = layout.FloorTop - 0.35f;
            bool room = p.x >= x0 + FeetHalf && p.x <= x1 - FeetHalf && p.y >= y0 && p.y <= y1;
            bool pavement = p.x >= -2.4f && p.x <= -FeetHalf && p.y >= 0.2f && p.y <= y1;
            bool door = p.x > -2.4f && p.x < x0 + FeetHalf + 0.1f && (!shell || p.y >= layout.entrance.x && p.y + FeetHigh <= layout.entrance.y);
            if (!room && !pavement && !door) return false;
            var feet = Rect.MinMaxRect(p.x - FeetHalf, p.y, p.x + FeetHalf, p.y + FeetHigh);
            foreach (var f in feetOfProps) if (f.Overlaps(feet)) return false;
            return true;
        }

        // WalkFree: the person's step d (tiles) for its customer, an axis at a time (along a shelf, not stuck at
        // it); false, nothing moved, when its task holds it.
        public bool WalkFree(string consumer, Vector2 d, float dt)
        {
            if (!customers.TryGetValue(consumer, out var c) || !CanWalk(consumer)) return false;
            Activate(consumer);
            var p = (Vector2)c.go.transform.localPosition;
            var q = p;
            if (Walkable(new Vector2(q.x + d.x, q.y))) q.x += d.x;
            if (Walkable(new Vector2(q.x, q.y + d.y))) q.y += d.y;
            if (d != Vector2.zero)
            {
                c.facing = Mathf.Abs(d.x) >= Mathf.Abs(d.y) ? (d.x > 0 ? 0 : 2) : (d.y > 0 ? 1 : 3); // as it means to go, even against a shelf
                c.tick += dt;
                if (c.tick > 0.1f) { c.tick = 0f; c.frame = (c.frame + 1) % 6; }
            }
            c.body.sprite = q != p ? c.walk[c.facing][c.frame] : c.facing == 1 ? c.idleUp : c.facing == 3 ? c.idleDown : c.walk[c.facing][0];
            c.go.transform.localPosition = q;
            if (q != p) c.free = true;
            ShowLabel(c);
            return true;
        }

        // FreeWay: from one place to another over walkable cells (a quarter tile, breadth first), then
        // straightened (each point on to the farthest one in a clear line); the nearest walkable cell to a place
        // that is not. Null when there is none.
        public List<Vector2> FreeWay(Vector2 from, Vector2 to)
        {
            const float C = 0.25f, X0 = -2.5f, Y0 = 0f;
            int W = Mathf.CeilToInt((layout.size.x + 2.5f) / C), H = Mathf.CeilToInt(layout.size.y / C);
            Vector2 At(int i) => new Vector2(X0 + (i % W + 0.5f) * C, Y0 + (i / W + 0.5f) * C);
            int Of(Vector2 p) => Mathf.Clamp((int)((p.x - X0) / C), 0, W - 1) + Mathf.Clamp((int)((p.y - Y0) / C), 0, H - 1) * W;
            var ok = new bool[W * H];
            for (int i = 0; i < ok.Length; i++) ok[i] = Walkable(At(i));
            int b = Of(to);
            if (!ok[b]) { int best = -1; for (int i = 0; i < ok.Length; i++) if (ok[i] && (best < 0 || (At(i) - to).sqrMagnitude < (At(best) - to).sqrMagnitude)) best = i; if (best < 0) return null; b = best; to = At(b); }
            int a = Of(from);
            var prev = new int[W * H];
            for (int i = 0; i < prev.Length; i++) prev[i] = -1;
            prev[a] = a;
            var queue = new Queue<int>();
            queue.Enqueue(a);
            while (queue.Count > 0 && prev[b] < 0)
            {
                int i = queue.Dequeue(), x = i % W;
                foreach (int n in new[] { x + 1 < W ? i + 1 : -1, x > 0 ? i - 1 : -1, i + W < W * H ? i + W : -1, i - W })
                    if (n >= 0 && prev[n] < 0 && ok[n]) { prev[n] = i; queue.Enqueue(n); }
            }
            if (prev[b] < 0) return null;
            var cells = new List<Vector2> { to };
            for (int i = prev[b]; i != a; i = prev[i]) cells.Insert(0, At(i));
            var way = new List<Vector2>();
            for (var at = from; way.Count == 0 || way[way.Count - 1] != to;)
            {
                int j = cells.Count - 1;
                while (j > 0 && !ClearLine(at, cells[j])) j--;
                way.Add(cells[j]);
                at = cells[j];
                cells.RemoveRange(0, j + 1);
            }
            return way;
        }

        bool ClearLine(Vector2 a, Vector2 b)
        {
            int n = Mathf.CeilToInt((b - a).magnitude / 0.05f);
            for (int i = 1; i <= n; i++) if (!Walkable(Vector2.Lerp(a, b, i / (float)n))) return false;
            return true;
        }

        void Activate(string consumer)
        {
            foreach (var kv in customers) kv.Value.body.color = new Color(1f, 1f, 1f, Multi || kv.Key == consumer ? 1f : 0.45f);
        }

        // Go: a new target (16.4 rules 1 and 2). done(gen) is called once,
        // when the customer has arrived and shows the target; a transition
        // replaced by a newer one, or dropped by ResetAll, never calls it.
        public void Go(string consumer, Target t, bool browse, bool animate, int gen, System.Action<int> done)
        {
            if (!customers.TryGetValue(consumer, out var c)) return;
            Activate(consumer);
            c.target = t;
            var plan = Plan(c, t, browse, out bool ahead);
            c.route = plan;
            c.at = 0;
            c.arrived = false;
            c.wait = 0f;
            c.gen = gen;
            c.done = done;
            if (!animate || plan.Count == 0) { Finish(c); return; } // 16.5 rule 1: a stored snapshot is not news
            c.seg = plan[0].seg;
            AnimationsStarted++;
            // 16.5 rule 4, from the message that set this target, on the real
            // clock: Unity caps a slow frame's deltaTime, so in a slow browser
            // game time falls behind and only a real-time limit holds.
            c.deadline = Time.realtimeSinceStartup + MaxTransition;
            c.speed = Walk;
            c.pauseScale = 1f;
            if (ahead)
            {
                // 16.5 rule 2: the new end lies ahead on the way already
                // taken: the same way at twice the speed, no more waiting.
                foreach (var s in plan) s.pause = 0f;
                c.speed = 2f * Walk;
            }
            else
            {
                // 16.5 rule 3 (or a first move): the corridor at the usual
                // pace, sped up evenly when it would take over 2.5 s.
                float len = 0f, rest = 0f;
                var p = (Vector2)c.go.transform.localPosition;
                foreach (var s in plan) { len += (s.to - p).magnitude; p = s.to; rest += s.pause; }
                float k = (len / Walk + rest) / MaxTransition;
                if (k > 1f) { c.speed = Walk * k; c.pauseScale = 1f / k; }
            }
            ShowLabel(c);
        }

        // Plan: the route from where the customer stands to the target;
        // ahead when its points are the next points of the running route.
        List<Step> Plan(Customer c, Target t, bool browse, out bool ahead)
        {
            var steps = new List<Step>();
            var p = (Vector2)c.go.transform.localPosition;
            int a = c.from, b = c.to;
            if (c.free)
            {
                // Walked off the corridor (the game): round the props back to the door, then the corridor as ever.
                var back = FreeWay(p, P(c.id, Door));
                if (back != null) foreach (var q in back) steps.Add(new Step { to = q, node = Door, seg = "final" });
                steps.Add(new Step { to = P(c.id, Door), node = Door, seg = "final" });
                p = P(c.id, Door);
                a = b = Door;
                c.free = false;
            }
            if (browse)
            {
                Leg(c.id, ref p, ref a, ref b, Shelf, "browse", steps);
                steps.Add(new Step { to = p, node = Shelf, seg = "browse", pause = BrowsePause });
            }
            if (SceneRules.Counter(t.Phase))
            {
                Leg(c.id, ref p, ref a, ref b, Counter, "counter", steps);
                if (t.Phase == "denied")
                {
                    steps.Add(new Step { to = p, node = Counter, seg = "final", pause = DenyPause });
                    Leg(c.id, ref p, ref a, ref b, Out, "final", steps);
                }
            }
            else if (t.Phase == "delivered")
            {
                // Delivered: by the delivery zone, a moment there, then out with the receipt.
                Leg(c.id, ref p, ref a, ref b, Pickup, "final", steps);
                steps.Add(new Step { to = p, node = Pickup, seg = "final", pause = DenyPause });
                Leg(c.id, ref p, ref a, ref b, Out, "final", steps);
            }
            else if (!(browse && t.Phase == "quoted")) Leg(c.id, ref p, ref a, ref b, Station(t.Phase), "final", steps);
            ahead = false;
            if (c.route != null && steps.Count > 0)
            {
                var here = (Vector2)c.go.transform.localPosition;
                var left = Points(c.route, c.at, here);
                var mine = Points(steps, 0, here);
                ahead = mine.Count <= left.Count;
                for (int i = 0; ahead && i < mine.Count; i++) ahead = (mine[i] - left[i]).sqrMagnitude < 1e-6f;
            }
            return steps;
        }

        // Points: the places a route still goes through from step i, once
        // each, leaving out where the customer stands now.
        static List<Vector2> Points(List<Step> route, int i, Vector2 here)
        {
            var pts = new List<Vector2>();
            for (; i < route.Count; i++)
            {
                var last = pts.Count > 0 ? pts[pts.Count - 1] : here;
                if ((route[i].to - last).sqrMagnitude > 1e-6f) pts.Add(route[i].to);
            }
            return pts;
        }

        // Leg: the corridor from p (on the line a-b) to goal.
        void Leg(string consumer, ref Vector2 p, ref int a, ref int b, int goal, string seg, List<Step> steps)
        {
            Ways(consumer, out var d, out var next);
            int via = a;
            if (b != a && (p - P(consumer, b)).magnitude + d[b, goal] < (p - P(consumer, a)).magnitude + d[a, goal]) via = b;
            if ((p - P(consumer, via)).sqrMagnitude > 1e-6f) steps.Add(new Step { to = P(consumer, via), node = via, seg = seg });
            for (int n = via; n != goal;)
            {
                n = next[n, goal];
                steps.Add(new Step { to = P(consumer, n), node = n, seg = seg });
            }
            p = P(consumer, goal);
            a = b = goal;
        }

        // Relabel: the same target place (16.4 rules 3 and 4). The customer
        // keeps its place and any running transition, its time limit
        // included; only the label and the target's data change. True when
        // it has arrived, so the newest sequence can be reported at once.
        public bool Relabel(string consumer, Target t)
        {
            if (!customers.TryGetValue(consumer, out var c)) return false;
            Activate(consumer);
            c.target = t;
            ShowLabel(c);
            return c.route == null;
        }

        void Update()
        {
            foreach (var c in customers.Values) if (c.route != null) Advance(c, Time.unscaledDeltaTime);
            if (Picked != null && Input.GetMouseButtonDown(0) && Camera.main != null)
            {
                var who = PickAt(transform.InverseTransformPoint(Camera.main.ScreenToWorldPoint(Input.mousePosition)));
                if (who != null) Picked(who);
            }
        }

        // PickAt: the customer under a point (local units): its body, from the
        // feet up two tiles and half a tile to each side; the nearest wins.
        public string PickAt(Vector2 p)
        {
            string best = null;
            float bestD = float.MaxValue;
            foreach (var kv in customers)
            {
                var at = (Vector2)kv.Value.go.transform.localPosition;
                if (Mathf.Abs(p.x - at.x) > 0.5f || p.y < at.y - 0.25f || p.y > at.y + 2f) continue;
                float d = (p - (at + new Vector2(0f, 1f))).sqrMagnitude;
                if (d < bestD) { bestD = d; best = kv.Key; }
            }
            return best;
        }

        // Fit puts a plate under a text, a little larger than it.
        static void Fit(SpriteRenderer plate, TextMesh text)
        {
            var b = text.GetComponent<MeshRenderer>().bounds;
            var local = plate.transform.parent.InverseTransformPoint(b.center);
            plate.transform.localPosition = new Vector3(local.x, local.y, 0f);
            plate.transform.localScale = new Vector3(b.size.x + 0.2f, b.size.y + 0.1f, 1f);
        }

        // Highlight: the ring under one customer (null: none). Only the page
        // calls it, through the bridge.
        public void Highlight(string consumer)
        {
            Highlighted = consumer != null && customers.ContainsKey(consumer) ? consumer : null;
            foreach (var kv in customers) kv.Value.ring.enabled = kv.Key == Highlighted;
        }

        // LateUpdate fits each label's plate to its text, and lets the ring breathe.
        void LateUpdate()
        {
            Fit(bannerPlate, banner);
            foreach (var (plate, text) in notePlates) Fit(plate, text);
            foreach (var c in customers.Values)
            {
                Fit(c.plate, c.tag);
                if (c.ring.enabled) c.ring.transform.localScale = Vector3.one * (1f + 0.08f * Mathf.Sin(Time.unscaledTime * 5f));
            }
        }

        void Advance(Customer c, float dt)
        {
            if (Time.realtimeSinceStartup >= c.deadline) { Finish(c); return; } // 16.5 rule 4: never over 2.5 s
            while (c.at < c.route.Count)
            {
                var s = c.route[c.at];
                c.seg = s.seg;
                c.to = s.node;
                if (!c.arrived)
                {
                    var p = (Vector2)c.go.transform.localPosition;
                    float d = (s.to - p).magnitude;
                    if (d > 1e-4f)
                    {
                        var dir = s.to - p;
                        c.facing = Mathf.Abs(dir.x) >= Mathf.Abs(dir.y) ? (dir.x > 0 ? 0 : 2) : (dir.y > 0 ? 1 : 3);
                        float v = c.speed * Pace;
                        if (v * dt < d)
                        {
                            c.go.transform.localPosition = p + dir * (v * dt / d);
                            c.tick += dt;
                            if (c.tick > 0.1f) { c.tick = 0f; c.frame = (c.frame + 1) % 6; }
                            c.body.sprite = c.walk[c.facing][c.frame];
                            ShowLabel(c);
                            return;
                        }
                        dt -= d / v;
                    }
                    c.go.transform.localPosition = s.to;
                    c.from = s.node;
                    c.arrived = true;
                    c.wait = s.pause * c.pauseScale;
                }
                if (c.wait > 0f)
                {
                    Idle(c);
                    ShowLabel(c);
                    if (dt < c.wait) { c.wait -= dt; return; }
                    dt -= c.wait;
                    c.wait = 0f;
                }
                c.at++;
                c.arrived = false;
            }
            Finish(c);
        }

        // Finish: at the target's place, showing it (P = T); then done.
        void Finish(Customer c)
        {
            if (c.route.Count > 0)
            {
                var last = c.route[c.route.Count - 1];
                c.go.transform.localPosition = last.to;
                c.from = c.to = last.node;
            }
            c.route = null;
            c.seg = "final";
            Idle(c);
            ShowLabel(c);
            var done = c.done;
            c.done = null;
            done?.Invoke(c.gen);
        }

        // Standing still faces down; at the counter or the delivery zone it
        // faces them (their props stand to the right in every room).
        void Idle(Customer c)
        {
            var here = (Vector2)c.go.transform.localPosition;
            bool atCounter = (here - P(c.id, Counter)).sqrMagnitude < 1e-4f || (here - P(c.id, Pickup)).sqrMagnitude < 1e-4f;
            bool up = atCounter && layout.facing == "up";
            c.facing = up ? 1 : atCounter ? 0 : 3;
            c.body.sprite = up ? c.idleUp : atCounter ? c.idleRight : c.idleDown;
        }

        // LabelNow: the label of the segment on now; a display-only segment
        // carries AnimMark and says no event of its own stands behind it.
        public const string AnimMark = "（動畫）", NoTask = "沒有客人";
        public const string Browsing = "逛商品中" + AnimMark, ToCounter = "走向櫃台" + AnimMark;
        static string LabelNow(Customer c)
        {
            switch (c.seg)
            {
                case "browse": return Browsing + "：沒有對應的事件";
                case "counter": return ToCounter + "：報價已綁在請購裡";
                default: return c.target.Label;
            }
        }

        void ShowLabel(Customer c)
        {
            var label = LabelNow(c);
            // UX1: with two customers the canvas keeps a display-only
            // segment's （動畫） mark and drops its explanation (the page's
            // card says it), so a walking label does not cover the other.
            var shown = Multi && c.seg == "browse" ? Browsing : Multi && c.seg == "counter" ? ToCounter : label;
            c.tag.text = shown == "" ? c.name : c.name + "\n" + shown;
            // Kept inside the view: to the right of a customer on the left
            // half of the room, to the left on the right half.
            bool left = c.go.transform.localPosition.x < 6f;
            // Version 2: B's label under its feet, A's over its head, so two
            // customers side by side never cover each other's words.
            bool below = Multi && c.id == "consumer-b";
            c.tag.anchor = below ? (left ? TextAnchor.UpperLeft : TextAnchor.UpperRight) : (left ? TextAnchor.LowerLeft : TextAnchor.LowerRight);
            c.tag.transform.localPosition = new Vector3(left ? -0.4f : 0.4f, below ? -0.1f : 2.05f, 0f);
            if (status == null) return;
            if (Multi) status.text = StatusLines();
            else if (c.body.color.a > 0.9f) status.text = (c.id == "consumer-a" ? "A" : "B") + "：" + (label == "" ? c.target.Phase : label);
        }
    }
}
