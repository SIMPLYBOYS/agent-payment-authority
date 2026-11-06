// PlayMode tests of the town (T2, docs/commercial-town.md; option A, Aaron
// 10/27): the room is a place of its own, the page's View goes there and
// back through a closing and opening circle, passers-by walk, A and B stand
// at their venue's door while they have a task, and a click on a venue is
// reported, never acted on.
using System.Collections;
using System.Collections.Generic;
using System.Linq;
using APA;
using NUnit.Framework;
using UnityEngine;
using UnityEngine.SceneManagement;
using UnityEngine.TestTools;

public class TownTests
{
    ShopBridge bridge;
    ShopView view;
    TownView town;

    [UnitySetUp]
    public IEnumerator Load()
    {
        yield return SceneManager.LoadSceneAsync("Shop");
        bridge = Object.FindAnyObjectByType<ShopBridge>();
        view = Object.FindAnyObjectByType<ShopView>();
        town = Object.FindAnyObjectByType<TownView>();
        for (int i = 0; i < 60 && bridge.reports.Count == 0; i++) yield return null;
        yield return null; // one LateUpdate: the room placed
        Assert.IsTrue(town.HasTown, "the scene has the town (Resources/Town/town.json)");
    }

    IEnumerator Settle()
    {
        for (float end = Time.realtimeSinceStartup + 3f; town.Moving && Time.realtimeSinceStartup < end;) yield return null; // a transition is twice TownView.Half seconds of real time
        Assert.IsFalse(town.Moving, "the move ends");
    }

    // The camera at the scale it says; at a whole multiple, on the art's pixel grid.
    void Crisp()
    {
        var cam = Camera.main;
        Assert.AreEqual(Screen.height / (2f * TownView.PPU * town.Scale), cam.orthographicSize, 1e-4f);
        if (town.Scale != Mathf.Floor(town.Scale)) return; // a fraction (Aaron 10/27, as pixel-office): no whole pixel grid
        // Snapped: the view's edges, in art pixels, at multiples of 1/k (art pixel edges on screen pixel edges).
        float k = town.Scale;
        foreach (var (c, px) in new[] { (cam.transform.position.x, Screen.width), (cam.transform.position.y, Screen.height) })
        {
            float edge = (c * TownView.PPU - px / (2f * k)) * k;
            Assert.AreEqual(Mathf.Round(edge), edge, 1e-2f, "snapped: " + edge);
        }
    }

    // The page's uncovered part, in CSS pixels from the top left: the target sits at its centre.
    [UnityTest]
    public IEnumerator TheTownSitsInTheUncoveredPart()
    {
        int w = Screen.width, h = Screen.height;
        bridge.View("{\"mode\":\"town\",\"safe\":{\"x\":" + w / 2 + ",\"y\":0,\"width\":" + (w - w / 2) + ",\"height\":" + h / 2 + "}}");
        yield return Settle();
        Crisp();
        var at = Camera.main.WorldToScreenPoint(town.transform.TransformPoint((Vector3)town.Focus));
        Assert.AreEqual(w / 2 + (w - w / 2) / 2f, at.x, 1f + town.Scale, "x: the right half's centre");
        Assert.AreEqual(h - h / 4f, at.y, 1f + town.Scale, "y: the top half's centre (screen y is up)");
    }

    // A room fills the uncovered part (Aaron 10/27: the cafe was too small): the largest scale at which it fits, a fraction allowed.
    [UnityTest]
    public IEnumerator ARoomFillsTheUncoveredPart()
    {
        // Fixed cases (review: not the formula again): the room is 480 x 320 at scale 1.
        if (Screen.width < 600 || Screen.height < 400) Assert.Ignore("the batch screen is smaller than the cases: " + Screen.width + "x" + Screen.height);
        foreach (var (w, h, want) in new[] { (600, 400, 1.25f), (504, 400, 1f), (480, 320, 1f), (400, 300, 0.8333f) })
        {
            bridge.View("{\"mode\":\"venue\",\"safe\":{\"x\":0,\"y\":0,\"width\":" + w + ",\"height\":" + h + "}}");
            yield return Settle();
            Assert.AreEqual(want, town.Scale, 1e-3f, w + "x" + h + ": a fraction where one is needed, a whole multiple within a tenth");
            Crisp();
        }
    }

    // The page measures the uncovered part again once the canvas fills the window: a second View for the
    // same view, arriving while the circle opens, still moves the camera (review of the first option A build).
    [UnityTest]
    public IEnumerator ANewUncoveredPartWhileTheCircleOpens()
    {
        int w = Screen.width, h = Screen.height;
        bridge.View("{\"mode\":\"town\"}");
        for (float end = Time.realtimeSinceStartup + 3f; town.Moving && !town.Opening && Time.realtimeSinceStartup < end;) yield return null;
        Assert.IsTrue(town.Opening, "the circle opens on the town");
        bridge.View("{\"mode\":\"town\",\"safe\":{\"x\":" + w / 2 + ",\"y\":0,\"width\":" + (w - w / 2) + ",\"height\":" + h + "}}");
        yield return Settle();
        var at = Camera.main.WorldToScreenPoint(town.transform.TransformPoint((Vector3)town.Focus));
        Assert.AreEqual(w / 2 + (w - w / 2) / 2f, at.x, 1f + town.Scale, "x: the right half's centre");
    }

    // Turning back while the circle closes: it opens again on the side it closed on, and the camera never left it.
    [UnityTest]
    public IEnumerator TurningBackWhileItCloses()
    {
        var room = TownView.Interior + TownView.RoomCentre(view.RoomSize);
        bridge.View("{\"mode\":\"town\"}");
        yield return null;
        Assert.IsTrue(town.Moving && !town.Opening, "closing");
        bridge.View("{\"mode\":\"venue\"}");
        Assert.IsTrue(town.Opening, "opens again where it closed");
        for (float end = Time.realtimeSinceStartup + 3f; town.Moving && Time.realtimeSinceStartup < end;)
        {
            Assert.Less(((Vector2)Camera.main.transform.position - room).magnitude, 1f, "never went to the town");
            yield return null;
        }
        Assert.IsFalse(town.Moving);
        Assert.AreEqual("venue", town.Mode);
    }

    // The page's View and the scene's new room may come in either order in one frame; the camera ends on the new room.
    [UnityTest]
    public IEnumerator ViewAndANewRoomInEitherOrder()
    {
        foreach (var viewFirst in new[] { true, false })
        {
            bridge.View("{\"mode\":\"town\"}");
            yield return Settle();
            if (viewFirst) { bridge.View("{\"mode\":\"venue\"}"); Assert.IsTrue(view.UseLayout("cafe")); }
            else { Assert.IsTrue(view.UseLayout("cafe")); bridge.View("{\"mode\":\"venue\"}"); }
            yield return null;
            yield return Settle();
            Assert.AreEqual("cafe", view.LayoutKey);
            var c = (Vector2)Camera.main.transform.position;
            Assert.Less((c - (TownView.Interior + TownView.RoomCentre(view.RoomSize))).magnitude, 1f, "the camera on the room: " + c);
            Assert.IsFalse(town.CoverOn, "the circle is gone");
            Crisp();
            Assert.IsTrue(view.UseLayout(ShopView.DefaultLayout));
            yield return null;
        }
    }

    // The ground is drawn in its file's order (the grass first, the road over it), never in an order left to the renderer.
    [Test]
    public void TheGroundIsDrawnInFileOrder()
    {
        var ground = town.GetComponentsInChildren<SpriteRenderer>().Where(r => r.transform.parent == town.transform && r.sortingOrder >= -1000 && r.sortingOrder < -500).ToList(); // the room's backdrop and doorstep lie below
        Assert.Greater(ground.Count, 1);
        Assert.AreEqual(ground.Count, ground.Select(r => r.sortingOrder).Distinct().Count(), "each ground piece its own order");
        Assert.AreEqual("x-grass", ground.OrderBy(r => r.sortingOrder).First().name, "the grass under everything");
    }

    [UnityTest]
    public IEnumerator TheRoomIsAPlaceOfItsOwn()
    {
        Assert.AreEqual("venue", town.Mode, "the page's usual view: the room, as before");
        Assert.AreEqual(TownView.Interior, (Vector2)view.transform.position, "away from the map");
        var c = (Vector2)Camera.main.transform.position;
        Assert.Less((c - (TownView.Interior + TownView.RoomCentre(view.RoomSize))).magnitude, 1f);
        Crisp();
        Assert.IsTrue(view.UseLayout("cafe"));
        yield return null;
        Assert.AreEqual(TownView.Interior, (Vector2)view.transform.position, "every room in the same place");
        Assert.IsFalse(town.Moving, "a new room in the venue view is no transition");
        // The customers' places are the room's own, wherever it stands.
        Assert.AreEqual(view.Point("consumer-a", ShopView.Out), view.PositionOf("consumer-a"));
        Assert.IsTrue(view.UseLayout(ShopView.DefaultLayout));
    }

    // Passers-by walk (illustrative): they move and stay on the map.
    [UnityTest]
    public IEnumerator PassersByWalk()
    {
        var before = town.WalkerPositions.ToList();
        Assert.Greater(before.Count, 5, "more passers-by (Aaron 10/27)");
        for (float end = Time.realtimeSinceStartup + 0.6f; Time.realtimeSinceStartup < end;) yield return null;
        var after = town.WalkerPositions.ToList();
        Assert.IsTrue(before.Zip(after, (a, b) => (a - b).magnitude > 0.05f).Any(m => m), "someone walked");
        foreach (var p in after) Assert.IsTrue(p.x >= 0f && p.x <= 98f && p.y >= 0f && p.y <= 48f, "on the map: " + p); // 98: town v2 (V3)
    }

    // Aaron 10/27: a clerk and other customers in each shop (illustrative): only that shop's, and never under A or B, who
    // draw on top: no body box meets A's or B's anywhere along their legs, nor their labels where they wait (review of 9420431).
    [UnityTest]
    public IEnumerator EachShopHasItsOwnPeopleClearOfAAndB()
    {
        var data = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        bridge.Receive("{\"v\":2,\"scene\":1,\"clear\":true}"); // the two-customer scene: B's points stand aside (ShopView.P)
        int sc = 1;
        foreach (var layout in new[] { "books", "cafe", "mart", "post", "hotel" }) // the town's venues (docs/commercial-consumer.md: the bookshop)
        {
            Assert.IsTrue(view.UseLayout(layout));
            yield return null;
            var g = data.inside.First(x => x.layout == layout);
            CollectionAssert.AreEquivalent(g.people.Select(w => w.key), town.PeopleInside.Select(x => x.key), layout + ": its own people, and only them");
            Assert.GreaterOrEqual(g.people.Length, 3, layout + ": a clerk and customers");
            var people = g.people.SelectMany(w => Samples(w.path).Select(at => (w.key, box: Body(at), face: Face(at)))).ToList();
            // Bodies, all along every leg of A's and B's corridor.
            foreach (var c in new[] { "consumer-a", "consumer-b" })
                for (int e = 0; e < ShopView.Lines.GetLength(0); e++)
                    foreach (var at in Samples(new[] { view.P(c, ShopView.Lines[e, 0]), view.P(c, ShopView.Lines[e, 1]) }))
                        foreach (var (key, box, _) in people)
                            Assert.IsFalse(box.Overlaps(Body(at)), layout + ": " + key + " under " + c + " at " + at);
            // Labels, where A and B wait: the shelf, the counter, the delivery zone.
            foreach (var phase in new[] { "quoted", "waiting", "paid" })
            {
                sc++;
                bridge.Receive(V2(sc, "consumer-a", phase, 3, "", layout));
                bridge.Receive(V2(sc, "consumer-b", phase, 3, "", layout));
                yield return null;
                yield return null; // the plates fit their labels in LateUpdate
                foreach (var c in new[] { "consumer-a", "consumer-b" })
                {
                    var b = GameObject.Find(c + "-plate").GetComponent<SpriteRenderer>().bounds;
                    var o = (Vector2)view.transform.position;
                    var label = Rect.MinMaxRect(b.min.x - o.x, b.min.y - o.y, b.max.x - o.x, b.max.y - o.y);
                    foreach (var (key, _, face) in people) Assert.IsFalse(face.Overlaps(label), layout + ": " + key + " " + face + " under " + c + "'s label " + label + " at " + phase);
                }
            }
        }
        Assert.IsTrue(view.UseLayout(ShopView.DefaultLayout));
    }

    // Body: a character's opaque box from its feet (premade sheets: 42 to 50 px of 64, standing or seated, about 30 px wide).
    // Face: the same below the top of the hair: a label passing over may touch a head's top (the cafe's seats lie under
    // B's label at the shelf, about 0.3 tile of hair, while B waits there), never a face or a body.
    static Rect Body(Vector2 feet) => new Rect(feet.x - 0.45f, feet.y, 0.9f, 1.4f);
    static Rect Face(Vector2 feet) => new Rect(feet.x - 0.45f, feet.y, 0.9f, 1.1f);

    // Samples: the points of a path, and every 0.1 tile along its legs.
    static IEnumerable<Vector2> Samples(Vector2[] path)
    {
        if (path.Length == 1) { yield return path[0]; yield break; }
        for (int i = 0; i + 1 < path.Length; i++)
            for (float t = 0f; t <= 1f; t += 0.1f / Mathf.Max(0.1f, (path[i + 1] - path[i]).magnitude)) yield return Vector2.Lerp(path[i], path[i + 1], t);
        yield return path[path.Length - 1];
    }

    static string V2(int scene, string consumer, string phase, int seq = 3, string end = "", string layout = "cafe") =>
        "{\"v\":2,\"scene\":" + scene + ",\"consumer\":\"" + consumer + "\",\"slot\":1,\"task_id\":\"t-" + consumer + "\",\"seq\":" + seq + ",\"phase\":\"" + phase +
        "\",\"verdict\":\"\",\"reason\":\"\",\"animate\":false,\"cause_seq\":" + seq + ",\"task_end\":\"" + end + "\",\"layout\":\"" + layout + "\"}";

    // Aaron 10/27: once a task ends, its customer leaves the door, as the room walks it out.
    [UnityTest]
    public IEnumerator AnEndedTaskLeavesTheDoor()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        int sc = 0;
        foreach (var (phase, seq, end) in new[] { ("delivered", 9, ""), ("denied", 6, ""), ("expired", 6, ""), ("allowed_unpaid", 6, ""), ("delivery_failed", 9, ""), ("waiting", 6, "cancelled"), ("waiting", 6, "timed_out") })
        {
            sc++; // each case a scene of its own: a new scene starts every slot over
            bridge.Receive(V2(sc, "consumer-a", "waiting"));
            bridge.Receive(V2(sc, "consumer-b", "paid", 7));
            yield return null;
            Assert.IsTrue(town.StandsAtDoor("consumer-a") && town.StandsAtDoor("consumer-b"), "both in the cafe");
            bridge.Receive(V2(sc, "consumer-a", phase, seq, end));
            yield return null;
            Assert.IsFalse(town.StandsAtDoor("consumer-a"), "A's task ended (" + phase + (end != "" ? "/" + end : "") + "): gone from the door");
            Assert.IsTrue(town.StandsAtDoor("consumer-b"), "B's goes on: still there");
        }
    }

    // The customers who transact are A and B (Aaron 10/27: passers-by are only illustrative): in the town, while one has a task, it stands at its venue's door.
    [UnityTest]
    public IEnumerator AAndBStandAtTheirVenuesDoor()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        Assert.IsFalse(town.StandsAtDoor("consumer-a") || town.StandsAtDoor("consumer-b"), "no task, no one at a door");
        bridge.Receive(V2(1, "consumer-a", "waiting"));
        yield return null;
        yield return null;
        Assert.AreEqual("cafe", view.LayoutKey);
        Assert.IsTrue(town.StandsAtDoor("consumer-a"), "A has a task in the cafe");
        Assert.IsFalse(town.StandsAtDoor("consumer-b"), "B has none");
        var cafe = GameObject.Find("venue-cafe").GetComponentsInChildren<SpriteRenderer>().Where(r => !r.name.Contains("pill")).ToList();
        var at = town.StandOf("consumer-a");
        Assert.IsTrue(at.x > town.Origin("cafe").x && at.x < town.Origin("cafe").x + 12f && at.y < town.Origin("cafe").y, "in front of the cafe: " + at);
        var body = GameObject.Find("consumer-a-at-door").GetComponent<SpriteRenderer>();
        Assert.Greater(body.sortingOrder, cafe.Max(r => r.sortingOrder), "drawn over the front it stands at, head and all");
        var tag = GameObject.Find("consumer-a-door-tag").GetComponent<MeshRenderer>();
        Assert.IsTrue(tag.enabled && tag.sortingOrder > body.sortingOrder, "its label too");
    }

    // V4 (docs/town-v2.md): the camera pans. A tour runs from the west end to the east end and back to the shops,
    // on the pixel grid and never past the map's ends; in the game it follows the walked customer.
    [UnityTest]
    public IEnumerator TheCameraPansTheStreet()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        Assert.AreEqual(town.Focus.x, town.LookX, 0.01f, "at rest on the shops");
        float half = Screen.width / (2f * TownView.PPU * town.Scale), w = 98f;
        float speed = TownView.PanSpeed;
        TownView.PanSpeed = 40f;
        try
        {
            bridge.Pan("{\"to\":\"tour\"}");
            Assert.AreEqual("pan:0:0:tour|Applied", bridge.reports.Last());
            Assert.AreEqual(half, town.LookX, 0.01f, "a cut to the west end");
            float max = float.MinValue, t0 = Time.realtimeSinceStartup, tenth = -1f, held = 0f, d = w - 2f * half;
            for (float end = Time.realtimeSinceStartup + 20f; town.Touring && Time.realtimeSinceStartup < end;)
            {
                yield return null;
                max = Mathf.Max(max, town.LookX);
                if (tenth < 0f && town.LookX >= half + 0.1f * d) tenth = Time.realtimeSinceStartup - t0;
                if (Mathf.Abs(town.LookX - (w - half)) < 0.01f) held += Time.unscaledDeltaTime;
                Crisp();
            }
            Assert.IsFalse(town.Touring, "the tour ends");
            Assert.AreEqual(w - half, max, 0.01f, "to the east end");
            // Aaron 11/3: it eases in (a tenth of the way takes longer than an even pan's tenth) and stays at the east end.
            Assert.Greater(tenth, 0.15f * d / TownView.PanSpeed, "it eases in");
            Assert.GreaterOrEqual(held, TownView.TourHold - 0.1f, "it stays at the east end a moment");
            Assert.AreEqual(town.Focus.x, town.LookX, 0.01f, "and back on the shops");
        }
        finally { TownView.PanSpeed = speed; }
        bridge.Pan("{\"to\":\"north\"}");
        Assert.AreEqual("pan:0:0:north|Invalid", bridge.reports.Last());
        // The game: the camera keeps the walked customer in the middle (but at the ends).
        bridge.Play("{\"consumer\":\"consumer-a\"}");
        Assert.IsTrue(town.GoTo(town.DoorAt("mart"), "mart"));
        for (float end = Time.realtimeSinceStartup + 25f; town.RouteLeft > 0 && Time.realtimeSinceStartup < end;) yield return null;
        for (float end = Time.realtimeSinceStartup + 3f; Mathf.Abs(town.LookX - Mathf.Clamp(town.PlayerAt.x, half, w - half)) > 0.05f && Time.realtimeSinceStartup < end;) yield return null;
        Assert.AreEqual(Mathf.Clamp(town.PlayerAt.x, half, w - half), town.LookX, 0.05f, "on the customer");
    }

    [UnityTest]
    public IEnumerator TheTownViewAndBack()
    {
        bridge.View("{\"mode\":\"town\"}");
        Assert.AreEqual("view:0:0:town|Applied", bridge.reports.Last());
        yield return Settle();
        Assert.AreEqual("town", town.Mode);
        Assert.IsFalse(town.CoverOn);
        var c = (Vector2)Camera.main.transform.position;
        Assert.Less((c - town.Focus).magnitude, 1f, "the camera on the town: " + c);
        Crisp();
        bridge.View("{\"mode\":\"venue\",\"safe\":{\"x\":0,\"y\":0,\"width\":0,\"height\":0}}");
        Assert.IsTrue(town.Moving);
        yield return null;
        Assert.IsTrue(town.CoverOn, "the circle closes on the door first");
        Assert.Less(((Vector2)Camera.main.transform.position - town.Focus).magnitude, 1f, "still the town while it closes");
        yield return Settle();
        c = (Vector2)Camera.main.transform.position;
        Assert.Less((c - (TownView.Interior + TownView.RoomCentre(view.RoomSize))).magnitude, 1f, "then the room: " + c);
        Assert.IsFalse(town.CoverOn);
        Crisp();
        bridge.View("{\"mode\":\"street\"}");
        Assert.AreEqual("view:0:0:street|Invalid", bridge.reports.Last());
        bridge.View("not json");
        Assert.AreEqual("view:0:0:|Invalid", bridge.reports.Last());
        Assert.AreEqual("venue", town.Mode, "nothing changed");
    }

    // A click in the town names the venue under it; the bridge reports it and does nothing else.
    [UnityTest]
    public IEnumerator AClickOnAVenueIsReportedOnly()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        var o = town.Origin("mart");
        Assert.AreEqual("mart", town.VenueAt(o + new Vector2(6f, 4f)));
        Assert.AreEqual("mart", town.VenueAt(o + new Vector2(15f, 4f)), "as wide as the shopping centre (18 tiles, its AC to 19)");
        Assert.AreEqual(o.x + 9.5f, GameObject.Find("venue-mart").transform.Find("pill").position.x, 1e-3f, "its name over the middle of its pieces, its side AC included");
        Assert.AreEqual("cafe", town.VenueAt(town.Origin("cafe") + new Vector2(6f, town.Top("cafe") + 0.5f)), "its name pill too");
        Assert.IsNull(town.VenueAt(o + new Vector2(-1.3f, 0.6f)), "the lane outside the door is no venue");
        int before = bridge.reports.Count;
        town.Clicked("mart");
        Assert.AreEqual("venue:" + bridge.scene.Scene + ":0:mart", bridge.reports.Last(), "with the scene it was clicked in");
        Assert.AreEqual(before + 1, bridge.reports.Count);
        Assert.AreEqual("data", view.LayoutKey, "the room is the page's to change");
        Assert.AreEqual("town", town.Mode);
    }

    // Aaron 10/28: passers-by go in and out of the shops: a path through a venue's door goes in there, fading out
    // facing it, stays a while, and comes out again; never over A or B, who transact at that door.
    static Vector2 DoorOf(string layout)
    {
        var v = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text).venues.First(x => x.layout == layout);
        return new Vector2(v.x, v.y) + v.door;
    }

    // Until: waits up to s seconds for a condition, and says how long it took (s if never: polled, as batch mode's frames can stall).
    static IEnumerator Until(System.Func<bool> ok, float s, System.Action<float> took)
    {
        float t0 = Time.realtimeSinceStartup;
        while (Time.realtimeSinceStartup - t0 < s && !ok()) yield return null;
        took(Time.realtimeSinceStartup - t0);
    }
    bool InSight(string key) => town.Passer(key).shop == null && town.Passer(key).shown == 1f; // back on the street (its short path then takes it in again)
    bool InThere(string key, string layout) => town.Passer(key).shop == layout && town.Passer(key).shown < 0.05f;

    static Town.Walker Visit(string key, Vector2 door) => new Town.Walker { key = key, speed = 4f, path = new[] { door - Vector2.up, door, door - Vector2.up } };

    [UnityTest]
    public IEnumerator APasserByGoesInAndComesOut()
    {
        var own = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text).walkers;
        foreach (var l in new[] { "books", "cafe", "mart", "post", "hotel" }) // Aaron 11/6: no one went in or out of the post office or the hotel
            Assert.IsTrue(own.Any(w => w.path.Any(p => (p - DoorOf(l)).sqrMagnitude < 0.01f)), "the town's own passers-by visit " + l);
        town.AddWalker(Visit("x-walk-11", DoorOf("mart")));
        float took = 0f, after = 0f;
        Assert.AreEqual(0, town.DoorFrame("mart"), "the mart's door shut");
        yield return Until(() => town.DoorFrame("mart") == 7, 2f, t => took = t);
        Assert.Less(took, 2f, "its door opens wide for the one going in (Aaron 10/28)");
        yield return Until(() => InThere("x-walk-11", "mart"), 3f, t => took = t); // a tile at 4 a second, then the fade
        Assert.Less(took, 3f, "gone into the mart, out of sight");
        float shut = 0f;
        yield return Until(() => town.DoorFrame("mart") == 0, 3f, t => shut = t);
        Assert.Less(shut, 3f, "and shuts behind it");
        yield return Until(() => InSight("x-walk-11"), 6f, t => after = t);
        Assert.Less(after, 6f, "out again, in sight");
        Assert.Greater(shut + after, 3.5f, "after a while in there");
    }

    // Every venue's door opens (Aaron 11/6: walking past the post office, its door did not): for the person's
    // customer on its step, and wide for a passer-by going in.
    [UnityTest]
    public IEnumerator EveryDoorOpens()
    {
        var withDoors = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text).venues
            .Where(v => v.doorway != null && !string.IsNullOrEmpty(v.doorway.key)).Select(v => v.layout).ToList();
        CollectionAssert.AreEquivalent(new[] { "books", "cafe", "mart", "post", "hotel" }, withDoors, "every venue has a door that opens");
        bridge.View("{\"mode\":\"town\"}"); // the game walks the town, as the page's game mode shows it
        yield return Settle();
        bridge.Play("{\"consumer\":\"consumer-a\"}");
        Assert.AreEqual(0, town.DoorFrame("post"), "the post office's door shut");
        Assert.IsTrue(town.GoTo(town.DoorAt("post")));
        float took = 0f;
        yield return Until(() => town.RouteLeft == 0, 20f, t => took = t);
        yield return Until(() => town.DoorFrame("post") == 7, 2f, t => took = t);
        Assert.AreEqual(7, town.DoorFrame("post"), "it opens for the customer on its step");
        foreach (var l in withDoors)
        {
            town.AddWalker(Visit("x-walk-11", DoorOf(l)));
            yield return Until(() => town.DoorFrame(l) == 7, 2f, t => took = t);
            Assert.AreEqual(7, town.DoorFrame(l), l + "'s door opens wide for a passer-by going in");
        }
    }

    // Aaron 11/6: the buses call at their stops: each halts with its centre at its stop for its seconds (the red one's
    // doors opening, open, shutting), the cars of its lane never on it, and it goes on. Faster than real time.
    [UnityTest]
    public IEnumerator TheBusesCallAtTheirStops()
    {
        var buses = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text).traffic.cars.Where(c => c.stopFor > 0f).ToList();
        CollectionAssert.AreEquivalent(new[] { "x-bus-red", "x-bus-blue-left" }, buses.Select(b => b.key));
        float scale = Time.timeScale;
        Time.timeScale = 4f;
        try
        {
            foreach (var b in buses)
            {
                (string key, float x, float halt, int door) Now() => town.Buses.First(x => x.key == b.key);
                float took = 0f;
                yield return Until(() => Now().halt > 0f, 120f, t => took = t);
                Assert.Greater(Now().halt, 0f, b.key + " halts at its stop");
                Assert.AreEqual(b.stopAt, Now().x, 0.02f, b.key + " with its centre at its stop");
                float t0 = Time.time;
                var doors = new HashSet<int>();
                while (Now().halt > 0f && Time.time - t0 < b.stopFor + 2f)
                {
                    doors.Add(Now().door);
                    var lane = town.Cars.Where(c => c.y == b.y).ToList();
                    foreach (var p in lane)
                        foreach (var q in lane)
                            if (p.x < q.x) Assert.GreaterOrEqual(q.x - q.half - (p.x + p.half), 0f, "two on each other at " + p.x + ", " + q.x + " while " + b.key + " waits");
                    yield return null;
                }
                Assert.GreaterOrEqual(Time.time - t0, b.stopFor - 0.2f, b.key + " stays its seconds");
                int open = b.doorFrames / 2;
                if (!string.IsNullOrEmpty(b.doors))
                    Assert.IsTrue(doors.Any(d => d >= 0 && d < open) && doors.Contains(open) && doors.Any(d => d > open), b.key + "'s doors open, stay open and shut: " + string.Join(",", doors));
                yield return Until(() => Mathf.Abs(Now().x - b.stopAt) > 1f, 10f, t => took = t);
                Assert.Greater(Mathf.Abs(Now().x - b.stopAt), 1f, b.key + " goes on");
            }
        }
        finally { Time.timeScale = scale; }
    }

    [UnityTest]
    public IEnumerator NoOneGoesInOrComesOutOverAAndB()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        var door = DoorOf("cafe");
        float took = 0f;
        town.AddWalker(Visit("x-walk-14", door));
        yield return Until(() => InThere("x-walk-14", "cafe"), 3f, t => took = t);
        Assert.Less(took, 3f, "in before A comes");
        // one on its way up to the door as A comes (review of 10feaa0): in at once, not over A
        town.AddWalker(new Town.Walker { key = "x-walk-02", speed = 1f, path = new[] { door - Vector2.up, door, door - Vector2.up } });
        yield return Until(() => town.Passer("x-walk-02").at.y > door.y - 0.6f, 3f, t => took = t);
        bridge.Receive(V2(1, "consumer-a", "waiting"));
        yield return null;
        Assert.IsTrue(town.StandsAtDoor("consumer-a"), "A at the cafe's door");
        // and one along the lane: past the door, not in, and on (not stuck)
        town.AddWalker(new Town.Walker { key = "x-walk-04", speed = 4f, path = new[] { door - Vector2.up - 2f * Vector2.right, door - Vector2.up, door, door - Vector2.up, door - Vector2.up + 2f * Vector2.right } });
        float x0 = town.Passer("x-walk-04").at.x, far = 0f, came = Time.realtimeSinceStartup;
        for (float end = Time.realtimeSinceStartup + 6f; Time.realtimeSinceStartup < end;)
        {
            if (Time.realtimeSinceStartup - came > 0.6f) Assert.AreEqual(0, town.DoorFrame("cafe"), "the door shut while A stands there"); // after the one caught going up
            var o = town.Passer("x-walk-04");
            Assert.IsTrue(o.shop == null && o.at.y <= door.y - 0.99f, "walks on below A, not in: " + o.at);
            far = Mathf.Max(far, o.at.x - x0);
            Assert.Less(town.Passer("x-walk-14").shown, 0.05f, "the one inside waits there");
            var up = town.Passer("x-walk-02");
            Assert.IsTrue(up.shop == "cafe" && up.shown < 0.05f, "the one going up went in: " + up.at + " " + up.shown);
            yield return null;
        }
        Assert.Greater(far, 3f, "walked on past the door");
        bridge.Receive(V2(1, "consumer-a", "delivered", 9));
        yield return null;
        Assert.IsFalse(town.StandsAtDoor("consumer-a"));
        float after = 0f;
        yield return Until(() => InSight("x-walk-14"), 2f, t => after = t);
        Assert.Less(after, 1.5f, "out once A has gone, the door open first");
    }

    // Aaron 10/28: the plaza's fountain plays.
    [UnityTest]
    public IEnumerator TheFountainPlays()
    {
        var f = GameObject.Find("x-fountain").GetComponent<SpriteRenderer>();
        var seen = new HashSet<Sprite>();
        for (float t0 = Time.time, end = Time.realtimeSinceStartup + 5f; Time.time - t0 < 1f && Time.realtimeSinceStartup < end;) { seen.Add(f.sprite); yield return null; } // a second of its time
        Assert.Greater(seen.Count, 3, "its frames go round");
        Assert.AreEqual(2f, f.sprite.bounds.size.x, 1e-3f, "one frame wide, as the still one was");
    }

    // Aaron 10/28: the signals change and the cars drive: they stop at the crossing on red, keep apart in a lane,
    // and a walker at its kerb steps out only on red (and the cars wait for whoever is on it). A whole cycle of the lights and a crossing.
    [UnityTest]
    public IEnumerator TheLightsChangeAndTheCarsStopForTheCrossing()
    {
        var t = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text).traffic;
        Assert.IsNotNull(t, "the town has traffic");
        float x = (t.from + t.to) / 2f + 0.4f;
        town.AddWalker(new Town.Walker { key = "x-walk-07", speed = 1f, path = new[] { new Vector2(x, t.top + 0.4f), new Vector2(x, t.bottom - 0.4f) } });
        var signal = GameObject.Find(t.lights[0]).GetComponent<SpriteRenderer>();
        var seen = new HashSet<string>();
        var sprites = new HashSet<Sprite>();
        var start = town.Cars.Select(c => c.x).ToList();
        float t0 = Time.time, redSince = -1f;
        bool crossed = false, moved = false;
        var turned = new HashSet<int>(); // the wheels (Aaron 10/28): frames go round while it drives, and stay while it waits
        var last = town.Cars.ToList();
        for (float end = Time.realtimeSinceStartup + 60f; Time.time - t0 < t.cycle.Sum() + (t.top - t.bottom + 1f) + 1f && Time.realtimeSinceStartup < end;) // a cycle, and the time to cross after the red it waited for
        {
            yield return null;
            string light = town.Light;
            seen.Add(light);
            sprites.Add(signal.sprite);
            redSince = light != "red" ? -1f : redSince < 0f ? Time.time : redSince;
            var cars = town.Cars.ToList();
            moved |= cars.Select(c => c.x).Zip(start, (a, b) => Mathf.Abs(a - b) > 1f).Any(m => m);
            foreach (var c in cars) turned.Add(c.frame);
            for (int i = 0; i < cars.Count; i++) if (cars[i].x == last[i].x) Assert.AreEqual(last[i].frame, cars[i].frame, "a car standing still turns no wheel");
            last = cars;
            if (redSince >= 0f && Time.time - redSince > 1.5f) // the last to go on amber has gone over
                foreach (var c in cars) Assert.IsFalse(c.x + c.half > t.from && c.x - c.half < t.to, "a car on the crossing on red at " + c.x);
            foreach (var a in cars)
                foreach (var b in cars)
                    if (a.x < b.x && a.y == b.y) Assert.GreaterOrEqual(b.x - b.half - (a.x + a.half), 0.3f, "two cars in a lane apart");
            var w = town.Passer("x-walk-07").at;
            if (w.y < t.top && w.y > t.bottom)
            {
                Assert.AreEqual("red", light, "on the crossing only on red: " + w);
                foreach (var c in cars) Assert.IsFalse(c.x + c.half > t.from && c.x - c.half < t.to, "no car on the crossing while someone is on it");
            }
            crossed |= w.y <= t.bottom - 0.39f;
        }
        CollectionAssert.AreEquivalent(new[] { "green", "amber", "red" }, seen, "the lights go round");
        Assert.AreEqual(3, sprites.Count, "the signal shows each");
        Assert.IsTrue(moved, "the cars drive");
        Assert.AreEqual(t.cars[0].frames, turned.Count, "their wheels turn: every driving frame shows");
        Assert.IsTrue(crossed, "the walker went over in a cycle");
    }

    // V5 (docs/town-v2.md): the crossroads. The north-south cars go over it on the east-west red; the east-west ones
    // stop before it on their red; a walker crosses the north-south road there only on the east-west green; never two
    // ways at once, never a car onto someone crossing. Two cycles of the lights.
    [UnityTest]
    public IEnumerator TheCrossroadsTakesTurns()
    {
        var t = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text).traffic;
        var j = t.junction;
        Assert.Greater(j.w, 0f, "the town has a junction");
        Assert.AreEqual(2, town.CrossCars.Count(), "two north-south cars");
        var start = town.CrossCars.Select(c => c.y).ToList();
        bool moved = false, crossed = false;
        var lights = new HashSet<string>();
        float t0 = Time.time;
        for (float end = Time.realtimeSinceStartup + 90f; Time.time - t0 < 2f * t.cycle.Sum() && Time.realtimeSinceStartup < end;)
        {
            yield return null;
            var ns = town.CrossCars.ToList();
            moved |= ns.Select(c => c.y).Zip(start, (a, b) => Mathf.Abs(a - b) > 1f).Any(m => m);
            bool nsOnRoad = ns.Any(c => c.y + c.half > t.bottom && c.y - c.half < t.top);
            bool ewOnJunction = town.Cars.Any(c => c.x + c.half > j.x && c.x - c.half < j.x + j.w);
            Assert.IsFalse(nsOnRoad && ewOnJunction, "both ways on the crossroads at once");
            lights.Add(town.CrossLight);
            if (town.Light != "red") Assert.AreEqual("red", town.CrossLight, "the north-south light is red while the east-west one is not");
            if (nsOnRoad) Assert.AreNotEqual("red", town.CrossLight, "a north-south car on the road only on its green or amber");
            var w = town.Passer("x-walk-01").at;
            if (w.x > j.x && w.x < j.x + j.w)
            {
                Assert.AreEqual("green", town.Light, "over the north-south road only on the east-west green: " + w);
                Assert.IsFalse(ns.Any(c => c.y + c.half > j.y && c.y - c.half < j.y + j.h), "no car on the junction while someone crosses it");
                crossed = true;
            }
        }
        Assert.IsTrue(moved, "the north-south cars drive");
        CollectionAssert.AreEquivalent(new[] { "green", "amber", "red" }, lights, "the junction's lights go round");
        Assert.IsTrue(crossed, "a walker went over the north-south road");
    }

    // V5 (Aaron 11/2: the road's cars call at the gas station): one at a time, a westbound car pulls in to the
    // pump, the attendant comes over and fills it, and only then it pulls back out into its lane; not always the
    // same car. Faster than real time.
    [UnityTest]
    public IEnumerator CarsCallAtTheGasStation()
    {
        var v = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text).traffic.visit;
        Assert.Greater(v.from, 0f, "the town has the gas station's visits");
        var called = new HashSet<string>();
        bool atPump = false, backInLane = false, filled = false;
        int backs = 0;
        float scale = Time.timeScale;
        Time.timeScale = 4f;
        try
        {
            for (float end = Time.realtimeSinceStartup + 150f; (called.Count < 3 || backs < 3) && Time.realtimeSinceStartup < end;) // every westbound car, and three times back into the lane
            {
                var before = town.Visitors.ToList();
                yield return null;
                var now = town.Visitors.ToList();
                Assert.LessOrEqual(now.Count, 1, "one at a time");
                foreach (var (key, at, visit) in now)
                {
                    called.Add(key);
                    Assert.IsTrue(at.x >= v.to - 1e-3f && at.x <= v.from + 1e-3f && at.y >= v.lane - 1e-3f && at.y <= v.y + 1e-3f, "between the lane and the pump: " + at);
                    atPump |= visit == 2 && Mathf.Abs(at.x - v.at) < 1e-3f && Mathf.Abs(at.y - v.y) < 1e-3f;
                }
                bool back = before.Any(b => b.visit == 3) && !now.Any() && town.Cars.Any(c => Mathf.Abs(c.x - v.to) < 0.5f && c.y == v.lane);
                backInLane |= back;
                if (back) backs++;
                // Aaron 11/2: one back from the gas station ran onto another. In a lane, never two on each other.
                var road = town.Cars.ToList();
                foreach (var a in road)
                    foreach (var b in road)
                        if (a.x < b.x && a.y == b.y) Assert.GreaterOrEqual(b.x - b.half - (a.x + a.half), 0f, "two cars on each other at " + a.x + ", " + b.x);
                foreach (var (key, at, visit) in now)
                    if (visit == 3 && at.y < v.lane + 1.2f)
                        foreach (var b in road) if (b.y == v.lane) Assert.GreaterOrEqual(Mathf.Abs(b.x - at.x) - b.half, 1.5f, key + " pulling out onto " + b.x);
                var (who, phase) = town.Attendant;
                if (phase == 2)
                {
                    Assert.Less((who - v.side).magnitude, 1e-3f, "filling at the car's side");
                    Assert.IsTrue(now.Any(n => n.visit == 2), "filling a car that is there");
                    filled = true;
                }
                if (now.Any(n => n.visit == 3) && before.Any(b => b.visit == 2)) Assert.IsTrue(phase == 3 || phase == 0, "it leaves once it has been filled");
            }
        }
        finally { Time.timeScale = scale; }
        Assert.IsTrue(atPump, "a car stopped at the pump");
        Assert.IsTrue(filled, "the attendant filled it");
        Assert.IsTrue(backInLane, "and went back into its lane");
        Assert.AreEqual(3, called.Count, "not always the same car: " + string.Join(",", called));
        Assert.GreaterOrEqual(backs, 3, "three times back into the lane");
    }

    // Aaron 11/2: the cars wait for people. Someone standing on the way in: the car calling stops short of them.
    [UnityTest]
    public IEnumerator ACallerWaitsForPeople()
    {
        var v = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text).traffic.visit;
        var them = new Vector2((v.from + v.at) / 2f, v.y + 0.4f);
        town.AddWalker(new Town.Walker { key = "x-walk-07", speed = 0f, path = new[] { them } });
        float nearest = float.MaxValue;
        bool reached = false;
        float scale = Time.timeScale;
        Time.timeScale = 4f;
        try
        {
            for (float end = Time.realtimeSinceStartup + 40f; Time.realtimeSinceStartup < end;)
            {
                yield return null;
                foreach (var (_, at, visit) in town.Visitors)
                {
                    nearest = Mathf.Min(nearest, at.x);
                    reached |= visit == 2;
                    // Its body on the ground: half a car either side, from its wheels up three quarters of its height.
                    Assert.IsFalse(Mathf.Abs(at.x - them.x) < 1.9f && them.y >= at.y && them.y <= at.y + 1.7f, "its body reached them at " + at);
                }
            }
        }
        finally { Time.timeScale = scale; }
        Assert.Less(nearest, v.from, "a car came to call");
        Assert.IsFalse(reached, "it waited for them rather than go round");
    }

    // The game (Aaron 10/29): the person walks A or B; the keys and a click move it over the pavement only; at a
    // door, walked up into or reached by a click on the building, the venue is reported once, as a click is.
    [UnityTest]
    public IEnumerator ThePersonWalksACustomerIntoAVenue()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        bridge.Play("{\"consumer\":\"consumer-x\"}");
        Assert.AreEqual("play:0:0:consumer-x|Invalid", bridge.reports.Last());
        bridge.Play("{\"consumer\":\"consumer-b\"}");
        Assert.AreEqual("play:0:0:consumer-b|Applied", bridge.reports.Last());
        yield return null;
        var start = town.PlayerAt;
        Assert.IsTrue(town.Walkable(start), "it starts on the pavement");
        Assert.IsTrue(town.StandsAtDoor("consumer-b"), "shown, with no task");
        // Up against the shops' fronts: it stops at the pavement's edge.
        bridge.Steer("{\"x\":0,\"y\":1}");
        for (float end = Time.realtimeSinceStartup + 2f; Time.realtimeSinceStartup < end;) yield return null;
        bridge.Steer("{\"x\":0,\"y\":0}");
        Assert.Greater(town.PlayerAt.y, start.y, "it walked up");
        Assert.IsTrue(town.Walkable(town.PlayerAt), "never off the pavement: " + town.PlayerAt);
        // A click on the cafe: along the pavement to its door, then reported once.
        int before = bridge.reports.Count(r => r.StartsWith("venue:"));
        Assert.IsTrue(town.GoTo(town.DoorAt("cafe"), "cafe"));
        float took = 0f;
        yield return Until(() => bridge.reports.Count(r => r.StartsWith("venue:")) > before, 15f, t => took = t);
        Assert.AreEqual("venue:" + bridge.scene.Scene + ":0:cafe", bridge.reports.Last(r => r.StartsWith("venue:")), "at the door: the venue, as a click on it");
        Assert.Less((town.PlayerAt - town.DoorAt("cafe")).magnitude, TownView.DoorReach);
        for (int i = 0; i < 10; i++) yield return null;
        Assert.AreEqual(before + 1, bridge.reports.Count(r => r.StartsWith("venue:")), "once, while it stays there");
        // Across the road to the mart: over the crossing, down the alley.
        Assert.IsTrue(town.GoTo(town.DoorAt("mart"), "mart"));
        bool off = false;
        for (float end = Time.realtimeSinceStartup + 25f; town.RouteLeft > 0 && Time.realtimeSinceStartup < end;) { yield return null; off |= !town.Walkable(town.PlayerAt); }
        Assert.IsFalse(off, "the way stays on the pavement");
        yield return null;
        Assert.AreEqual("venue:" + bridge.scene.Scene + ":0:mart", bridge.reports.Last(r => r.StartsWith("venue:")));
        Assert.AreEqual("town", town.Mode, "the view is the page's to change");
        // Into a room and out: it comes back on that room's door step, facing the street. A town venue's room (the
        // default room is the data shop's, no building in the town since the bookshop, docs/commercial-consumer.md).
        Assert.IsTrue(view.UseLayout("mart"));
        bridge.View("{\"mode\":\"venue\"}");
        yield return Settle();
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        Assert.AreEqual(town.DoorAt(view.LayoutKey) - new Vector2(0f, 0.8f), town.PlayerAt);
        Assert.IsTrue(town.Walkable(town.PlayerAt));
        bridge.Play("{\"consumer\":\"\"}");
        Assert.AreEqual("play:0:0:|Applied", bridge.reports.Last());
        Assert.AreEqual(Vector2.zero, town.PlayerAt, "no one walked");
    }

    // Out by walking (Aaron 11/5): in a room, the person's customer walked out of the entrance and along the pavement is
    // reported as the town, as a click on a venue is (the page goes back, as its button does); once while it stays out
    // there, again after it steps back in and out.
    [UnityTest]
    public IEnumerator ThePersonWalksACustomerOutToTheTown()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        bridge.Play("{\"consumer\":\"consumer-b\"}");
        Assert.IsTrue(view.UseLayout("mart"));
        bridge.View("{\"mode\":\"venue\"}");
        yield return Settle();
        string toTown = "venue:" + bridge.scene.Scene + ":0:" + TownView.ToTown;
        int Out() => bridge.reports.Count(r => r == toTown);
        Assert.Greater(view.PositionOf("consumer-b").x, TownView.LeaveAt, "it waits outside the door, short of the pavement's far side");
        Assert.AreEqual(0, Out());
        bridge.Steer("{\"x\":1,\"y\":0}");
        for (float end = Time.realtimeSinceStartup + 0.6f; Time.realtimeSinceStartup < end;) yield return null;
        Assert.AreEqual(0, Out(), "walking in is no way out");
        bridge.Steer("{\"x\":-1,\"y\":0}");
        yield return Until(() => Out() > 0, 5f, _ => { });
        Assert.AreEqual(1, Out(), "out on the pavement: the town");
        for (float end = Time.realtimeSinceStartup + 0.5f; Time.realtimeSinceStartup < end;) yield return null;
        Assert.AreEqual(1, Out(), "once, while it stays out there");
        bridge.Steer("{\"x\":1,\"y\":0}");
        for (float end = Time.realtimeSinceStartup + 0.6f; Time.realtimeSinceStartup < end;) yield return null;
        bridge.Steer("{\"x\":-1,\"y\":0}");
        yield return Until(() => Out() > 1, 5f, _ => { });
        Assert.AreEqual(2, Out(), "in and out again");
        bridge.Steer("{\"x\":0,\"y\":0}");
        bridge.Play("{\"consumer\":\"\"}");
        Assert.IsTrue(view.UseLayout(ShopView.DefaultLayout));
    }
}
