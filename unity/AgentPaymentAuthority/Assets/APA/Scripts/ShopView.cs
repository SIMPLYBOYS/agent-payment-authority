// ShopView: one grocery shop and two pixel customers, A and B. It shows a
// projection and nothing else: where a customer stands follows the phase it
// was sent, and an animation never feeds back into a decision.
// Art is LimeZu (limezu.itch.io) when the files were imported; any missing
// file is a plain placeholder, and the scene then says so.
using System.Collections.Generic;
using UnityEngine;

namespace APA
{
    public class ShopView : MonoBehaviour
    {
        // Assigned by the editor's ShopBuilder from Assets/LimeZu; null when
        // a file was not imported.
        public Texture2D floorSheet, wallSheet, customerA, customerB;
        public Texture2D[] props = new Texture2D[0];
        public Vector2[] propPositions = new Vector2[0];
        public string[] propKeys = new string[0];

        public const int PPU = 32;
        public bool UsingPlaceholders { get; private set; }
        public int AnimationsStarted { get; private set; } // tests: one per animated sequence
        [System.NonSerialized] public float Pace = 1f; // tests only: below 1 slows the walk, so the 2.5 s limit snaps it

        // D2 pacing (docs/demo-presentation-design.md 16.5).
        public const float Walk = 4.5f, BrowsePause = 0.6f, DenyPause = 0.8f, MaxTransition = 2.5f;

        // The corridor (16.3): fixed points joined by horizontal and vertical
        // lines that keep clear of the props. A customer only walks along it.
        // Units are 32 px tiles; the room is 12 x 8.
        public const int Out = 0, OutRow = 1, Door = 2, Aisle = 3, Shelf = 4, Bend = 5, Front = 6, Counter = 7;
        static readonly int[,] Lines = { { Out, OutRow }, { OutRow, Door }, { Door, Aisle }, { OutRow, Aisle }, { Aisle, Shelf }, { Shelf, Bend }, { Bend, Front }, { Front, Counter } };

        // Each customer waits, and leaves to, its own row outside the door.
        public static Vector2 Point(string consumer, int n)
        {
            float row = consumer == "consumer-b" ? 0.2f : 3.0f;
            switch (n)
            {
                case Out: return new Vector2(-1.3f, row);
                case OutRow: return new Vector2(1.5f, row);
                case Door: return new Vector2(1.5f, 1.2f);
                case Aisle: return new Vector2(1.5f, 3.2f);
                case Shelf: return new Vector2(4.5f, 3.2f);
                case Bend: return new Vector2(7.5f, 3.2f);
                case Front: return new Vector2(7.5f, 1.6f);
                default: return new Vector2(8.5f, 1.6f);
            }
        }

        // Shortest ways between the eight points (Floyd-Warshall).
        static void Ways(string consumer, out float[,] d, out int[,] next)
        {
            const int N = 8;
            d = new float[N, N];
            next = new int[N, N];
            for (int i = 0; i < N; i++) for (int j = 0; j < N; j++) { d[i, j] = i == j ? 0f : float.MaxValue; next[i, j] = j; }
            for (int e = 0; e < Lines.GetLength(0); e++)
            {
                int a = Lines[e, 0], b = Lines[e, 1];
                d[a, b] = d[b, a] = (Point(consumer, a) - Point(consumer, b)).magnitude;
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
                default: return Out;
            }
        }

        // One leg of a route: walk to a point, then wait there. seg names the
        // label while on it: browse, counter (both [anim]) or final.
        class Step { public Vector2 to; public int node; public string seg; public float pause; }

        class Customer
        {
            public string id, name;
            public GameObject go;
            public SpriteRenderer body;
            public TextMesh tag;
            public Sprite idleDown, idleRight;
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
        }

        static readonly string[] Facings = { "right", "up", "left", "down" };

        readonly Dictionary<string, Customer> customers = new Dictionary<string, Customer>();
        TextMesh status, banner;
        Font font;

        void Awake()
        {
            font = Resources.GetBuiltinResource<Font>("LegacyRuntime.ttf");
            UsingPlaceholders = floorSheet == null || wallSheet == null || customerA == null || customerB == null;
            for (int i = 0; i < props.Length; i++) if (props[i] == null) UsingPlaceholders = true;
            BuildRoom();
            customers["consumer-a"] = MakeCustomer("consumer-a", customerA, "A (sim agent)", new Color(0.55f, 0.75f, 1f));
            customers["consumer-b"] = MakeCustomer("consumer-b", customerB, "B (sim agent)", new Color(1f, 0.7f, 0.5f));
            banner = MakeText("banner", new Vector2(5.6f, 9.5f), 0.1f, Color.white);
            banner.text = "M1 / fake agent / no payment" + (UsingPlaceholders ? "   [LimeZu pending: placeholder art]" : "   Art: LimeZu (limezu.itch.io)");
            status = MakeText("status", new Vector2(6f, -0.6f), 0.1f, new Color(1f, 0.95f, 0.6f));
            ResetAll();
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

        SpriteRenderer Put(string name, Sprite s, Vector2 at, int order)
        {
            var go = new GameObject(name);
            go.transform.SetParent(transform, false);
            go.transform.localPosition = at;
            var r = go.AddComponent<SpriteRenderer>();
            r.sprite = s;
            r.sortingOrder = order;
            return r;
        }

        void BuildRoom()
        {
            var floor = floorSheet != null ? Cut(floorSheet, 288, 1056, 32, 32) : Placeholder(new Color(0.78f, 0.8f, 0.84f), 32, 32);
            var wall = wallSheet != null ? Cut(wallSheet, 32, 64, 32, 64) : Placeholder(new Color(0.85f, 0.85f, 0.9f), 32, 64);
            for (int x = 0; x < 12; x++)
            {
                for (int y = 0; y < 7; y++) Put("floor", floor, new Vector2(x + 0.5f, y), -100);
                Put("wall", wall, new Vector2(x + 0.5f, 7f), -90);
            }
            for (int i = 0; i < props.Length && i < propPositions.Length; i++)
            {
                var t = props[i];
                var s = t != null ? Sprite.Create(t, new Rect(0, 0, t.width, t.height), new Vector2(0.5f, 0f), PPU)
                                  : Placeholder(new Color(0.6f, 0.5f, 0.4f), 32, 48);
                Put(i < propKeys.Length ? propKeys[i] : "prop", s, propPositions[i], -(int)(propPositions[i].y * 10));
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
                c.idleDown = Frames(sheet, 64, 18)[0];
                c.walk = new[] { Frames(sheet, 128, 0), Frames(sheet, 128, 6), Frames(sheet, 128, 12), Frames(sheet, 128, 18) };
            }
            else
            {
                var p = new[] { Placeholder(tint, 20, 40) };
                c.idleRight = c.idleDown = p[0];
                c.walk = new[] { p, p, p, p };
            }
            c.id = id;
            c.name = label;
            c.body.sprite = c.idleDown;
            c.tag = MakeText(id + "-tag", Vector2.zero, 0.07f, tint);
            c.tag.transform.SetParent(c.go.transform, false);
            c.tag.text = label;
            return c;
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
            foreach (var c in customers.Values)
            {
                c.route = null;
                c.done = null;
                c.seg = "final"; // a dropped walk's [anim] label goes too (review of d7070af)
                c.target = new Target();
                c.from = c.to = Out;
                c.go.transform.localPosition = Point(c.id, Out);
                c.body.color = new Color(1f, 1f, 1f, 0.45f);
                Idle(c);
                ShowLabel(c);
            }
            if (status != null) status.text = "No task shown";
        }

        // For the PlayMode tests: observations only.
        public string PhaseOf(string consumer) => customers.TryGetValue(consumer, out var c) ? c.target.Phase : null;
        public Vector2 PositionOf(string consumer) => customers.TryGetValue(consumer, out var c) ? (Vector2)c.go.transform.localPosition : Vector2.zero;
        public bool Moving(string consumer) => customers.TryGetValue(consumer, out var c) && c.route != null;
        public string LabelOf(string consumer) => customers.TryGetValue(consumer, out var c) ? LabelNow(c) : null;
        public string FacingOf(string consumer) => customers.TryGetValue(consumer, out var c) ? Facings[c.facing] : null;
        public float DeadlineOf(string consumer) => customers.TryGetValue(consumer, out var c) && c.route != null ? c.deadline : -1f;
        public string StatusText => status.text;
        // The points still to walk, from where the customer stands.
        public List<Vector2> RouteOf(string consumer)
        {
            var pts = new List<Vector2>();
            if (!customers.TryGetValue(consumer, out var c)) return pts;
            pts.Add(c.go.transform.localPosition);
            if (c.route != null) for (int i = c.at; i < c.route.Count; i++) pts.Add(c.route[i].to);
            return pts;
        }

        void Activate(string consumer)
        {
            foreach (var kv in customers) kv.Value.body.color = new Color(1f, 1f, 1f, kv.Key == consumer ? 1f : 0.45f);
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
        static void Leg(string consumer, ref Vector2 p, ref int a, ref int b, int goal, string seg, List<Step> steps)
        {
            Ways(consumer, out var d, out var next);
            int via = a;
            if (b != a && (p - Point(consumer, b)).magnitude + d[b, goal] < (p - Point(consumer, a)).magnitude + d[a, goal]) via = b;
            if ((p - Point(consumer, via)).sqrMagnitude > 1e-6f) steps.Add(new Step { to = Point(consumer, via), node = via, seg = seg });
            for (int n = via; n != goal;)
            {
                n = next[n, goal];
                steps.Add(new Step { to = Point(consumer, n), node = n, seg = seg });
            }
            p = Point(consumer, goal);
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

        // Standing still faces down; at the counter it faces the counter.
        void Idle(Customer c)
        {
            bool atCounter = ((Vector2)c.go.transform.localPosition - Point(c.id, Counter)).sqrMagnitude < 1e-4f;
            c.facing = atCounter ? 0 : 3;
            c.body.sprite = atCounter ? c.idleRight : c.idleDown;
        }

        // LabelNow: the label of the segment on now; a display-only segment
        // says [anim] and that no event of its own stands behind it.
        static string LabelNow(Customer c)
        {
            switch (c.seg)
            {
                case "browse": return "[anim] browsing - no event";
                case "counter": return "[anim] to counter - quote bound in request";
                default: return c.target.Label;
            }
        }

        void ShowLabel(Customer c)
        {
            var label = LabelNow(c);
            c.tag.text = label == "" ? c.name : c.name + "\n" + label;
            // Kept inside the view: to the right of a customer on the left
            // half of the room, to the left on the right half.
            bool left = c.go.transform.localPosition.x < 6f;
            c.tag.anchor = left ? TextAnchor.LowerLeft : TextAnchor.LowerRight;
            c.tag.transform.localPosition = new Vector3(left ? -0.4f : 0.4f, 2.05f, 0f);
            if (status != null && c.body.color.a > 0.9f) status.text = (c.id == "consumer-a" ? "A" : "B") + ": " + (label == "" ? c.target.Phase : label);
        }
    }
}
