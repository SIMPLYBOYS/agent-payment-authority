// TownView (T2, docs/commercial-town.md): the town around the shop. Its
// ground, props, walking passers-by and the venues' outsides are
// Resources/Town/town.json. The room ShopView shows is a place of its own,
// away from the map (Aaron 10/27, option A: enter a shop through a
// transition, leave the same way). Two views: the town (a click on a venue
// is reported, the page decides; customers A and B, while they have a task,
// stand at their venue's door) and a venue (the room as before). Between
// them a circle closes on the door being entered or left and opens on the
// other side. The camera fills the canvas's uncovered part: the town with
// its frame, a venue with its room, at a whole number of screen pixels per
// art pixel when one fits within a tenth, else the exact fraction (Aaron
// 10/27, as pixel-office; the page renders at the screen's own density, so
// a fraction is fine-grained). It shows; it never decides.
using System.Collections.Generic;
using System.Linq;
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
        public int w, h;               // the map: x from x0 to w, y from 0 to h
        public float x0;               // its west end (Aaron 11/8: a hospital west of the first street; 0 before)
        public Vector2 focus, frame;   // the town view: its centre, and the box it must show whole
        public Piece[] ground = new Piece[0], props = new Piece[0];
        public Walker[] walkers = new Walker[0];
        public Inside[] inside = new Inside[0];
        public Venue[] venues = new Venue[0];
        public Traffic traffic;
        public Area[] walk = new Area[0]; // the game (Aaron 10/29): where the person's customer may stand, by its feet: the pavement, the crossing
        public Vector2 start;             // and where they start
        // Aaron 11/8: a beach reached by the subway. apart: the map's other parts (x from, x to), east of w, no road
        // between: the view keeps inside the part it looks at (the town is x0 to w). subway: its stairs in pairs (0 and
        // 1, 2 and 3): the walked customer goes down one's mouth and comes up on the other's step.
        public Vector2[] apart = new Vector2[0];
        public Stairs[] subway = new Stairs[0];
        [System.Serializable] public class Stairs { public Vector2 at, step; }
        // A train at a platform (Aaron 11/8): its cars west to east from x, bottom y, each an outside over the same car
        // cut away (open: LimeZu's cut-away cars, their door on the platform's side); the outside fades only while the
        // walked customer is near its door or in it (Aaron 11/9: so one sees where to get in). inside: where one stands
        // in it (its aisle, a walk area too); to: the train it rides to (the other station's); from: the west tunnel's
        // mouth, whence it comes in (TownView.Ride).
        public Train[] trains = new Train[0];
        // A subway gate (docs/commercial-transit.md, Aaron 11/9): its line (x) across its way in (y from y0 to y1), crossed
        // east going in (paid while the page has the gates on) and west going out (free); its station, as the page names
        // it; its arm's two looks, up (open) and down across the way (shut).
        public Gate[] gates = new Gate[0];
        // The street's ATM (docs/commercial-atm.md, Aaron 11/9): spot, where the walked customer stands at it (on the
        // walk), reported once until it steps off; note, the height of the words shown over it. Its look is a prop.
        public Atm[] atms = new Atm[0];
        [System.Serializable] public class Atm { public string id; public Area spot = new Area(); public float note; }
        [System.Serializable] public class Gate { public string station; public float line, y0, y1; public Piece open = new Piece(), shut = new Piece(); }
        // name, en: its station's name, shown as it comes in (Aaron 11/9).
        [System.Serializable] public class Train { public float x, y, from; public string name, en; public string[] cars = new string[0], open = new string[0]; public Area inside = new Area(); public int to = -1; }

        // frames: the file is that many frames side by side (Aaron 10/28: a fountain that plays, doors that open).
        // times: seconds each frame shows, over and over (Aaron 11/9: a billboard's ad holds, its louvres flick); none: 8 a second.
        // over (a prop): the y of the building it stands on, its roof or its wall (Aaron 11/9: the homes as fine as LimeZu's
        // sample): drawn just in front of that building, in the order listed, not by its own y; 0: it stands on the ground.
        [System.Serializable] public class Piece { public string key; public float x, y, w, h; public int frames; public float[] times; public float over; }
        // A venue: the room (layout) it holds, its number and name on the
        // map, its outside, and its door (from its origin): where the
        // transition closes and A and B stand.
        // doorway: its door's frames (closed, opening, open at the middle, closing), over the door in its outside.
        // pill: its name's height over the origin; 0: just above its top (a building taller than the view, the post
        // office's or the hotel's, has it over its front).
        [System.Serializable] public class Venue { public string layout, number, name; public float x, y, pill; public Vector2 door; public Piece[] exterior = new Piece[0]; public Piece doorway; }
        // A passer-by (illustrative): a premade character's walk row (24 frames of 32 x
        // 64: right, up, left, down), along a path of horizontal and vertical
        // legs; a path that ends where it starts loops, any other goes back.
        // A path of one point is a pose there: its piece's frames (sitting,
        // reading, a clerk standing) play in a loop.
        // wait: seconds a walker stands at each point of its path, facing up (in a shop: at a shelf; interiors v2); none: on.
        // over: the y of the building it walks on (Aaron 11/9: the resident at the laundry on a roof): drawn over that
        // building and the things on it, not by its own y; 0: on the ground.
        // talk (Aaron 11/14): a guide the walked customer may talk to, by its id (the page has its lines); "" none.
        [System.Serializable] public class Walker { public string key; public float speed = 1.2f; public Vector2[] path = new Vector2[0]; public float[] wait = new float[0]; public float over; public string talk; }
        // The people in a shop (Aaron 10/27: a clerk, other customers; illustrative,
        // never A or B), in its room's coordinates, shown with that room only.
        [System.Serializable] public class Inside { public string layout; public Walker[] people = new Walker[0]; }
        // Traffic (Aaron 10/28: the signals change, the cars drive): the crossing's x from..to over the road's
        // y bottom..top; the lights' seconds green, amber, red and a prop key for each (the props of the first are
        // the signals); cars along a lane at y, at speed (tiles a second, + east, - west), wrapping round off the map.
        // junction (town v2 V5, docs/town-v2.md): the crossroads with its two crossings, x over the north-south road,
        // y from the south crossing to the north one. The east-west cars stop before it as at the crossing; the
        // north-south cars go over it on their red; a walker crosses the north-south road there on their green.
        [System.Serializable] public class Traffic { public float from, to, bottom, top; public float[] cycle = new float[0]; public string[] lights = new string[0]; public Car[] cars = new Car[0]; public Area junction = new Area(); public Visit visit = new Visit(); }
        // frames: its sheet's driving frames (the wheels turn, Aaron 10/28), played as it goes. vertical: along the
        // north-south road, its lane at x, from y, at speed (+ north, - south). passes: it drives by the gas station
        // (Aaron 11/6: buses and trucks among the cars; they have no nose pieces to pull in with).
        // stopAt (Aaron 11/6: the buses call at their stops): it halts there, its centre at that x (y for a vertical one),
        // stopFor seconds, once each time round; doors: its pieces for it (doorFrames of them: opening, open, shutting).
        [System.Serializable] public class Car { public string key, doors; public float x, y, speed, stopAt, stopFor; public int frames, doorFrames; public bool vertical, passes; }
        // Visit (town v2 V5, Aaron 11/2: the road's cars call at the gas station): of the cars of the lane at y lane
        // going west, every so many that come by x from while no one is in pull in, nose up, to the stop at x at,
        // y; stay; and pull out, nose down, back into the lane at x to once it is clear. The noses are each car
        // key's "-tilt-up" and "-tilt-down" pieces (drawn facing east: flipped). attendant (a premade's walk row, Aaron
        // 11/2: someone fills it up): waits at post facing the street; when a car stops, walks to its side, fills it
        // for fill seconds facing it, and walks back; the car leaves once they are done. None: it stays stay seconds.
        [System.Serializable] public class Visit { public float lane, from, at, y, to, stay, fill = 4f; public int every = 2; public string attendant; public Vector2 post, side; }
        [System.Serializable] public class Area { public float x, y, w, h; public bool Holds(Vector2 p) => p.x >= x && p.x <= x + w && p.y >= y && p.y <= y + h; }
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
        // What a venue view shows of a room of size (ShopView.Layout): the pavement's 2.5 tiles on its left, half a
        // tile on its right; its art from y -0.5 (a 12 x 10 room: ShopBuilder's old camera, 15 x 10 centred at 5, 4.5).
        public static Vector2 RoomView(Vector2 size) => new Vector2(size.x + 3f, size.y);
        public static Vector2 RoomCentre(Vector2 size) => new Vector2((size.x - 2f) / 2f, (size.y - 1f) / 2f);
        public static readonly Vector2 Interior = new Vector2(200f, 0f); // where the room stands, away from the map (98 wide, its ground to x 114; V3)
        public static readonly Vector2 RoomDoor = new Vector2(1.5f, 1.2f); // the room's door (every layout's), from the room's origin
        public const float Half = 0.35f; // seconds for the circle to close, and again to open

        Town town;
        Camera cam;
        readonly Dictionary<string, (Town.Venue v, float top, float right)> venues = new Dictionary<string, (Town.Venue, float, float)>();
        readonly List<(SpriteRenderer plate, TextMesh text)> plates = new List<(SpriteRenderer, TextMesh)>();
        public bool HasTown => town != null && System.Array.Exists(art, t => t != null); // a build without the pack has no town (the room keeps its old camera)
        public string Mode { get; private set; } = "venue"; // venue | town: what the page asked for
        string shown = "venue";                               // what the camera shows (they differ during a transition)
        public System.Action<string> Clicked; // town view: a click on a venue, with its layout; ToTown: walked out of a room
        public const string ToTown = "town";  // Clicked's name for the town: the walked customer stepped out onto the pavement
        Rect safe;                            // the part of the canvas the page leaves uncovered, in Screen pixels from its top left; empty: all of it
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
            var onTop = new Dictionary<float, int>(); // a building's y -> the things on it so far
            foreach (var p in town.props)
            {
                int order = Order(p.y);
                if (p.over != 0f) { onTop.TryGetValue(p.over, out int n); onTop[p.over] = n + 1; order = Order(p.over) + 1 + n; }
                var r = Piece(transform, p, order, new Vector2(0.5f, 0f));
                if (r != null && p.frames > 1) loops.Add((r, Frames(p.key, p.frames, new Vector2(0.5f, 0f)), p.times));
            }
            trains.Clear();
            foreach (var tr in town.trains)
            {
                var s = new TrainState { t = tr, root = new GameObject("train").transform };
                s.root.SetParent(transform, false);
                float x = tr.x;
                var outs = new List<SpriteRenderer>();
                for (int i = 0; i < tr.cars.Length; i++)
                {
                    float w = Width(tr.cars[i]);
                    if (i < tr.open.Length) Piece(s.root, new Town.Piece { key = tr.open[i], x = x + w / 2f, y = tr.y }, Order(tr.y) - 2, new Vector2(0.5f, 0f));
                    var r = Piece(s.root, new Town.Piece { key = tr.cars[i], x = x + w / 2f, y = tr.y }, Order(tr.y) - 1, new Vector2(0.5f, 0f));
                    if (r != null) outs.Add(r);
                    x += w;
                }
                s.outside = outs.ToArray();
                s.length = x - tr.x;
                trains.Add(s);
            }
            gates.Clear();
            foreach (var g in town.gates)
            {
                var s = new GateState { g = g, open = Piece(transform, g.open, Order(g.open.y), new Vector2(0.5f, 0f)), shut = Piece(transform, g.shut, Order(g.shut.y), new Vector2(0.5f, 0f)) };
                s.root = new GameObject("gate-" + g.station).transform;
                s.root.SetParent(transform, false);
                s.root.localPosition = new Vector2(g.line + 0.9f, g.y1 + 1.9f); // over its machine
                s.note = Words(s.root, "gate-note", Vector2.zero, 0.085f, Color.white, Labels + 20);
                s.plate = Plate(s.root, s.note, Rounded(), GateWait, Labels + 20);
                s.root.gameObject.SetActive(false);
                gates.Add(s);
                Look(s);
            }
            atms.Clear();
            foreach (var a in town.atms)
            {
                var s = new AtmState { a = a, root = new GameObject("atm-" + a.id).transform };
                s.root.SetParent(transform, false);
                s.root.localPosition = new Vector2(a.spot.x + a.spot.w / 2f, a.note);
                s.note = Words(s.root, "atm-note", Vector2.zero, 0.085f, Color.white, Labels + 20);
                s.plate = Plate(s.root, s.note, Rounded(), GateWait, Labels + 20);
                s.root.gameObject.SetActive(false);
                atms.Add(s);
            }
            foreach (var v in town.venues)
            {
                var root = new GameObject("venue-" + v.layout).transform;
                root.SetParent(transform, false);
                root.localPosition = new Vector2(v.x, v.y);
                // By depth, as everything on the street: a building by its bottom row, its pieces in file order within it.
                float bottom = float.MaxValue;
                foreach (var e in v.exterior) bottom = Mathf.Min(bottom, e.y);
                for (int i = 0; i < v.exterior.Length; i++) Piece(root, v.exterior[i], Order(v.y + bottom) + i, Vector2.zero);
                if (v.doorway != null && !string.IsNullOrEmpty(v.doorway.key))
                {
                    var r = Piece(root, v.doorway, Order(v.y + bottom) + v.exterior.Length, Vector2.zero);
                    if (r != null) doors[v.layout] = new DoorState { r = r, frames = Frames(v.doorway.key, Mathf.Max(1, v.doorway.frames), Vector2.zero) };
                }
                // The building's top, from the data (a repeated piece's h, else its file's height): the pill sits just above it.
                float top = 0f;
                foreach (var e in v.exterior) top = Mathf.Max(top, e.y + (e.h > 0f ? e.h : Height(e.key)));
                float right = 0f; // its width, the same way (the mart 19 tiles with its side AC, the cafe 12, the data lab 12.7 with its sign)
                foreach (var e in v.exterior) right = Mathf.Max(right, e.x + (e.w > 0f ? e.w : Width(e.key)));
                venues[v.layout] = (v, top, right);
                Pill(root, v, top, right);
            }
            foreach (var w in town.walkers) MakeWalker(w, transform, false);
            MakeTraffic();
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
            Piece(transform, new Town.Piece { key = "x-pave", x = Interior.x - 4f, y = Interior.y, w = 4f, h = 13f }, -1050, Vector2.zero); // the tallest room's height
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

        // Sprite: a piece's whole file (its first frame), anchored at pivot; FullRect so it can repeat.
        Sprite Sprite(string key, Vector2 pivot, int frames = 1)
        {
            var tex = Tex(key);
            return tex == null ? null : UnityEngine.Sprite.Create(tex, new Rect(0, 0, tex.width / Mathf.Max(1, frames), tex.height), pivot, PPU, 0, SpriteMeshType.FullRect);
        }

        Sprite[] Frames(string key, int n, Vector2 pivot)
        {
            var tex = Tex(key);
            var f = new Sprite[n];
            for (int i = 0; i < n; i++) f[i] = UnityEngine.Sprite.Create(tex, new Rect(i * tex.width / n, 0, tex.width / n, tex.height), pivot, PPU);
            return f;
        }

        // ---- what plays: a prop's frames in a loop; a door opening for whoever goes in or comes out ----
        readonly List<(SpriteRenderer r, Sprite[] frames, float[] times)> loops = new List<(SpriteRenderer, Sprite[], float[])>();

        // The frame of n a loop shows at time t: 8 a second, or each for its times (cycled over the frames).
        public static int FrameAt(float t, int n, float[] times)
        {
            if (times == null || times.Length == 0) return (int)(t * 8f) % n;
            float total = 0f;
            for (int i = 0; i < n; i++) total += times[i % times.Length];
            t = Mathf.Repeat(t, total);
            for (int i = 0; i < n; i++) if ((t -= times[i % times.Length]) < 0f) return i;
            return n - 1;
        }
        class DoorState { public SpriteRenderer r; public Sprite[] frames; public float open; }
        readonly Dictionary<string, DoorState> doors = new Dictionary<string, DoorState>();
        public int DoorFrame(string layout) => doors.TryGetValue(layout, out var d) ? System.Array.IndexOf(d.frames, d.r.sprite) : -1; // tests
        const float DoorTime = 0.4f; // to open, or to close

        // Opens: someone goes in or comes out there: between the street and the door, or just in, or about to come out.
        bool Opens(string layout)
        {
            if (Player != "" && shown == "town" && venues.TryGetValue(layout, out var e) && (PlayerAt - DoorOf(e.v)).magnitude < 1.2f) return true; // the person's customer on its step
            if (Busy(layout)) return false; // no one does while A or B stand there
            foreach (var s in walkers)
            {
                if (s.room || s.doorOf == null) continue;
                if (s.inside >= 0f) { if (s.doorOf[s.leg] == layout && (s.inside < Fade + DoorTime || s.inside > Stay - Fade - DoorTime)) return true; continue; }
                var p = (Vector2)s.r.transform.localPosition;
                int nx = s.leg + s.dir;
                if (nx >= 0 && nx < s.doorOf.Length && s.doorOf[nx] == layout && p != s.w.path[s.leg]) return true; // on the way up
                if (s.doorOf[s.leg] == layout) return true; // just out, on the way down
            }
            return false;
        }

        void Play(float dt)
        {
            // timed loops start apart by where they stand, so two billboards don't turn together
            foreach (var (r, f, ts) in loops) r.sprite = f[FrameAt(Time.time + (ts != null && ts.Length > 0 ? r.transform.position.x * 0.1f : 0f), f.Length, ts)];
            foreach (var kv in doors)
            {
                var d = kv.Value;
                d.open = Mathf.MoveTowards(d.open, Opens(kv.Key) ? 1f : 0f, dt / DoorTime);
                d.r.sprite = d.frames[Mathf.RoundToInt(d.open * (d.frames.Length / 2))]; // the opening half, backwards to close
            }
        }

        float Height(string key) => Tex(key) != null ? Tex(key).height / (float)PPU : 0f;
        float Width(string key) => Tex(key) != null ? Tex(key).width / (float)PPU : 0f;

        SpriteRenderer Piece(Transform parent, Town.Piece p, int order, Vector2 pivot)
        {
            var s = Sprite(p.key, pivot, p.frames);
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
        void Pill(Transform root, Town.Venue v, float top, float right)
        {
            var text = Words(root, "pill", new Vector2(right / 2f, v.pill > 0f ? v.pill : top + 0.5f), 0.085f, Ink, Labels);
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
        // doorOf: for each point of its path, the venue whose door it is (null for the street); inside: seconds in that shop, or -1.
        class WalkerState { public Town.Walker w; public SpriteRenderer r; public Sprite[] frames; public bool room; public int leg; public int dir = 1; public float tick; public int frame; public string[] doorOf; public float inside = -1f; public float rest; }
        readonly List<WalkerState> walkers = new List<WalkerState>();
        readonly Dictionary<string, GameObject> insides = new Dictionary<string, GameObject>();
        public IEnumerable<Vector2> WalkerPositions { get { foreach (var s in walkers) if (!s.room) yield return s.r.transform.localPosition; } } // tests
        // The people shown in the room now, in its coordinates (tests).
        public IEnumerable<(string key, Vector2 at)> PeopleInside { get { foreach (var s in walkers) if (s.room && s.r.gameObject.activeInHierarchy) yield return (s.w.key, s.r.transform.localPosition); } }
        // Tests: a passer-by of their own, and the last one of a key: the shop it is in (null on the street), how much of it shows, where.
        public void AddWalker(Town.Walker w) => MakeWalker(w, transform, false);
        public (string shop, float shown, Vector2 at) Passer(string key)
        {
            (string, float, Vector2) found = (null, 0f, Vector2.zero);
            foreach (var s in walkers) if (!s.room && s.w.key == key) found = (s.inside >= 0f ? s.doorOf[s.leg] : null, s.r.color.a, s.r.transform.localPosition);
            return found;
        }

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
            var s = new WalkerState { w = w, r = r, frames = frames, room = room, doorOf = new string[w.path.Length] };
            // A point at a venue's door is a way in (Aaron 10/28: people go in and out of the shops).
            for (int i = 0; i < w.path.Length && !room; i++)
                foreach (var e in venues.Values)
                    if ((w.path[i] - (new Vector2(e.v.x, e.v.y) + e.v.door)).sqrMagnitude < 0.01f) s.doorOf[i] = e.v.layout;
            s.r.sortingOrder = Depth(s, w.path[0].y);
            walkers.Add(s);
            if (room || string.IsNullOrEmpty(w.talk)) return;
            // A guide (Aaron 11/14): pixel-office's dots bubble over its head while the walked customer is near (Talkers).
            var t = new TalkerState { s = s, dots = new GameObject("talk-" + w.talk).AddComponent<SpriteRenderer>() };
            t.dots.transform.SetParent(r.transform, false);
            t.dots.transform.localPosition = new Vector2(0.1f, 2.15f);
            t.dots.sprite = Resources.Load<Sprite>("UI/bubble");
            t.dots.sortingOrder = Labels + 20;
            t.dots.gameObject.SetActive(false);
            talkers.Add(t);
        }

        // ---- talking to a guide (Aaron 11/14: near a person, a key starts the talk) ----
        // The walked customer within TalkReach of a guide (the nearest) is reported (Near, with its talk id), and ""
        // once it is near none, or no longer walked in the town: each change once. The page shows the lines and takes
        // the key; the town only shows the bubble over the guide it is near.
        public System.Action<string> Near;
        public const float TalkReach = 1.5f;
        class TalkerState { public WalkerState s; public SpriteRenderer dots; }
        readonly List<TalkerState> talkers = new List<TalkerState>();
        string nearTalk = "";
        public string NearBy => nearTalk; // tests
        public Vector2 GuideAt(string id) { var t = talkers.Find(x => x.s.w.talk == id); return t != null ? (Vector2)t.s.r.transform.localPosition : Vector2.zero; } // tests

        void Talkers(Vector2 q)
        {
            TalkerState best = null;
            foreach (var t in talkers)
            {
                float d = ((Vector2)t.s.r.transform.localPosition - q).magnitude;
                if (d < TalkReach && (best == null || d < ((Vector2)best.s.r.transform.localPosition - q).magnitude)) best = t;
            }
            SetNear(best);
        }

        void SetNear(TalkerState best)
        {
            string id = best != null ? best.s.w.talk : "";
            if (id == nearTalk) return;
            foreach (var t in talkers) t.dots.gameObject.SetActive(t == best && sayAt != "guide:" + id);
            nearTalk = id;
            Near?.Invoke(id);
        }

        // Depth: on the street, the town's scale; in a room, the room's own (ShopView: a prop at y draws at
        // -(y * 10)), just in front of a prop on the same row; always under A and B (50).
        static int Depth(WalkerState s, float y) => s.room ? -(int)(y * 10f) + 1 : s.w.over != 0f ? Order(s.w.over) + 10 : Order(y);

        const float Stay = 5f, Fade = 0.35f; // seconds in a shop; to go in or come out
        // Busy: A or B stands at that venue's door (they transact there; PlaceStands' rule, this frame): no one else goes in or comes out over them.
        bool Busy(string layout)
        {
            if (layout == null || shop == null || shop.LayoutKey != layout) return false;
            foreach (var k in stands.Keys) if (k != Player && shop.Transacting(k)) return true; // the person's customer walks; it never stands there
            return false;
        }

        void Walk(WalkerState s, float dt)
        {
            var path = s.w.path;
            if (s.inside >= 0f)
            {
                // In a shop: fades out facing its door, stays, and fades in facing the street, once the door is clear.
                s.inside += dt;
                if (Busy(s.doorOf[s.leg])) s.inside = Mathf.Clamp(s.inside, Fade, Stay - Fade - DoorTime); // out of sight until they have gone; then the door opens first
                s.r.color = new Color(1f, 1f, 1f, Mathf.Clamp01(Mathf.Max(Fade - s.inside, s.inside - (Stay - Fade)) / Fade));
                int f = (s.inside < Stay / 2f ? 1 : 3) * 6; // up, down
                if (f < s.frames.Length) s.r.sprite = s.frames[f];
                if (s.inside < Stay) return;
                s.inside = -1f;
                s.r.color = Color.white;
            }
            if (s.rest > 0f)
            {
                s.rest -= dt; // at a point it waits at: standing, facing up (a walk sheet's up frames start at 6)
                if (s.frames.Length > 6) s.r.sprite = s.frames[6];
                return;
            }
            if (path.Length == 1)
            {
                // A pose: its frames in a loop, in place.
                s.tick += dt;
                if (s.tick > 0.18f) { s.tick = 0f; s.frame = (s.frame + 1) % s.frames.Length; s.r.sprite = s.frames[s.frame]; }
                return;
            }
            bool loop = path[0] == path[path.Length - 1];
            var p = (Vector2)s.r.transform.localPosition;
            // Caught between the street and a door (going in, or just out) as A or B arrive there: in at once, out of their way.
            int nx = s.leg + s.dir;
            if (p != path[s.leg] && (Busy(s.doorOf[nx]) || Busy(s.doorOf[s.leg])))
            {
                if (s.doorOf[nx] != null) s.leg = nx;
                s.inside = Fade;
                s.r.color = Color.clear;
                return;
            }
            float go = s.w.speed * dt;
            Vector2 d = Vector2.zero;
            bool wait = false;
            for (int guard = 0; go > 0f && guard < 8; guard++)
            {
                int next = s.leg + s.dir;
                // At the kerb of the crossing: over only on red, with the time to reach the other side.
                if (p == path[s.leg] && Crosses(path[s.leg], path[next]) && !MayCross((path[next] - p).magnitude / Mathf.Max(0.1f, s.w.speed))) { wait = true; d = path[next] - p; break; }
                if (p == path[s.leg] && CrossesSide(path[s.leg], path[next]) && !MaySide((path[next] - p).magnitude / Mathf.Max(0.1f, s.w.speed))) { wait = true; d = path[next] - p; break; }
                bool skip = s.doorOf[next] != null && Busy(s.doorOf[next]); // A or B at that door: on past it, from where it is
                if (!skip)
                {
                    var to = path[next];
                    d = to - p;
                    if (d.magnitude > go) { p += d.normalized * go; go = 0f; break; }
                    go -= d.magnitude;
                    p = to;
                }
                s.leg = next;
                bool enter = !skip && s.doorOf[s.leg] != null;
                float rest = !skip && s.leg < s.w.wait.Length ? s.w.wait[s.leg] : 0f;
                if (loop && s.leg == path.Length - 1) { s.leg = 0; if (!skip) p = path[0]; }
                else if (!loop && (s.leg == path.Length - 1 || s.leg == 0)) s.dir = -s.dir;
                if (enter) { s.inside = 0f; break; }
                if (rest > 0f) { s.rest = rest; break; }
            }
            s.r.transform.localPosition = p;
            s.r.sortingOrder = Depth(s, p.y);
            if (d.sqrMagnitude > 1e-6f)
            {
                int facing = Mathf.Abs(d.x) >= Mathf.Abs(d.y) ? (d.x > 0 ? 0 : 2) : (d.y > 0 ? 1 : 3); // right, up, left, down
                if (wait) { if (facing * 6 < s.frames.Length) s.r.sprite = s.frames[facing * 6]; return; } // stands, facing the road
                s.tick += dt;
                if (s.tick > 0.1f) { s.tick = 0f; s.frame = (s.frame + 1) % 6; }
                int i = facing * 6 + s.frame;
                if (i < s.frames.Length) s.r.sprite = s.frames[i];
            }
        }

        // ---- traffic ----
        class CarState { public Town.Car c; public SpriteRenderer r; public Sprite[] frames; public float x, half, rolled; public int visit; public float vy, wait; public bool called, served; public Sprite noseUp, noseDown; public Sprite[] doors; public float halt; public bool halted; } // halt: seconds left at its stop; halted: this time round. x: its centre along its road (y for a vertical one); visit: 0 on the road, 1 pulling in, 2 at the stop, 3 pulling out
        readonly List<CarState> cars = new List<CarState>();
        readonly List<SpriteRenderer> signals = new List<SpriteRenderer>();
        Sprite[] lightSprites = new Sprite[0];
        static readonly string[] LightNames = { "green", "amber", "red" };
        const float Gap = 0.8f, Line = 0.3f, WheelFrames = 4f; // between cars; the stop line before the crossing; driving frames a tile
        float roadFrom, roadTo; // where the ground ends: a car goes round there, a half car past it (review of 54435f5: a wide view shows -12.7..77)
        // A lane is a ring (Aaron 11/6: buses among the cars): round past the longest one's half, every car jumps
        // the same length, so the gaps hold; and the one just round is ahead of the one about to go.
        float edgeX, edgeY;
        float RingLength(bool vertical) => vertical ? roadTop - roadBottom + 2f * edgeY : roadTo - roadFrom + 2f * edgeX;
        public string Light => LightNames[LightState(out _)]; // tests
        readonly List<SpriteRenderer> crossSignals = new List<SpriteRenderer>();
        const float CrossAmber = 2f;
        // CrossState: the north-south cars' light: green on the east-west red but for its last seconds, amber then; else red.
        int CrossState() { int st = LightState(out var left); return st != 2 ? 2 : left > CrossAmber ? 0 : 1; }
        public string CrossLight => LightNames[CrossState()]; // tests
        public IEnumerable<(float x, float half, float y, float speed, int frame)> Cars { get { foreach (var c in cars) if (!c.c.vertical && c.visit == 0) yield return (c.x, c.half, c.c.y, c.c.speed, Wheel(c)); } } // tests: the east-west ones on the road, frame its wheels
        public IEnumerable<(string key, float x, float halt, int door)> Buses { get { foreach (var c in cars) if (c.c.stopFor > 0f) yield return (c.c.key, c.x, c.halt, c.doors != null ? System.Array.IndexOf(c.doors, c.r.sprite) : -1); } } // tests
        static int Wheel(CarState c) => (int)(c.rolled * WheelFrames) % c.frames.Length;
        public IEnumerable<(string key, Vector2 at, int visit)> Visitors { get { foreach (var c in cars) if (c.visit != 0) yield return (c.c.key, new Vector2(c.x, c.vy), c.visit); } } // tests
        int arrivals; // lane cars come by the gas station: every so many call
        // The attendant: 0 at the post, 1 on the way to the car, 2 filling it, 3 on the way back.
        SpriteRenderer att;
        Sprite[] attFrames;
        int attPhase, attFrame;
        float attTick, attLeft;
        Vector2 attAt;
        public (Vector2 at, int phase) Attendant => (attAt, attPhase); // tests

        void MakeAttendant(Town.Visit v)
        {
            var tex = string.IsNullOrEmpty(v.attendant) ? null : Tex(v.attendant);
            if (tex == null) return;
            attFrames = new Sprite[tex.width / 32];
            for (int i = 0; i < attFrames.Length; i++) attFrames[i] = UnityEngine.Sprite.Create(tex, new Rect(i * 32, 0, 32, tex.height), new Vector2(0.5f, 0f), PPU);
            att = new GameObject(v.attendant).AddComponent<SpriteRenderer>();
            att.transform.SetParent(transform, false);
            attAt = v.post;
            Tend(0f);
        }

        // Tend: the attendant's walk: from the post along to the car's side (over, then across) and back.
        void Tend(float dt)
        {
            var v = town.traffic.visit;
            int facing = 3; // right, up, left, down: at the post, facing the street
            if (attPhase == 1 || attPhase == 3)
            {
                var corner = new Vector2(v.post.x, v.side.y);
                var to = attPhase == 1 ? (attAt.y != v.side.y ? corner : v.side) : (attAt.x != v.post.x ? corner : v.post);
                var d = to - attAt;
                float go = 1.4f * dt;
                facing = Mathf.Abs(d.x) >= Mathf.Abs(d.y) ? (d.x > 0f ? 0 : 2) : (d.y > 0f ? 1 : 3);
                attAt = d.magnitude <= go ? to : attAt + d.normalized * go;
                attTick += dt;
                if (attTick > 0.1f) { attTick = 0f; attFrame = (attFrame + 1) % 6; }
                if (attAt == v.side && attPhase == 1) { attPhase = 2; attLeft = v.fill; }
                if (attAt == v.post && attPhase == 3) attPhase = 0;
            }
            if (attPhase == 2) { facing = 2; attLeft -= dt; if (attLeft <= 0f) attPhase = 3; } // facing the car, filling it
            bool walking = attPhase == 1 || attPhase == 3;
            att.sprite = attFrames[Mathf.Min(attFrames.Length - 1, facing * 6 + (walking ? attFrame : 0))];
            att.transform.localPosition = attAt;
            att.sortingOrder = Order(attAt.y);
        }
        public IEnumerable<(float x, float y, float half, float speed)> CrossCars { get { foreach (var c in cars) if (c.c.vertical) yield return (c.c.x, c.x, c.half, c.c.speed); } } // tests: the north-south ones, y their centre
        float roadBottom, roadTop;

        void MakeTraffic()
        {
            var t = town.traffic;
            if (t == null || t.cycle.Length != 3 || t.lights.Length != 3) return;
            lightSprites = new Sprite[3];
            for (int i = 0; i < 3; i++) lightSprites[i] = Sprite(t.lights[i], new Vector2(0.5f, 0f));
            foreach (Transform c in transform)
                if (c.name == t.lights[0] && c.TryGetComponent<SpriteRenderer>(out var r))
                    (t.junction.w > 0f && c.localPosition.x > t.junction.x - 1.5f && c.localPosition.x < t.junction.x + t.junction.w + 1.5f ? crossSignals : signals).Add(r); // at the junction: the north-south road's
            roadFrom = float.MaxValue; roadTo = float.MinValue;
            roadBottom = float.MaxValue; roadTop = float.MinValue;
            foreach (var g in town.ground)
            {
                if (g.x > town.w) continue; // the beach's ground (apart): no road there
                roadFrom = Mathf.Min(roadFrom, g.x); roadTo = Mathf.Max(roadTo, g.x + g.w); roadBottom = Mathf.Min(roadBottom, g.y); roadTop = Mathf.Max(roadTop, g.y + g.h);
            }
            if (t.visit.from > 0f) MakeAttendant(t.visit);
            foreach (var car in t.cars)
            {
                var r = Piece(transform, new Town.Piece { key = car.key, x = car.x, y = car.y, frames = car.frames }, Order(car.y), new Vector2(0.5f, 0f));
                if (r == null) continue;
                var size = r.sprite.bounds.size;
                cars.Add(new CarState { c = car, r = r, half = (car.vertical ? size.y : size.x) / 2f, frames = Frames(car.key, Mathf.Max(1, car.frames), new Vector2(0.5f, 0f)), noseUp = Sprite(car.key + "-tilt-up", new Vector2(0.5f, 0f)), noseDown = Sprite(car.key + "-tilt-down", new Vector2(0.5f, 0f)),
                    doors = string.IsNullOrEmpty(car.doors) ? null : Frames(car.doors, Mathf.Max(1, car.doorFrames), new Vector2(0.5f, 0f)) });
                cars[cars.Count - 1].x = car.vertical ? car.y + cars[cars.Count - 1].half : car.x;
                if (car.vertical) edgeY = Mathf.Max(edgeY, cars[cars.Count - 1].half); else edgeX = Mathf.Max(edgeX, cars[cars.Count - 1].half);
            }
        }

        // LightState: 0 green, 1 amber, 2 red (for the cars; the crossing's walkers go on red), and the seconds it has left.
        int LightState(out float left)
        {
            left = 0f;
            var c = town != null && town.traffic != null ? town.traffic.cycle : null;
            if (c == null || c.Length != 3) return 0;
            float t = Time.time % (c[0] + c[1] + c[2]);
            for (int i = 0; i < 3; t -= c[i], i++) if (t < c[i]) { left = c[i] - t; return i; }
            return 0;
        }

        bool Crosses(Vector2 a, Vector2 b)
        {
            var t = town != null ? town.traffic : null;
            return t != null && t.cycle.Length == 3 && a.x == b.x && a.x >= t.from && a.x <= t.to && Mathf.Min(a.y, b.y) <= t.bottom && Mathf.Max(a.y, b.y) >= t.top;
        }

        bool HasJunction => town != null && town.traffic != null && town.traffic.junction.w > 0f;
        // Over the north-south road at the junction: a level leg from one side of it to the other.
        bool CrossesSide(Vector2 a, Vector2 b)
        {
            if (!HasJunction || a.y != b.y) return false;
            var j = town.traffic.junction;
            return a.y >= j.y && a.y <= j.y + j.h && Mathf.Min(a.x, b.x) <= j.x && Mathf.Max(a.x, b.x) >= j.x + j.w;
        }

        // MaySide: green for the east-west cars (so the north-south ones wait) with the time to cross left, and none of those on the junction.
        bool MaySide(float seconds)
        {
            if (LightState(out var left) != 0 || left < seconds + 0.5f) return false;
            foreach (var c in cars) if (c.c.vertical && OnJunction(c)) return false;
            return true;
        }

        bool OnJunction(CarState c) { var j = town.traffic.junction; return c.x + c.half > j.y && c.x - c.half < j.y + j.h; } // a vertical one

        // MayCross: red for the cars with the time to cross left, and no car on the crossing.
        bool MayCross(float seconds)
        {
            if (LightState(out var left) != 2 || left < seconds + 0.5f) return false;
            foreach (var c in cars) if (!c.c.vertical && c.x + c.half > town.traffic.from && c.x - c.half < town.traffic.to) return false;
            return true;
        }

        void Drive(float dt)
        {
            int st = LightState(out var left);
            foreach (var r in signals) r.sprite = lightSprites[st];
            foreach (var r in crossSignals) r.sprite = lightSprites[CrossState()];
            if (att != null) Tend(dt);
            if (cars.Count == 0) return;
            var t = town.traffic;
            bool someoneOn = false; // a walker on the crossing: every car waits at the line
            foreach (var w in walkers)
            {
                if (w.room || w.inside >= 0f) continue;
                var p = (Vector2)w.r.transform.localPosition;
                if (p.x > t.from - Line && p.x < t.to + Line && p.y > t.bottom && p.y < t.top) someoneOn = true;
            }
            if (Player != "" && PlayerAt.x > t.from - Line && PlayerAt.x < t.to + Line && PlayerAt.y > t.bottom && PlayerAt.y < t.top) someoneOn = true; // the person's customer crosses when it likes: the cars wait
            var j = t.junction;
            bool junction = HasJunction, crossBusy = false, sideOn = false, eastWestOn = false;
            if (junction)
            {
                foreach (var c in cars) if (c.c.vertical ? OnJunction(c) : c.x + c.half > j.x && c.x - c.half < j.x + j.w) { if (c.c.vertical) crossBusy = true; else eastWestOn = true; }
                foreach (var w in walkers)
                {
                    if (w.room || w.inside >= 0f) continue;
                    var p = (Vector2)w.r.transform.localPosition;
                    if (j.Holds(p) && p.x > j.x && p.x < j.x + j.w) sideOn = true;
                }
                if (Player != "" && j.Holds(PlayerAt)) sideOn = true;
            }
            foreach (var c in cars)
            {
                if (c.visit != 0) { Call(c, dt); continue; }
                if (c.halt > 0f) { Halt(c, dt); continue; }
                float dir = Mathf.Sign(c.c.speed), front = c.x + dir * c.half;
                float room = float.MaxValue; // how far its front may go
                float ahead = float.MaxValue; // to the one ahead in its lane, a gap short of it
                foreach (var o in cars)
                {
                    if (o == c || o.c.vertical != c.c.vertical || (c.c.vertical ? o.c.x != c.c.x : o.c.y != c.c.y)) continue;
                    if ((o.visit == 1 || o.visit == 2) && o.vy > o.c.y + 1.2f) continue; // off the lane, at the gas station; one pulling out is in the way
                    // Ahead by its centre: so one caught alongside (review, Aaron 11/2: a car back from the gas station on another) waits till it is clear.
                    float ox = (o.x - c.x) * dir > 0f ? o.x : o.x + dir * RingLength(c.c.vertical); // behind by its centre: ahead round the ring
                    float back = (ox - dir * o.half - front) * dir;
                    ahead = Mathf.Min(ahead, Mathf.Max(0f, back - Gap));
                }
                if (c.c.vertical)
                {
                    // Over the junction only on the east-west red, with the time to be over it; never onto someone crossing.
                    float toEdge = ((dir > 0 ? j.y - Line : j.y + j.h + Line) - front) * dir;
                    bool clears = (toEdge + j.h + 2f * c.half + Line) / Mathf.Abs(c.c.speed) < left;
                    if (toEdge > -0.01f && (st != 2 || !clears || sideOn || eastWestOn)) room = Mathf.Max(0f, toEdge);
                }
                else
                {
                    // The crossing, or the junction if it comes first: the nearest line ahead.
                    float toLine = ((dir > 0 ? t.from - Line : t.to + Line) - front) * dir, len = t.to - t.from;
                    bool blocked = someoneOn;
                    if (junction)
                    {
                        float toJ = ((dir > 0 ? j.x - Line : j.x + j.w + Line) - front) * dir;
                        if (toJ > -0.01f && (toLine <= -0.01f || toJ < toLine)) { toLine = toJ; len = j.w; blocked = crossBusy; }
                    }
                    // Green or amber: one that would be over before red goes on (a bus on at green's end is not, Aaron 11/6: the
                    // walker waits for it); red, or someone on it: it stops at the line.
                    // And only with room past it (a queue back from a bus at its stop): never left standing on it.
                    float toRed = st == 0 ? left + t.cycle[1] : left, past = toLine + len + 2f * c.half + Line;
                    bool over = past / Mathf.Abs(c.c.speed) < toRed && ahead >= past;
                    if (toLine > -0.01f && (blocked || st == 2 || !over)) room = Mathf.Max(0f, toLine); // -0.01: one stopped on the line is still before it (float error)
                }
                if (c.c.stopFor > 0f && !c.halted) // its stop ahead: it slows to it
                {
                    float toStop = (c.c.stopAt - c.x) * dir;
                    if (toStop > -0.01f) room = Mathf.Min(room, Mathf.Max(0f, toStop));
                }
                room = Mathf.Min(room, ahead);
                float go = Mathf.Abs(c.c.speed) * dt * Mathf.Clamp(room / 1.5f, 0.3f, 1f); // slows to a stop
                go = Mathf.Clamp(go, 0f, Mathf.Max(0f, room));
                float was = c.x;
                c.x += dir * go;
                var v = t.visit;
                if (v.from > 0f && !c.c.vertical && !c.c.passes && dir < 0f && c.c.y == v.lane && was > v.from && c.x <= v.from && ++arrivals % Mathf.Max(1, v.every) == 0 && !cars.Exists(o => o.visit != 0))
                    { c.visit = 1; c.vy = v.lane; }
                // As far as it went: a creep too small to move x (queued behind one stopped, far out on the map) turns no wheel.
                c.rolled = (c.rolled + Mathf.Abs(c.x - was)) % (c.frames.Length / WheelFrames); // round and round: a float that only grows would freeze the wheels after days
                c.r.sprite = c.frames[Wheel(c)]; // the wheels turn as far as it goes: still when it stops
                if (c.c.stopFor > 0f && !c.halted && Mathf.Abs(c.c.stopAt - c.x) < 0.01f) c.halt = c.c.stopFor; // at its stop
                float lo = c.c.vertical ? roadBottom : roadFrom, hi = c.c.vertical ? roadTop : roadTo;
                float edge = c.c.vertical ? edgeY : edgeX;
                if (dir > 0f && c.x > hi + edge) { c.x -= RingLength(c.c.vertical); c.halted = false; }
                if (dir < 0f && c.x < lo - edge) { c.x += RingLength(c.c.vertical); c.halted = false; }
                if (!c.c.vertical) { c.r.transform.localPosition = new Vector2(c.x, c.c.y); continue; }
                c.r.transform.localPosition = new Vector2(c.c.x, c.x - c.half);
                c.r.sortingOrder = Order(c.x - c.half);
            }
        }

        // Halt: a bus at its stop: its doors open in the first second, stay open, and shut in the last (none: it just
        // waits there); then it drives on, its wheels as they were. The cars behind wait for it as for any car.
        void Halt(CarState c, float dt)
        {
            c.halt -= dt;
            if (c.halt <= 0f) { c.halt = 0f; c.halted = true; c.r.sprite = c.frames[Wheel(c)]; return; }
            if (c.doors == null) return;
            int n = c.doors.Length, open = n / 2;
            float since = c.c.stopFor - c.halt;
            int k = since < 1f ? (int)(since * open) : c.halt > 1f ? open : open + (int)((1f - c.halt) * (n - open));
            c.r.sprite = c.doors[Mathf.Clamp(k, 0, n - 1)];
        }

        // Call: a car at the gas station: in, nose up, slowing to the stop; there a while; out, nose down, once
        // the lane is clear behind and ahead; then on its way again. People in its way: it waits.
        void Call(CarState c, float dt)
        {
            var v = town.traffic.visit;
            Sprite nose = null;
            float go = 0f;
            if (c.visit == 2)
            {
                // With an attendant: they come over and fill it; it goes when they have done (and stepped back a moment).
                if (att != null && !c.served)
                {
                    if (!c.called && attPhase == 0) { attPhase = 1; c.called = true; }
                    if (c.called && attPhase == 3) { c.served = true; c.wait = 0.6f; } else c.wait = 1f;
                }
                c.wait -= dt;
                // The lane clear ahead, and behind as far as a car comes while it pulls out (about 4 seconds).
                bool clear = !cars.Exists(o => o != c && !o.c.vertical && o.visit == 0 && o.c.y == v.lane && o.x > v.to - 5f && o.x < v.to + 20f);
                if (c.wait <= 0f && clear) { c.visit = 3; c.called = c.served = false; }
            }
            else
            {
                bool @in = c.visit == 1;
                float end = @in ? v.at : v.to;
                // Where it is on the way at x, and how far along (0..1): in, it reaches the pump's row by three quarters of the way.
                float U(float x) => @in ? (v.from - x) / (v.from - v.at) : (v.at - x) / (v.at - v.to);
                float Y(float x) => @in ? Mathf.SmoothStep(v.lane, v.y, U(x) / 0.75f) : Mathf.SmoothStep(v.y, v.lane, U(x));
                go = Mathf.Abs(c.c.speed) * dt * (@in ? Mathf.Clamp((c.x - end) / 2f, 0.25f, 1f) : 0.6f); // slows to the stop; out gently
                // Someone where it is or a little further along its way: it waits.
                var size = c.r.sprite.bounds.size;
                float ahead = Mathf.Max(end, c.x - 1.2f);
                Rect a = Footprint(new Vector2(c.x, c.vy), size), b = Footprint(new Vector2(ahead, Y(ahead)), size);
                if (InTheWay(Rect.MinMaxRect(Mathf.Min(a.xMin, b.xMin), Mathf.Min(a.yMin, b.yMin), Mathf.Max(a.xMax, b.xMax), Mathf.Max(a.yMax, b.yMax)))) go = 0f;
                c.x = Mathf.Max(end, c.x - go);
                c.vy = Y(c.x);
                float u = U(c.x);
                if (@in ? u > 0.06f && u < 0.68f : u > 0.12f && u < 0.85f) nose = @in ? c.noseUp : c.noseDown;
                if (c.x <= end) { if (@in) { c.visit = 2; c.wait = v.stay; } else { c.visit = 0; c.vy = v.lane; } }
            }
            c.rolled = (c.rolled + go) % (c.frames.Length / WheelFrames);
            c.r.sprite = nose != null ? nose : c.frames[(int)(c.rolled * WheelFrames) % c.frames.Length];
            c.r.flipX = nose != null; // the turn's pieces face east
            c.r.transform.localPosition = new Vector2(c.x, c.vy);
            c.r.sortingOrder = Order(c.visit == 0 ? c.c.y : c.vy);
        }

        // Footprint: a car's body at p as drawn, from its wheels up three quarters of its height, and a little room.
        static Rect Footprint(Vector2 p, Vector2 size) => new Rect(p.x - size.x / 2f - 0.3f, p.y - 0.3f, size.x + 0.6f, size.y * 0.75f + 0.3f);

        // InTheWay: a passer-by, or the person's customer, in that box (Aaron 11/2: the car waits for people).
        bool InTheWay(Rect box)
        {
            foreach (var w in walkers) if (!w.room && w.inside < 0f && box.Contains(w.r.transform.localPosition)) return true;
            return Player != "" && box.Contains(PlayerAt);
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
                bool on = kv.Key == Player || there && shop.Transacting(kv.Key); // done (delivered, denied, any end): gone from the door
                kv.Value.body.enabled = kv.Value.tag.GetComponent<MeshRenderer>().enabled = kv.Value.plate.enabled = on;
                if (!on || kv.Key == Player) continue; // the person's customer is where it walked
                var at = new Vector2(e.v.x, e.v.y) + e.v.door + new Vector2(kv.Key == "consumer-a" ? -0.6f : 0.6f, 0f); // a side each, whoever else is there
                kv.Value.body.transform.localPosition = at;
                kv.Value.body.sortingOrder = Order(at.y) + 1;
            }
        }

        // ---- the game (Aaron 10/29: the game mode, choose a customer and walk it round the town) ----
        // The page names the customer (Play) and steers it with the keys held
        // (Steer); a click on the ground or a building walks it there along
        // the pavement. At a door, walked up into or reached by a click on its
        // building, the venue is reported as a click on it is: the page
        // decides. It crosses the road at the crossing only, when it likes
        // (the cars wait), and walks through the street's props and people.
        public string Player { get; private set; } = ""; // the customer the person walks; "" none
        public const float PlayerSpeed = 3.2f, DoorReach = 0.4f, Cell = 0.5f;
        Sprite[][] playerWalk = new Sprite[0][];
        Sprite[] playerIdle = new Sprite[0]; // right, up, left, down
        Vector2 steer;
        readonly List<Vector2> route = new List<Vector2>();
        string routeTo; // the venue a click walks it to: in on arrival
        string atDoor;  // the door it was last reported at: once, until it steps away
        int pFacing = 3, pFrame;
        float pTick;
        public Vector2 PlayerAt => Player != "" ? (Vector2)stands[Player].body.transform.localPosition : Vector2.zero;
        public int RouteLeft => route.Count; // tests
        static Vector2 DoorOf(Town.Venue v) => new Vector2(v.x, v.y) + v.door;
        public Vector2 DoorAt(string layout) => venues.TryGetValue(layout ?? "", out var e) ? DoorOf(e.v) : Vector2.zero; // tests
        public bool Walkable(Vector2 p) => town != null && System.Array.Exists(town.walk, a => a.Holds(p));

        // Play: the customer to walk (consumer-a or consumer-b), at the start; "" or null: none (A and B
        // stand at their door again while they have a task). False for one this build does not show.
        public bool Play(string consumer)
        {
            consumer = consumer ?? "";
            if (!HasTown || consumer != "" && !stands.ContainsKey(consumer)) return false;
            route.Clear(); routeTo = null; steer = Vector2.zero;
            outside = true; // a customer that stands out on a room's pavement is not reported until it steps in and out
            if (consumer == Player) return true;
            if (phase == 0) { Halt(); hopTo = -1; } // another customer, or none: no ride left half done
            if (Player != "") stands[Player].body.sprite = playerIdle[3]; // back to standing
            Player = consumer;
            atGate = atAtm = null;
            if (Player == "") return true;
            var sheet = Player == "consumer-a" ? shop.customerA : shop.customerB;
            // The premade sheet (ShopView's rows): idle at 64 from the top, walk at 128; right 0, up 6, left 12, down 18.
            Sprite[] Row(int top, int first) { var f = new Sprite[6]; for (int i = 0; i < 6; i++) f[i] = UnityEngine.Sprite.Create(sheet, new Rect((first + i) * 32, sheet.height - top - 64, 32, 64), new Vector2(0.5f, 0f), PPU); return f; }
            playerWalk = new[] { Row(128, 0), Row(128, 6), Row(128, 12), Row(128, 18) };
            playerIdle = new[] { Row(64, 0)[0], Row(64, 6)[0], Row(64, 12)[0], Row(64, 18)[0] };
            pFacing = 3;
            var body = stands[Player].body;
            body.sprite = playerIdle[3];
            body.transform.localPosition = town.start;
            body.sortingOrder = Order(town.start.y) + 1;
            atDoor = null;
            return true;
        }

        // Steer: the direction of the keys held (each of x, y in -1..1); zero: stop. It ends a walk a click started.
        public void Steer(Vector2 d)
        {
            steer = d;
            if (d != Vector2.zero) { route.Clear(); routeTo = null; }
        }

        // GoTo: walk to a point (the nearest pavement to it), and in at a venue's door when one is named. False: no way
        // there, or none now: down the stairs (phase) or on a train that moves (riding), a click does not walk it off.
        // Either way the walk it is on goes on: only a way found replaces it.
        public bool GoTo(Vector2 to, string venue = null)
        {
            if (Player == "" || phase != 0 || riding >= 0) return false;
            var way = Way(PlayerAt, to);
            if (way == null) return false;
            route.Clear();
            route.AddRange(way);
            routeTo = venue;
            return true;
        }

        // Way: from one point to another over the pavement: half-tile cells, breadth first, then
        // straightened (each point on to the farthest one in a clear line). Null when none.
        List<Vector2> Way(Vector2 from, Vector2 to)
        {
            float x0 = town.x0, x1 = town.w; // the cells from the map's west end (the hospital's side is below x 0) to its east one (the beach's)
            foreach (var s in town.apart) x1 = Mathf.Max(x1, s.y);
            int W = Mathf.CeilToInt((x1 - x0) / Cell), H = Mathf.CeilToInt(town.h / Cell);
            Vector2 C(int i) => new Vector2(x0 + (i % W + 0.5f) * Cell, (i / W + 0.5f) * Cell);
            int Of(Vector2 p) => Mathf.Clamp(Mathf.FloorToInt((p.x - x0) / Cell), 0, W - 1) + Mathf.Clamp((int)(p.y / Cell), 0, H - 1) * W;
            if (!Walkable(to))
            {
                int best = -1;
                for (int i = 0; i < W * H; i++) if (Walkable(C(i)) && (best < 0 || (C(i) - to).sqrMagnitude < (C(best) - to).sqrMagnitude)) best = i;
                if (best < 0) return null;
                to = C(best);
            }
            int a = Of(from), b = Of(to);
            var prev = new int[W * H];
            for (int i = 0; i < prev.Length; i++) prev[i] = -1;
            prev[a] = a;
            var queue = new Queue<int>();
            queue.Enqueue(a);
            while (queue.Count > 0 && prev[b] < 0)
            {
                int i = queue.Dequeue(), x = i % W;
                foreach (int n in new[] { x + 1 < W ? i + 1 : -1, x > 0 ? i - 1 : -1, i + W < W * H ? i + W : -1, i - W })
                    if (n >= 0 && prev[n] < 0 && (n == b || Walkable(C(n)))) { prev[n] = i; queue.Enqueue(n); } // the end's cell may be the door's, on the pavement's edge
            }
            if (prev[b] < 0) return null;
            var cells = new List<Vector2> { to };
            for (int i = prev[b]; i != a; i = prev[i]) cells.Insert(0, C(i));
            var way = new List<Vector2>();
            for (var at = from; way.Count == 0 || way[way.Count - 1] != to;)
            {
                int j = cells.Count - 1;
                while (j > 0 && !Clear(at, cells[j])) j--;
                way.Add(cells[j]);
                at = cells[j];
                cells.RemoveRange(0, j + 1);
            }
            return way;
        }

        bool Clear(Vector2 a, Vector2 b)
        {
            int n = Mathf.CeilToInt((b - a).magnitude / 0.1f);
            for (int i = 1; i <= n; i++) if (!Walkable(Vector2.Lerp(a, b, i / (float)n))) return false;
            return true;
        }

        readonly List<Vector2> roomRoute = new List<Vector2>(); // a click's walk inside the room shown (the game, interiors v2)
        public const float LeaveAt = -2f; // a room's x out on its pavement, past the customers' place outside (x -1.3)
        bool outside = true;              // the walked customer stands out there already: reported once until it steps back
        // At the counter (Aaron 11/14: the customer talks to the clerk): the walked customer standing by its counter
        // point (where a task's customer stands to pay) is reported (AtCounter, with the room), once until it steps
        // away. Never while its task moves it (CanWalk). The page holds the talk; the town only reports.
        public System.Action<string> AtCounter;
        public const float CounterReach = 0.8f;
        bool atCounter;
        void Move(float dt)
        {
            if (Player == "" || shown != "town" || phase != 0 || riding >= 0) SetNear(null); // talks are the town's, walked
            if (Player != "" && shown == "venue" && phase == 0 && shop != null) { MoveInside(dt); return; }
            if (Player == "" || shown != "town" || phase != 0 || riding >= 0) return; // on a train: it carries the customer (Ride)
            var body = stands[Player].body;
            var p = (Vector2)body.transform.localPosition;
            float go = PlayerSpeed * dt;
            var d = steer != Vector2.zero ? steer.normalized * go : Vector2.zero;
            if (d == Vector2.zero && route.Count > 0)
            {
                d = Vector2.ClampMagnitude(route[0] - p, go);
                if ((route[0] - p).magnitude <= go) route.RemoveAt(0);
            }
            var q = p;
            if (Walkable(new Vector2(q.x + d.x, q.y))) q.x += d.x; // an axis at a time: along a kerb, not stuck at it
            if (Walkable(new Vector2(q.x, q.y + d.y))) q.y += d.y;
            q = PastGates(p, q, dt);
            AtAtms(q, dt);
            Talkers(q);
            body.transform.localPosition = q;
            int car = trains.FindIndex(s => q.x >= s.t.x && q.x <= s.t.x + s.length && q.y >= s.t.y && q.y <= s.t.y + 4f);
            body.sortingOrder = car >= 0 ? Order(trains[car].t.y) + 1 : Order(q.y) + 1; // in a car: over it, in its aisle
            if (d != Vector2.zero)
            {
                pFacing = Mathf.Abs(d.x) >= Mathf.Abs(d.y) ? (d.x > 0 ? 0 : 2) : (d.y > 0 ? 1 : 3); // as it means to go, even against a wall
                pTick += dt;
                if (pTick > 0.1f) { pTick = 0f; pFrame = (pFrame + 1) % 6; }
            }
            body.sprite = q != p ? playerWalk[pFacing][pFrame] : playerIdle[pFacing];
            // A door: walked up into, or the end of a click's walk to it; once until it steps away.
            string door = null;
            foreach (var kv in venues) if ((q - DoorOf(kv.Value.v)).magnitude < DoorReach) door = kv.Key;
            if (door == null) atDoor = null;
            else if (door != atDoor && (steer.y > 0f || route.Count == 0 && routeTo == door))
            {
                atDoor = door;
                routeTo = null;
                Clicked?.Invoke(door);
            }
            // The subway's stairs: walked into, or the end of a click's walk to them: down, and up the other's (Hop).
            for (int i = 0; i < town.subway.Length; i++)
                if ((q - town.subway[i].at).magnitude < DoorReach && (steer != Vector2.zero || route.Count == 0 && routeTo == Subway))
                {
                    route.Clear(); routeTo = null;
                    hopTo = i ^ 1;
                    Cover(true, town.subway[i].at);
                    radius = Full();
                    phase = 1;
                    break;
                }
        }

        // ---- the subway's gates (docs/commercial-transit.md, Aaron 11/9: the fare taken as the customer walks in) ----
        // With the page's gates on (Gates: it has a Host that takes fares), the walked customer stops at a shut gate going
        // in and it is reported (Gated, with its station); the page answers (OpenGate): open, the fare taken, or not, and
        // a code for the words shown over the gate. Going out is free. Off (no Host: the video, a page without one): open,
        // as before. The page decides; the town only shows.
        public System.Action<string> Gated;
        public bool GatesOn { get; private set; }
        public bool HasGates => gates.Count > 0; // the bridge says so (gates=1): built in Awake, before its report
        class Sign { public SpriteRenderer plate; public TextMesh note; public Transform root; public bool waiting; public float noteT = -1f; } // words over a gate or an ATM
        class GateState : Sign { public Town.Gate g; public SpriteRenderer open, shut; public bool letIn; public float outT; }
        readonly List<GateState> gates = new List<GateState>();
        string atGate; // the station whose gate the walked customer was reported at, until it steps away from it
        public const float GateStop = 0.45f, GateNote = 4f; // short of the line it waits; seconds an answer stays shown
        static readonly Color GateWait = new Color(0.18f, 0.18f, 0.22f, 0.92f), GatePaid = new Color(0.12f, 0.42f, 0.24f, 0.92f), GateRefused = new Color(0.58f, 0.14f, 0.14f, 0.92f);
        public bool GateShut(string station) => gates.Exists(s => s.g.station == station && s.shut != null && s.shut.enabled); // tests
        public string GateWords(string station) { var s = gates.Find(x => x.g.station == station); return s != null && s.root.gameObject.activeSelf ? s.note.text : ""; } // tests

        public void Gates(bool on)
        {
            GatesOn = on;
            atGate = null;
            foreach (var s in gates) { s.letIn = s.waiting = false; Look(s); }
        }

        // OpenGate: the page's answer for a station's gate. False: no such gate, or the gates are off.
        public bool OpenGate(string station, bool open, string code, string amount)
        {
            var s = gates.Find(x => x.g.station == station);
            if (s == null || !GatesOn) return false;
            s.letIn = open;
            s.waiting = code == "opening"; // the pass is being opened: an answer is still to come
            Note(s, GateText(code, amount), open ? GatePaid : s.waiting ? GateWait : GateRefused);
            Look(s);
            return true;
        }

        // The words over a gate, by the page's code; the amount only digits and a point.
        static string GateText(string code, string amount)
        {
            amount = new string((amount ?? "").Where(c => char.IsDigit(c) || c == '.').Take(8).ToArray());
            switch (code)
            {
                case "sensing": return "感應中…";
                case "opening": return "開通捷運自動扣款…";
                case "paid": return "自動扣款 " + amount;
                case "VELOCITY_LIMIT": return "次數太多，請稍後再搭";
                case "INSUFFICIENT_BUDGET": return "捷運額度已用完";
                case "AUTHORIZATION_EXPIRED": return "授權已過期";
                case "declined": return "未開通自動扣款";
                default: return "無法扣款";
            }
        }

        void Note(Sign s, string text, Color plate)
        {
            s.note.text = text;
            s.plate.color = plate;
            s.noteT = 0f;
            s.root.gameObject.SetActive(true);
        }

        void Look(GateState s)
        {
            bool up = !GatesOn || s.letIn || s.outT > 0f;
            if (s.open != null) s.open.enabled = up;
            if (s.shut != null) s.shut.enabled = !up;
        }

        // PastGates: where the walked customer gets to from p toward q: short of a shut gate going in (reported once),
        // through an open one (shut behind it), out freely (the arm up as it passes).
        Vector2 PastGates(Vector2 p, Vector2 q, float dt)
        {
            foreach (var s in gates)
            {
                var g = s.g;
                if (s.outT > 0f && (s.outT -= dt) <= 0f) Look(s);
                if (s.noteT >= 0f && !s.waiting && (s.noteT += dt) > GateNote && !(atGate == g.station && !s.letIn && s.note.text == GateText("sensing", ""))) { s.noteT = -1f; s.root.gameObject.SetActive(false); }
                if (q.y < g.y0 || q.y > g.y1 || Mathf.Abs(q.x - g.line) > 3f) continue;
                float stop = g.line - GateStop;
                if (q.x > p.x && p.x <= stop + 1e-3f && q.x > stop && GatesOn && !s.letIn) // going in, shut: wait, and ask once
                {
                    q.x = stop;
                    if (atGate != g.station) { atGate = g.station; Note(s, GateText("sensing", ""), GateWait); Gated?.Invoke(g.station); }
                }
                else if (q.x < p.x && p.x > g.line && q.x <= g.line + GateStop) { s.outT = 1.2f; Look(s); } // going out: free
                if (s.letIn && q.x > g.line + 0.6f) { s.letIn = false; Look(s); } // through: shut behind
            }
            if (atGate != null)
            {
                var s = gates.Find(x => x.g.station == atGate);
                if (s == null || (q - new Vector2(s.g.line - GateStop, (s.g.y0 + s.g.y1) / 2f)).magnitude > 1.2f) atGate = null; // asked again when it comes back
            }
            return q;
        }

        // ---- the street's ATM (docs/commercial-atm.md, Aaron 11/9: test USDC claimed in the town) ----
        // The walked customer stopping on an ATM's spot is reported (AtAtm, with its id) once until it steps off; the
        // page asks the Host and answers (AtmSay): minted (the local chain), watching for the faucet's (the testnet), or
        // arrived, with the words over the ATM. No answer (no Host that pays): nothing shown. The town only shows.
        public System.Action<string> AtAtm;
        class AtmState : Sign { public Town.Atm a; }
        readonly List<AtmState> atms = new List<AtmState>();
        string atAtm; // the ATM the walked customer was reported at, until it steps off its spot
        public const float AtmNote = 6f; // seconds an answer stays shown (watching stays)
        public string AtmWords(string id) { var s = atms.Find(x => x.a.id == id); return s != null && s.root.gameObject.activeSelf ? s.note.text : ""; } // tests

        // AtmSay: the page's answer for an ATM. False: no such ATM.
        public bool AtmSay(string id, string code, string amount, string balance, string address)
        {
            var s = atms.Find(x => x.a.id == id);
            if (s == null) return false;
            s.waiting = code == "watch"; // until the faucet's arrives, or the page stops watching
            Note(s, AtmText(code, amount, balance, address), code == "minted" || code == "arrived" ? GatePaid : s.waiting ? GateWait : GateRefused);
            return true;
        }

        // The words over an ATM, by the page's code: amounts only digits and a point, the address only hex, shortened.
        static string AtmText(string code, string amount, string balance, string address)
        {
            string Num(string v) => new string((v ?? "").Where(c => char.IsDigit(c) || c == '.').Take(12).ToArray());
            var a = new string((address ?? "").Where(c => c == 'x' || System.Uri.IsHexDigit(c)).Take(42).ToArray());
            if (a.Length > 12) a = a.Substring(0, 6) + "…" + a.Substring(a.Length - 4);
            switch (code)
            {
                case "minted": return "領到 +" + Num(amount) + "\n餘額 " + Num(balance);
                case "watch": return "到 Circle faucet 領取\n" + a + "\n餘額 " + Num(balance);
                case "arrived": return "入帳 +" + Num(amount) + "\n餘額 " + Num(balance);
                case "limit": return "今日已達提領上限\n餘額 " + Num(balance);
                case "stopped": return "沒有等到入帳";
                default: return "暫時無法使用";
            }
        }

        void AtAtms(Vector2 q, float dt)
        {
            foreach (var s in atms)
            {
                bool on = s.a.spot.Holds(q);
                // stopped on it, or walked up into it: not passing along the pavement
                if (on && atAtm != s.a.id && (steer.y > 0f || steer == Vector2.zero && route.Count == 0)) { atAtm = s.a.id; AtAtm?.Invoke(s.a.id); }
                else if (!on && atAtm == s.a.id) atAtm = null;
                if (s.noteT >= 0f && !s.waiting && (s.noteT += dt) > AtmNote) { s.noteT = -1f; s.root.gameObject.SetActive(false); }
            }
        }

        // ---- the trains (Aaron 11/8: the customer walks into the car and waits for it to start; it pulls in and out) ----
        class TrainState { public Town.Train t; public Transform root; public SpriteRenderer[] outside = new SpriteRenderer[0]; public float length, shift, cover = 1f; }
        readonly List<TrainState> trains = new List<TrainState>();
        int riding = -1, boarded = -1, hopTrain = -1; // the train the customer rides; the one it stands in, waiting; the one it comes in on
        bool arriving;                                // riding in (else out)
        float rideT, boardT;                          // seconds riding (this way); standing in it
        public const float DoorsClose = 3f, Leave = 2.6f, Pull = 8f, Arrive = 3f, RevealReach = 3f; // seconds in it, then out; tiles/s/s; seconds coming in; tiles from its door
        public float TrainShift(int i) => trains[i].shift; // tests: how far it is off its platform (- coming in, + going)
        public float TrainCover(int i) => trains[i].cover; // tests: its outside's opacity
        public bool Riding => riding >= 0;                 // tests
        public int Departures { get; private set; }        // tests
        public int Arrivals { get; private set; }          // tests

        // Ride: a train waits at its platform, open; the walked customer walks in by its door. Standing in its aisle for
        // DoorsClose seconds (out again: no), it pulls off east with the customer into the tunnel, the circle closes
        // there, and the other station's train comes in from its west tunnel with the customer in it, slowing to a stop
        // at the platform; off it they walk.
        void Ride(float dt)
        {
            // Each train's outside: faded while the walked customer is near its door, in it, or riding it; else whole.
            for (int i = 0; i < trains.Count; i++)
            {
                var s = trains[i];
                var door = new Vector2(s.t.inside.x + 0.9f + s.shift, s.t.y);
                bool near = Player != "" && shown == "town" && (riding == i || boarded == i || (PlayerAt - door).magnitude < RevealReach
                    || PlayerAt.x >= s.t.x + s.shift && PlayerAt.x <= s.t.x + s.shift + s.length && PlayerAt.y >= s.t.y && PlayerAt.y <= s.t.y + 4f);
                float c = Mathf.MoveTowards(s.cover, near ? 0f : 1f, 4f * dt);
                if (c == s.cover) continue;
                s.cover = c;
                foreach (var r in s.outside) r.color = new Color(1f, 1f, 1f, c);
            }
            if (Player == "" || shown != "town") return;
            if (riding < 0)
            {
                int on = phase == 0 ? trains.FindIndex(s => s.t.inside.w > 0f && s.t.inside.Holds(PlayerAt)) : -1;
                if (on != boarded) { boarded = on; boardT = 0f; }
                if (on < 0 || (boardT += dt) < DoorsClose) return;
                riding = on; boarded = -1; rideT = 0f; arriving = false;
                route.Clear(); routeTo = null;
            }
            var t = trains[riding];
            rideT += dt;
            float come = t.t.x + t.length - t.t.from;
            float shift = arriving ? -come * Mathf.Pow(1f - Mathf.Clamp01(rideT / Arrive), 2f) : 0.5f * Pull * rideT * rideT;
            var body = stands[Player].body;
            body.transform.localPosition += new Vector3(shift - t.shift, 0f, 0f);
            body.sortingOrder = Order(t.t.y) + 1;
            t.shift = shift;
            t.root.localPosition = new Vector3(shift, 0f, 0f);
            if (arriving)
            {
                if (rideT >= Arrive) { riding = -1; arriving = false; Arrivals++; } // stopped: off they walk
            }
            else if (rideT >= Leave && phase == 0 && t.t.to >= 0 && t.t.to < trains.Count)
            {
                Departures++;
                hopTrain = t.t.to;
                Cover(true, PlayerAt);
                radius = Full();
                phase = 1;
            }
        }

        // Change: the circle closed on the train going into the tunnel: it is back at its platform, and the customer is
        // in the same place in the other station's train, that one in its west tunnel, coming in (Ride). Where the
        // circle opens: that tunnel's mouth.
        Vector2 Change()
        {
            var a = trains[riding];
            var b = trains[hopTrain];
            var rel = PlayerAt - new Vector2(a.t.x + a.shift, a.t.y);
            a.shift = 0f; a.root.localPosition = Vector3.zero;
            float come = b.t.x + b.length - b.t.from;
            b.shift = -come; b.root.localPosition = new Vector3(-come, 0f, 0f);
            var body = stands[Player].body;
            body.transform.localPosition = new Vector2(b.t.x - come, b.t.y) + rel;
            body.sortingOrder = Order(b.t.y) + 1;
            riding = hopTrain; hopTrain = -1; arriving = true; rideT = 0f;
            lookX = b.t.from; lookY = float.NaN;
            ShowCaption(b.t.name, b.t.en);
            return new Vector2(b.t.from, b.t.y + 2f);
        }

        // ---- the station's name as the train comes in (Aaron 11/9): a plate at the top of the view, sliding down into
        // place and fading in, held while the train slows to a stop, then fading out ----
        Transform captionRoot;
        TextMesh captionText;
        SpriteRenderer captionPlate;
        float captionT = -1f; // seconds since shown (real time); -1 none
        public const float CaptionIn = 0.5f, CaptionHold = 3.5f, CaptionOut = 0.7f;
        public string Caption => captionT >= 0f && captionText != null ? captionText.text : ""; // tests

        void ShowCaption(string name, string en)
        {
            if (string.IsNullOrEmpty(name)) return;
            if (captionRoot == null)
            {
                captionRoot = new GameObject("caption").transform;
                captionRoot.SetParent(transform, false);
                captionText = Words(captionRoot, "caption", Vector2.zero, 0.16f, Color.white, Transition + 10);
                captionPlate = Plate(captionRoot, captionText, Rounded(), new Color(0.11f, 0.1f, 0.13f, 0.85f), Transition + 10);
            }
            captionText.text = string.IsNullOrEmpty(en) ? name : name + "\n<size=26>" + en + "</size>";
            captionT = 0f;
            captionRoot.gameObject.SetActive(true);
        }

        void Captioning(float dt)
        {
            if (captionT < 0f || captionRoot == null || cam == null) return;
            captionT += dt;
            float a = captionT < CaptionIn ? captionT / CaptionIn : captionT < CaptionIn + CaptionHold ? 1f : 1f - (captionT - CaptionIn - CaptionHold) / CaptionOut;
            if (a <= 0f && captionT > CaptionIn || shown != "town") { captionT = -1f; captionRoot.gameObject.SetActive(false); return; }
            var s = Safe();
            var top = cam.ScreenToWorldPoint(new Vector3(s.center.x, s.yMax - s.height * 0.14f, 0f));
            float drop = (1f - Mathf.Clamp01(captionT / CaptionIn)) * 0.6f;
            captionRoot.position = new Vector3(top.x, top.y + drop, 0f);
            var c = captionText.color; c.a = a; captionText.color = c;
            var pc = captionPlate.color; pc.a = 0.85f * a; captionPlate.color = pc;
        }

        void Halt() // another customer, or none: no ride half done; the trains back at their platforms
        {
            foreach (var s in trains) { s.shift = 0f; s.root.localPosition = Vector3.zero; }
            riding = boarded = hopTrain = -1; arriving = false;
        }

        public const string Subway = "subway"; // GoTo's venue for the subway's stairs: down them on arrival
        int hopTo = -1;                        // the stairs the walked customer comes up, while the circle runs; -1 none
        public int Mouth(Vector2 p) { if (town != null) for (int i = 0; i < town.subway.Length; i++) if ((p - town.subway[i].at).magnitude < 1.5f) return i; return -1; } // the stairs a click at p means

        // MoveInside: the keys held, or a click's walk, for the person's customer in the room; nothing while its
        // task holds it (ShopView.CanWalk).
        void MoveInside(float dt)
        {
            if (!shop.CanWalk(Player)) { roomRoute.Clear(); return; }
            float go = PlayerSpeed * dt;
            var p = shop.PositionOf(Player);
            var d = steer != Vector2.zero ? steer.normalized * go : Vector2.zero;
            if (steer != Vector2.zero) roomRoute.Clear();
            else if (roomRoute.Count > 0)
            {
                d = Vector2.ClampMagnitude(roomRoute[0] - p, go);
                if ((roomRoute[0] - p).magnitude <= go) roomRoute.RemoveAt(0);
            }
            shop.WalkFree(Player, d, dt);
            // Out of the entrance and along the pavement (Aaron 11/5: back to the town by walking): reported as the
            // town, the page's to decide (as its back-to-town button); the town then has it on the door step (Show).
            // Once until it steps back.
            bool onPavement = shop.PositionOf(Player).x <= LeaveAt;
            if (onPavement && !outside) { roomRoute.Clear(); Clicked?.Invoke(ToTown); }
            outside = onPavement;
            bool counter = (shop.PositionOf(Player) - shop.P(Player, ShopView.Counter)).magnitude < CounterReach;
            if (counter && !atCounter) AtCounter?.Invoke(shop.LayoutKey);
            atCounter = counter;
        }

        // GoInside: walk the person's customer to a place in the room shown (a click there). False: none, or no way.
        public bool GoInside(Vector2 to)
        {
            roomRoute.Clear();
            if (Player == "" || shown != "venue" || shop == null || !shop.CanWalk(Player)) return false;
            var way = shop.FreeWay(shop.PositionOf(Player), to);
            if (way == null) return false;
            roomRoute.AddRange(way);
            return true;
        }

        // ---- views ----
        public Vector2 Origin(string layout) => layout != null && venues.TryGetValue(layout, out var e) ? new Vector2(e.v.x, e.v.y) : Vector2.zero;
        public bool HasVenue(string layout) => layout != null && venues.ContainsKey(layout);
        // Door: where the circle closes or opens in the town: the current venue's door (its middle), else the town's centre.
        Vector2 Door() => venues.TryGetValue(shop != null ? shop.LayoutKey ?? "" : "", out var e) ? new Vector2(e.v.x, e.v.y) + e.v.door + new Vector2(0f, 1f) : (town != null ? town.focus : Vector2.zero);
        Vector2 Inside() => Interior + RoomDoor + new Vector2(0f, 0.8f);

        // Show: the page's view. safe is the uncovered part of the canvas in
        // the canvas's own pixels (Screen's) from its top left; empty
        // or missing: all of it. False (nothing changes) for an unknown mode
        // or a town view without a town. Another view than the one shown
        // goes through the circle; the same one only refits.
        public bool Show(string mode, Rect topLeftSafe)
        {
            if (mode != "venue" && (mode != "town" || !HasTown)) return false;
            safe = topLeftSafe.width > 0f && topLeftSafe.height > 0f ? topLeftSafe : new Rect();
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

        // Fit: the scale at which w x h tiles fill r: a whole multiple when one is within a tenth (crisp, as the
        // operator's page whose canvas is a whole multiple), else the exact fraction.
        static float Fit(Rect r, float w, float h)
        {
            float f = Mathf.Min(r.width / (w * PPU), r.height / (h * PPU));
            return f >= 1f && f - Mathf.Floor(f) < 0.1f ? Mathf.Floor(f) : f;
        }

        // Target: the scale and camera centre of the view shown.
        void Target(out float k, out Vector2 c)
        {
            var s = Safe();
            Vector2 at;
            // As large as the uncovered part allows (Aaron 10/27, as pixel-office: a fraction where it must be); the
            // town never below one screen pixel per art pixel (a narrow window crops it rather than shrink it).
            if (shown == "town") { k = Mathf.Max(1f, Fit(s, town.frame.x, town.frame.y)); at = new Vector2(Pan(float.IsNaN(lookX) ? town.focus.x : lookX, k), LookY); }
            else { var z = shop.RoomSize; var v = RoomView(z); k = Fit(s, v.x, v.y); at = Interior + RoomCentre(z); }
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
            if (k != Mathf.Floor(k)) return c; // a fraction has no whole pixel grid to snap to
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
            Play(Time.deltaTime);
            Drive(Time.deltaTime);
            Move(Time.deltaTime);
            if (HasTown) Ride(Time.deltaTime);
            if (HasTown && shown == "venue" && phase == 0 && Player != "" && Input.GetMouseButtonDown(0) && cam != null)
            {
                GoInside((Vector2)transform.InverseTransformPoint(cam.ScreenToWorldPoint(Input.mousePosition)) - Interior); // the room's own coordinates
                return;
            }
            if (!HasTown || shown != "town" || phase != 0 || !Input.GetMouseButtonDown(0) || cam == null) return;
            var p = (Vector2)transform.InverseTransformPoint(cam.ScreenToWorldPoint(Input.mousePosition));
            var hit = VenueAt(p);
            int m = Mouth(p);
            if (Player != "" && hit == null && m >= 0) GoTo(town.subway[m].at, Subway); // the subway's stairs: there, and down
            else if (Player != "") GoTo(hit != null ? DoorOf(venues[hit].v) : p, hit); // the game: it walks there; at a building, to its door and in
            else if (hit != null && Clicked != null) Clicked(hit);
        }

        // VenueAt: the venue whose outside (as wide as its pieces, up to its top,
        // the pill over it) holds a point in town units.
        public string VenueAt(Vector2 p)
        {
            foreach (var kv in venues)
            {
                var o = new Vector2(kv.Value.v.x, kv.Value.v.y);
                if (p.x >= o.x && p.x <= o.x + kv.Value.right && p.y >= o.y && p.y <= o.y + kv.Value.top + 1f) return kv.Key;
            }
            return null;
        }
        public Vector2 Focus => town != null ? town.focus : Vector2.zero; // tests

        // The pan (V4, docs/town-v2.md): the town view's centre x. At rest on focus (the three shops); in the
        // game it follows the walked customer; a tour (the video's prologue) runs from the west end to the
        // east end, stays there a moment, and goes back to focus; each leg eases in and out (Aaron 11/3). Never
        // past the map's ends.
        float lookX = float.NaN; // NaN: focus
        readonly Queue<(float to, float hold)> tour = new Queue<(float, float)>();
        float legFrom = float.NaN, legT; // the tour's leg now: where it set off, seconds since
        // Tiles a second on a tour, on average (a test runs it faster): 8 since the hospital and the fire station
        // widened the map from 98 tiles to 202 (Aaron 11/8), so the tour takes about as long as before.
        public static float PanSpeed = 8f;
        public const float TourHold = 2f;    // seconds at the east end: the gas station and the fire station
        public float LookX => town == null ? 0f : Pan(float.IsNaN(lookX) ? town.focus.x : lookX, Scale); // tests
        // The view's centre y (Aaron 11/8: up past the hospital and the fire station after the walked customer): at
        // rest on focus; in the game, up or down only as far as keeping the customer in sight needs (Tilt).
        float lookY = float.NaN; // NaN: focus
        public float LookY => town == null ? 0f : float.IsNaN(lookY) ? town.focus.y : lookY;
        public bool Touring => tour.Count > 0; // tests

        public bool Tour()
        {
            if (!HasTown) return false;
            tour.Clear();
            lookX = town.x0; // from the west end (a cut)
            legFrom = float.NaN;
            tour.Enqueue((town.w, TourHold));
            tour.Enqueue((town.focus.x, 0f));
            return true;
        }

        // Pan: x kept so the uncovered part shows nothing past the ends (x0 and w) of the part of the map it is in (the
        // town, or one apart: the beach); a part narrower than it: its middle.
        float Pan(float x, float k)
        {
            float half = Safe().width / (2f * PPU * k), a = town.x0, b = town.w;
            foreach (var s in town.apart) if (x >= s.x && x <= s.y) { a = s.x; b = s.y; }
            return b - a <= 2f * half ? (a + b) / 2f : Mathf.Clamp(x, a + half, b - half);
        }
        int Part(float x) { for (int i = 0; i < town.apart.Length; i++) if (x >= town.apart[i].x && x <= town.apart[i].y) return i; return -1; } // -1: the town

        // Tilt: the centre y nearest focus that shows a customer standing at y whole: its head (2 tiles up) and a
        // tile and a half over it, its feet and a tile under them.
        float Tilt(float y, float k)
        {
            float half = Safe().height / (2f * PPU * k), lo = y + 3.5f - half;
            return Mathf.Clamp(town.focus.y, lo, Mathf.Max(lo, y - 1f + half));
        }

        void Follow(float dt)
        {
            float k = Scale, now = LookX, next, nowY = LookY;
            float wantY = Player != "" && tour.Count == 0 ? Tilt(PlayerAt.y, k) : town.focus.y;
            float nextY = Mathf.MoveTowards(nowY, wantY, Mathf.Max(PanSpeed, 3f * Mathf.Abs(wantY - nowY)) * dt);
            if (tour.Count > 0)
            {
                // A leg of the tour: eased in and out over the time an even pan would take, then its hold.
                var (target, hold) = tour.Peek();
                float to = Pan(target, k);
                if (float.IsNaN(legFrom)) { legFrom = now; legT = 0f; }
                legT += dt;
                float dur = Mathf.Abs(to - legFrom) / PanSpeed;
                next = dur <= 0f ? to : Mathf.Lerp(legFrom, to, Mathf.SmoothStep(0f, 1f, legT / dur));
                if (legT >= dur + hold) { tour.Dequeue(); legFrom = float.NaN; }
            }
            else
            {
                // Catching up, quicker the further off (the customer walks at PlayerSpeed).
                float x = Player != "" ? PlayerAt.x : town.focus.x;
                if (riding >= 0 && Part(trains[riding].t.x) is int pi && pi >= 0) x = Mathf.Clamp(x, town.apart[pi].x, town.apart[pi].y); // in a tunnel: still that station
                float want = Pan(x, k);
                // Another part of the map (back from the beach with no one walked): a cut, not a pan over the gap.
                next = Part(want) != Part(now) ? want : Mathf.MoveTowards(now, want, Mathf.Max(PanSpeed, 3f * Mathf.Abs(want - now)) * dt);
            }
            if (next == lookX && nextY == lookY) return;
            lookX = next; lookY = nextY;
            Retarget();
        }
        public float Top(string layout) => venues.TryGetValue(layout ?? "", out var e) ? e.top : 0f; // tests

        void LateUpdate()
        {
            var room = shop != null ? shop.LayoutKey : null;
            foreach (var kv in insides) if (kv.Value.activeSelf != (kv.Key == room)) kv.Value.SetActive(kv.Key == room); // the shop's own people only
            if (cam == null) return;
            var now = new Vector2Int(Screen.width, Screen.height);
            if (now != screen) { screen = now; Retarget(); } // a new canvas size: at once
            if (phase != 0)
            {
                // The circle runs on the real clock, in steps of a few art pixels (chunky, as the art).
                float step = Full() / Half * Time.unscaledDeltaTime;
                if (phase == 1)
                {
                    if (shown == "town" && hopTo < 0 && hopTrain < 0) irisAt = Door(); // the room may change after the View (the scene's message comes later): close on the door being entered
                    radius = Mathf.Max(0f, radius - step);
                    if (radius <= 0f)
                    {
                        shown = Mode; // closed: the other side
                        Vector2 openAt = shown == "town" ? Door() : Inside();
                        if (hopTo >= 0 && shown == "town" && Player != "")
                        {
                            // Up the other stairs: on their step, facing away from them; the view cuts there (Pan keeps it in that part).
                            var s = town.subway[hopTo];
                            stands[Player].body.transform.localPosition = s.step;
                            stands[Player].body.sortingOrder = Order(s.step.y) + 1;
                            var away = s.step - s.at;
                            pFacing = Mathf.Abs(away.x) >= Mathf.Abs(away.y) ? (away.x > 0 ? 0 : 2) : (away.y > 0 ? 1 : 3);
                            stands[Player].body.sprite = playerIdle[pFacing];
                            lookX = s.step.x; lookY = float.NaN;
                            openAt = s.at;
                        }
                        else if (hopTrain >= 0 && riding >= 0 && shown == "town" && Player != "") openAt = Change(); // the train: into the other station
                        else if (shown == "town" && Player != "" && venues.TryGetValue(shop != null ? shop.LayoutKey ?? "" : "", out var left))
                        {
                            stands[Player].body.transform.localPosition = DoorOf(left.v) - new Vector2(0f, 0.8f); // out on its step, facing the street
                            pFacing = 3;
                            stands[Player].body.sprite = playerIdle[3];
                            lookX = PlayerAt.x; // the circle opens on them
                        }
                        hopTo = -1;
                        Retarget();
                        Cover(true, openAt);
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
            if (shown == "town" && phase == 0 && town != null) Follow(Time.unscaledDeltaTime);
            if (stands.Count > 0) PlaceStands();
            Captioning(Time.unscaledDeltaTime);
            foreach (var (plate, text) in plates)
            {
                var b = text.GetComponent<MeshRenderer>().bounds;
                plate.transform.position = new Vector3(b.center.x, b.center.y, 0f);
                var size = new Vector2(b.size.x + (plate.drawMode == SpriteDrawMode.Sliced ? 0.6f : 0.2f), b.size.y + (plate.drawMode == SpriteDrawMode.Sliced ? 0.25f : 0.1f));
                if (plate.drawMode == SpriteDrawMode.Sliced) plate.size = size;
                else plate.transform.localScale = new Vector3(size.x / plate.transform.parent.lossyScale.x, size.y / plate.transform.parent.lossyScale.y, 1f);
            }
            Speak();
        }

        // ---- the talk (Aaron 11/15: speech as pixel-office draws it) ----
        // The page says who speaks and what (Say: "guide:<id>" or "clerk", the room's own; the line; the walked
        // customer's answer; "" none). Each is a pixel-office speech panel over the head: Resources/UI/speech_panel,
        // a 9-slice white panel sized to the words, in Resources/OfficeFont (a CJK subset). The two open away from each
        // other, and the speaker's goes over the answer when they still meet. The town only draws; the page holds the
        // words and the keys.
        class Speech { public Transform root; public SpriteRenderer panel; public TextMesh text; }
        Speech said, answer;
        string sayAt = "";
        // SpeechOver: the panel's bottom over the head; OverTag: in the town, the walked customer's over its name tag too.
        // SpeechRef: the camera size the words are sized at (the town in a 640 px view, a screen pixel an art pixel); the
        // panels scale with the camera, so they show the same size in the town and in a room, and grow with the canvas.
        public const float SpeechOver = 0.25f, OverTag = 0.5f, SpeechRef = 10f;

        Speech MakeSpeech(string name)
        {
            var s = new Speech { root = new GameObject(name).transform };
            s.root.SetParent(transform, false);
            s.panel = new GameObject(name + "-panel").AddComponent<SpriteRenderer>();
            s.panel.transform.SetParent(s.root, false);
            s.panel.sprite = Resources.Load<Sprite>("UI/speech_panel");
            s.panel.drawMode = SpriteDrawMode.Sliced;
            s.panel.sortingOrder = Labels + 30;
            s.text = new GameObject(name + "-text").AddComponent<TextMesh>();
            s.text.transform.SetParent(s.root, false);
            var font = Resources.Load<Font>("OfficeFont") ?? Resources.Load<Font>(ShopView.FontName);
            s.text.font = font;
            s.text.GetComponent<MeshRenderer>().sharedMaterial = font.material;
            s.text.GetComponent<MeshRenderer>().sortingOrder = Labels + 31;
            s.text.fontSize = 64;
            s.text.characterSize = 0.09f; // pixel-office's 0.045 reads too small at our camera (Aaron 11/15)
            s.text.anchor = TextAnchor.MiddleCenter;
            s.text.alignment = TextAlignment.Center;
            s.text.color = new Color32(45, 50, 68, 255);
            s.root.gameObject.SetActive(false);
            return s;
        }

        public bool Say(string at, string line, string reply)
        {
            if (said == null)
            {
                said = MakeSpeech("talk-said");
                answer = MakeSpeech("talk-answer");
                answer.text.alignment = TextAlignment.Left; // numbered answers read down a left edge
            }
            sayAt = at ?? "";
            said.text.text = sayAt == "" ? "" : line ?? "";
            answer.text.text = sayAt == "" ? "" : reply ?? "";
            foreach (var t in talkers) t.dots.gameObject.SetActive(t.s.w.talk == nearTalk && sayAt != "guide:" + nearTalk);
            Speak();
            return true;
        }

        // Speak: each frame, both panels over their heads (hidden when who it is over is not shown), opened away from
        // each other, kept in the part of the view the page leaves uncovered, and never covering each other.
        void Speak()
        {
            if (said == null) return;
            SpriteRenderer a = null, b = null;
            if (sayAt != "")
            {
                foreach (var s in walkers)
                    if (s.r.gameObject.activeInHierarchy && (s.room ? sayAt == "clerk" && s.w.talk == "clerk" : sayAt == "guide:" + s.w.talk)) a = s.r;
                b = Player == "" ? null : shown == "venue" ? (shop != null ? shop.BodyOf(Player) : null) : stands.TryGetValue(Player, out var st) ? st.body : null;
            }
            bool sa = Over(said, a, 0f), sb = Over(answer, b, shown == "town" ? OverTag : 0f);
            if (sa && sb)
            {
                float dir = a.bounds.center.x <= b.bounds.center.x ? -1f : 1f; // the speaker's opens to its own side, the answer to the other
                said.root.position += new Vector3(dir * Mathf.Max(0f, Box(said).width / 2f - 0.4f), 0f, 0f);
                answer.root.position -= new Vector3(dir * Mathf.Max(0f, Box(answer).width / 2f - 0.4f), 0f, 0f);
            }
            var seen = Seen();
            if (sa) Inside(said, seen);
            if (sb) Inside(answer, seen);
            if (!sa || !sb || !Box(said).Overlaps(Box(answer))) return;
            // Still meeting (close together, or held in at a side): the speaker's over the answer, else the answer under it.
            Rect ra = Box(said), rb = Box(answer);
            float up = rb.yMax + 0.1f + ra.height / 2f;
            if (up + ra.height / 2f <= seen.yMax) said.root.position = new Vector3(said.root.position.x, up, 0f);
            else answer.root.position = new Vector3(answer.root.position.x, ra.yMin - 0.1f - rb.height / 2f, 0f);
        }

        // Over: the speech's panel sized to its words, its bottom just over r's head (and lift more); false, hidden, for
        // no words or no r.
        bool Over(Speech s, SpriteRenderer r, float lift)
        {
            bool on = r != null && r.gameObject.activeInHierarchy && s.text.text != "";
            if (s.root.gameObject.activeSelf != on) s.root.gameObject.SetActive(on);
            if (!on) return false;
            float u = cam.orthographicSize / SpeechRef;
            s.root.localScale = new Vector3(u, u, 1f);
            var size = s.text.GetComponent<MeshRenderer>().bounds.size / u;
            s.panel.size = new Vector2(size.x + 0.5f, size.y + 0.35f);
            var head = r.bounds;
            s.root.position = new Vector3(head.center.x, head.max.y + SpeechOver + lift + s.panel.size.y * u / 2f, 0f);
            return true;
        }

        // Box: a speech's panel in the world.
        Rect Box(Speech s)
        {
            var w = s.panel.size * s.root.localScale.x;
            return new Rect((Vector2)s.root.position - w / 2f, w);
        }

        // Seen: the part of the view the page leaves uncovered (safe), in the world.
        Rect Seen()
        {
            var r = safe.width > 0f && safe.height > 0f ? safe : new Rect(0f, 0f, Screen.width, Screen.height);
            Vector2 lo = cam.ScreenToWorldPoint(new Vector3(r.xMin, Screen.height - r.yMax, 0f)), hi = cam.ScreenToWorldPoint(new Vector3(r.xMax, Screen.height - r.yMin, 0f));
            return Rect.MinMaxRect(lo.x, lo.y, hi.x, hi.y);
        }

        // Inside: the panel moved into seen, a little in from its sides (as far as it fits).
        void Inside(Speech s, Rect seen)
        {
            const float m = 0.15f;
            var box = Box(s);
            float x = Mathf.Clamp(s.root.position.x, seen.xMin + m + box.width / 2f, Mathf.Max(seen.xMin + m + box.width / 2f, seen.xMax - m - box.width / 2f));
            float y = Mathf.Min(s.root.position.y, seen.yMax - m - box.height / 2f);
            s.root.position = new Vector3(x, y, 0f);
        }

        // Tests: a speech's words, whether it shows, and its panel in the world (centre and size).
        public (bool shown, string words, Vector2 at, Vector2 size) SpeechOf(bool customer)
        {
            var s = customer ? answer : said;
            return s == null ? (false, "", Vector2.zero, Vector2.zero) : (s.root.gameObject.activeSelf, s.text.text, (Vector2)s.root.position, Box(s).size);
        }

        // Tests: whether a guide's dots show.
        public bool DotsOver(string id) { var t = talkers.Find(x => x.s.w.talk == id); return t != null && t.dots.gameObject.activeSelf; }
    }
}
