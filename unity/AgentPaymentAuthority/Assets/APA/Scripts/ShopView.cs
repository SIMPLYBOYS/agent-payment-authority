// ShopView: one grocery shop and two pixel customers, A and B. It shows a
// projection and nothing else: where a customer stands follows the phase it
// was sent, and an animation never feeds back into a decision.
// Art is LimeZu (limezu.itch.io) when the files were imported; any missing
// file is a plain placeholder, and the scene then says so.
using System.Collections;
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

        // Fixed points of the room (units of one 32 px tile; the room is 12 x 8).
        static readonly Dictionary<string, Vector2> Spot = new Dictionary<string, Vector2>
        {
            { "door", new Vector2(1.5f, 1.2f) }, { "aisle", new Vector2(4.5f, 3.2f) }, { "counter", new Vector2(8.5f, 1.6f) },
        };

        // Each customer waits, and leaves to, its own place outside the door.
        static Vector2 Outside(string consumer) => consumer == "consumer-b" ? new Vector2(-1.3f, 0.2f) : new Vector2(-1.3f, 3.0f);

        static string SpotFor(string phase)
        {
            switch (phase)
            {
                case "entered": return "door";
                case "quoted": case "unknown": return "aisle";
                case "waiting": case "allowed_unpaid": return "counter";
                default: return "outside"; // also denied, expired, withdrawn: back out of the shop
            }
        }

        class Customer
        {
            public GameObject go;
            public SpriteRenderer body;
            public TextMesh tag;
            public Sprite[] idle, walkRight, walkLeft;
            public Coroutine move;
            public string phase = "empty";
        }

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
                c.idle = Frames(sheet, 64, 18);
                c.walkRight = Frames(sheet, 128, 0);
                c.walkLeft = Frames(sheet, 128, 12);
            }
            else
            {
                var p = new[] { Placeholder(tint, 20, 40) };
                c.idle = c.walkRight = c.walkLeft = p;
            }
            c.body.sprite = c.idle[0];
            c.tag = MakeText(id + "-tag", Vector2.zero, 0.07f, tint);
            c.tag.transform.SetParent(c.go.transform, false);
            c.tag.transform.localPosition = new Vector3(0f, 2.2f, 0f);
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
        // once, every running animation is stopped and the status is cleared.
        public void ResetAll()
        {
            foreach (var c in customers.Values)
            {
                if (c.move != null) StopCoroutine(c.move);
                c.move = null;
                c.phase = "empty";
                c.go.transform.localPosition = Outside(c.go.name);
                c.body.sprite = c.idle[0];
                c.body.color = new Color(1f, 1f, 1f, 0.45f);
            }
            if (status != null) status.text = "No task shown";
        }

        public string PhaseOf(string consumer) => customers.TryGetValue(consumer, out var c) ? c.phase : null;
        public Vector2 PositionOf(string consumer) => customers.TryGetValue(consumer, out var c) ? (Vector2)c.go.transform.localPosition : Vector2.zero;
        public bool Moving(string consumer) => customers.TryGetValue(consumer, out var c) && c.move != null;

        // Show places the consumer's customer for the phase. Only that customer
        // moves; the other stays outside. done(gen) is called when the move
        // has finished, with the generation it was started for.
        public void Show(string consumer, string phase, string verdict, string reason, bool animate, int gen, System.Action<int> done)
        {
            foreach (var kv in customers)
            {
                var c = kv.Value;
                bool active = kv.Key == consumer;
                c.body.color = new Color(1f, 1f, 1f, active ? 1f : 0.45f);
                if (!active) continue;
                c.phase = phase;
                var spot = SpotFor(phase);
                var target = spot == "outside" ? Outside(kv.Key) : Spot[spot];
                if (c.move != null) StopCoroutine(c.move);
                c.move = null;
                if (animate)
                {
                    AnimationsStarted++;
                    c.move = StartCoroutine(Walk(c, target, gen, done));
                }
                else
                {
                    c.go.transform.localPosition = target;
                    c.body.sprite = c.idle[0];
                    done?.Invoke(gen);
                }
            }
            status.text = StatusLine(consumer, phase, verdict, reason);
        }

        static string StatusLine(string consumer, string phase, string verdict, string reason)
        {
            var who = consumer == "consumer-a" ? "A" : "B";
            switch (phase)
            {
                case "waiting": return who + ": ASK - waiting for a human (M1 cannot approve or pay)";
                case "denied": return who + ": DENY (" + reason + ") - leaving";
                case "allowed_unpaid": return who + ": ALLOW - but M1 has no payment capability";
                case "expired": return who + ": ask expired (recorded by the backend) - leaving";
                case "withdrawn": return who + ": ask withdrawn (task cancelled) - leaving";
                case "quoted": return who + ": got a quote";
                case "entered": return who + ": entered the shop";
                default: return who + ": " + phase;
            }
        }

        IEnumerator Walk(Customer c, Vector2 target, int gen, System.Action<int> done)
        {
            var frames = target.x >= c.go.transform.localPosition.x ? c.walkRight : c.walkLeft;
            int f = 0;
            float tick = 0f;
            while (((Vector2)c.go.transform.localPosition - target).sqrMagnitude > 0.0004f)
            {
                c.go.transform.localPosition = Vector2.MoveTowards(c.go.transform.localPosition, target, 3f * Time.deltaTime);
                tick += Time.deltaTime;
                if (tick > 0.1f) { tick = 0f; f = (f + 1) % frames.Length; c.body.sprite = frames[f]; }
                yield return null;
            }
            c.body.sprite = c.idle[0];
            c.move = null;
            done?.Invoke(gen);
        }
    }
}
