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
        foreach (var p in after) Assert.IsTrue((p.x >= -59f && p.x <= 143f || p.x >= 400f && p.x <= 480f || p.x >= 800f && p.x <= 854f || p.x >= 1100f && p.x <= 1154f) && p.y >= 0f && p.y <= 48f, "on the map: " + p); // -59 to 143: the hospital and the fire station; then, far apart (Aaron 11/9: one part alone on a wide screen), the beach and the two stations
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
        foreach (var (phase, seq, end) in new[] { ("delivered", 9, ""), ("denied", 6, ""), ("expired", 6, ""), ("allowed_unpaid", 6, ""), ("delivery_failed", 9, ""), ("refunded", 10, ""), ("waiting", 6, "cancelled"), ("waiting", 6, "timed_out") })
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
        float half = Screen.width / (2f * TownView.PPU * town.Scale), x0 = -59f, w = 143f; // the map's ends (Aaron 11/8: the hospital and the fire station)
        float speed = TownView.PanSpeed;
        TownView.PanSpeed = 40f;
        try
        {
            bridge.Pan("{\"to\":\"tour\"}");
            Assert.AreEqual("pan:0:0:tour|Applied", bridge.reports.Last());
            Assert.AreEqual(x0 + half, town.LookX, 0.01f, "a cut to the west end");
            float max = float.MinValue, t0 = Time.realtimeSinceStartup, tenth = -1f, held = 0f, d = w - x0 - 2f * half;
            for (float end = Time.realtimeSinceStartup + 20f; town.Touring && Time.realtimeSinceStartup < end;)
            {
                yield return null;
                max = Mathf.Max(max, town.LookX);
                if (tenth < 0f && town.LookX >= x0 + half + 0.1f * d) tenth = Time.realtimeSinceStartup - t0;
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

    // Aaron 11/18: someone goes into the phone booth to make a call: its door opens for them, and they show in it,
    // over it, at the phone.
    [UnityTest]
    public IEnumerator ThePhoneBoothOpensForTheOneInIt()
    {
        var data = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        var booth = data.booths[0];
        Assert.IsTrue(data.walkers.Any(w => w.path.Any(p => (p - booth.at).sqrMagnitude < 0.01f)), "one of the town's passers-by calls from it");
        town.AddWalker(new Town.Walker { key = "x-walk-11", speed = 4f, path = new[] { booth.at - 2f * Vector2.up, booth.at }, wait = new[] { 0f, 30f } });
        float took = 0f;
        yield return Until(() => (town.Passer("x-walk-11").at - booth.at).sqrMagnitude < 0.01f, 3f, t => took = t);
        Assert.Less(took, 3f, "in it");
        Assert.IsTrue(town.BoothOpen(0), "its door open while they call");
        Assert.Greater(town.PasserOrder("x-walk-11"), town.BoothOrder(0), "drawn over the booth, not hidden behind it");
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

    // Aaron 11/8: the person's customer could not walk past the hospital's door (the map had widened, the pavement
    // not, and the way's cells began at x 0). A click there walks it along the pavement, and on to the fire station.
    [UnityTest]
    public IEnumerator TheCustomerWalksToBothEnds()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        bridge.Play("{\"consumer\":\"consumer-a\"}");
        float scale = Time.timeScale;
        Time.timeScale = 4f;
        try
        {
            foreach (var x in new[] { -28f, 122f }) // the hospital's door, the fire station's
            {
                Assert.IsTrue(town.GoTo(new Vector2(x, 25.6f)), "a way to x " + x);
                yield return Until(() => town.RouteLeft == 0, 20f, _ => { });
                Assert.AreEqual(x, town.PlayerAt.x, 0.5f, "it walked to x " + x);
            }
        }
        finally { Time.timeScale = scale; }
    }

    // Aaron 11/8: the subway: down the stairs by the coffee and tea kiosk into its station, through the gates onto the
    // platform and into the train (shown open); standing in it, it leaves after a few seconds into the tunnel, and the
    // beach station's train comes in with the customer in it and stops; out of it, up the stairs onto the beach. And
    // the same way back. Each change cuts the view to the part it comes into.
    [UnityTest]
    public IEnumerator TheSubwayGoesToTheBeach()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        bridge.Play("{\"consumer\":\"consumer-a\"}");
        var t = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        Assert.AreEqual(4, t.subway.Length, "the kiosk's stairs and its station's, the beach station's and the beach's");
        Assert.AreEqual(2, t.trains.Length, "a train in each station");
        Vector2 beach = t.apart[0], station = t.apart[1], beachStation = t.apart[2], street = new Vector2(t.x0, t.w);
        bool In(Vector2 part, float x) => x >= part.x && x <= part.y;
        Vector2 Aisle(Town.Train tr) => new Vector2(tr.inside.x + 1f, tr.inside.y + tr.inside.h / 2f); // just in by its door
        float scale = Time.timeScale;
        Time.timeScale = 4f;
        try
        {
            // where it walks, down stairs there or not, the train it rides (-1 none), the part it ends in
            var legs = new (Vector2 to, bool stairs, int train, Vector2 part)[] {
                (t.subway[0].at, true, -1, station), (Aisle(t.trains[0]), false, 0, beachStation), (t.subway[2].at, true, -1, beach),
                (t.subway[3].at, true, -1, beachStation), (Aisle(t.trains[1]), false, 1, station), (t.subway[1].at, true, -1, street) };
            foreach (var (to, stairs, train, part) in legs)
            {
                int left = town.Departures, came = town.Arrivals;
                float off = 0f, cover = 1f;
                bool walkedOff = false;
                Assert.IsTrue(town.GoTo(to, stairs ? TownView.Subway : null), "a way to " + to);
                yield return Until(() =>
                {
                    if (train >= 0) { off = Mathf.Max(off, town.TrainShift(train)); cover = Mathf.Min(cover, town.TrainCover(train)); }
                    if (town.Riding && town.GoTo(to)) walkedOff = true; // a click while it moves
                    return In(part, town.PlayerAt.x) && !town.Moving && !town.Riding;
                }, 40f, _ => { });
                Assert.IsTrue(In(part, town.PlayerAt.x), "came into " + part + ": " + town.PlayerAt);
                Assert.IsTrue(In(part, town.LookX), "the view in that part: " + town.LookX);
                if (train < 0) continue;
                var other = t.trains[train].to;
                Assert.IsFalse(walkedOff, "no walk off a moving train");
                Assert.Less(cover, 0.05f, "its outside faded as the customer came to its door (Aaron 11/9)");
                Assert.Greater(off, 5f, "the train pulled off with the customer in it");
                Assert.AreEqual(left + 1, town.Departures, "it left once");
                Assert.AreEqual(came + 1, town.Arrivals, "the other came in once");
                Assert.AreEqual(0f, town.TrainShift(train), "back at its platform");
                Assert.AreEqual(0f, town.TrainShift(other), "the other stopped at its platform");
                Assert.IsTrue(t.trains[other].inside.Holds(town.PlayerAt), "in the other train's aisle: " + town.PlayerAt);
                StringAssert.StartsWith(t.trains[other].name, town.Caption, "its station's name shown as it came in (Aaron 11/9)");
            }
        }
        finally { Time.timeScale = scale; }
    }

    // docs/commercial-transit.md (Aaron 11/9): with the page's gates on, the customer walking in stops at the shut gate
    // and it is reported with its station; a refusal keeps it shut and says why; an Allow lets it through, shut behind.
    [UnityTest]
    public IEnumerator TheGateWaitsForTheFare()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        bridge.Play("{\"consumer\":\"consumer-a\"}");
        var t = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        var g = t.gates[0];
        Assert.AreEqual("shop-street", g.station);
        Assert.IsFalse(town.GateShut(g.station), "off: open, as before");
        float scale = Time.timeScale;
        Time.timeScale = 4f;
        try
        {
            Assert.IsTrue(town.GoTo(t.subway[0].at, TownView.Subway));
            yield return Until(() => town.PlayerAt.x > t.apart[1].x && !town.Moving, 40f, _ => { });
            bridge.Gates("{\"on\":true}");
            Assert.AreEqual("gates:0:0:on|Applied", bridge.reports.Last());
            Assert.IsTrue(town.GateShut(g.station), "on: shut");
            var aisle = new Vector2(t.trains[0].inside.x + 1f, t.trains[0].inside.y + t.trains[0].inside.h / 2f);
            Assert.IsTrue(town.GoTo(aisle), "a way through the gate");
            string asked = "gate:" + bridge.scene.Scene + ":0:" + g.station;
            yield return Until(() => bridge.reports.Contains(asked), 20f, _ => { });
            Assert.That(bridge.reports, Has.Exactly(1).EqualTo(asked), "reported once");
            Assert.AreEqual(g.line - TownView.GateStop, town.PlayerAt.x, 0.01f, "waits short of the line");
            Assert.AreEqual("感應中…", town.GateWords(g.station));

            bridge.OpenGate("{\"station\":\"shop-street\",\"open\":false,\"code\":\"VELOCITY_LIMIT\"}");
            Assert.AreEqual("opengate:0:0:shop-street|Applied", bridge.reports.Last());
            yield return new WaitForSeconds(1f);
            Assert.Less(town.PlayerAt.x, g.line, "refused: still outside");
            Assert.IsTrue(town.GateShut(g.station));
            Assert.AreEqual("次數太多，請稍後再搭", town.GateWords(g.station));
            Assert.That(bridge.reports, Has.Exactly(1).EqualTo(asked), "not asked again while it waits there");

            bridge.OpenGate("{\"station\":\"shop-street\",\"open\":true,\"code\":\"paid\",\"amount\":\"0.02\"}");
            Assert.IsFalse(town.GateShut(g.station), "open");
            Assert.AreEqual("自動扣款 0.02", town.GateWords(g.station));
            yield return Until(() => t.trains[0].inside.Holds(town.PlayerAt), 20f, _ => { });
            Assert.IsTrue(t.trains[0].inside.Holds(town.PlayerAt), "through, on to the train: " + town.PlayerAt);
            Assert.IsTrue(town.GateShut(g.station), "shut behind");
            bridge.OpenGate("{\"station\":\"nowhere\",\"open\":true}");
            Assert.AreEqual("opengate:0:0:nowhere|Invalid", bridge.reports.Last());
        }
        finally { Time.timeScale = scale; }
    }

    // Aaron 11/9 (the homes as fine as LimeZu's sample): a home's roof things and wall air conditioners (props with over)
    // are drawn over the home they stand on, not behind it by their own y.
    [UnityTest]
    public IEnumerator ThingsOnAHomeAreDrawnOverIt()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        var t = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        var drawn = town.GetComponentsInChildren<SpriteRenderer>(true);
        SpriteRenderer At(Town.Piece p) => System.Array.Find(drawn, r => r.name == p.key && Mathf.Abs(r.transform.localPosition.x - p.x) < 0.01f && Mathf.Abs(r.transform.localPosition.y - p.y) < 0.01f);
        // The building a thing stands on: drawn at that y, its picture over the thing's x (the hospital is 27 wide).
        Town.Piece Under(float y, float x) => System.Array.Find(t.props, h =>
        {
            if (h.over != 0f || h.y != y) return false;
            var r = At(h);
            return r != null && x >= h.x + r.sprite.bounds.min.x && x <= h.x + r.sprite.bounds.max.x;
        });
        int n = 0;
        foreach (var p in t.props)
        {
            if (p.over == 0f) continue;
            var home = Under(p.over, p.x);
            Assert.IsNotNull(home, p.key + " at " + p.x + ": no home under it");
            var r = At(p);
            Assert.IsNotNull(r, p.key + " at " + p.x + " is not drawn");
            Assert.Greater(r.sortingOrder, At(home).sortingOrder, p.key + " at " + p.x + " over " + home.key);
            n++;
        }
        Assert.Greater(n, 10, "the homes' things");
        // Aaron 11/9 ("這個頂樓的人不會動？"): the resident at the laundry walks on the roof, over the home, and moves.
        // Aaron 11/10 (the hospital's people hidden or standing still): the companion and the visitor go to its doors,
        // over it, and move.
        // (People seated on a park bench use over too, only to sit in front of it: they stay put.)
        var walking = System.Array.FindAll(t.walkers, w => w.over != 0f && w.speed > 0f);
        Assert.GreaterOrEqual(walking.Length, 3, "the resident, the hospital's companion and visitor");
        var bodies = new SpriteRenderer[walking.Length];
        var from = new Vector3[walking.Length];
        for (int i = 0; i < walking.Length; i++)
        {
            var w = walking[i];
            var under = Under(w.over, w.path[0].x);
            Assert.IsNotNull(under, w.key + ": no building under it");
            float x0 = float.MaxValue, x1 = float.MinValue, y0 = float.MaxValue, y1 = float.MinValue;
            foreach (var q in w.path) { x0 = Mathf.Min(x0, q.x); x1 = Mathf.Max(x1, q.x); y0 = Mathf.Min(y0, q.y); y1 = Mathf.Max(y1, q.y); }
            bodies[i] = System.Array.Find(drawn, d => { var at = d.transform.localPosition; return d.name == w.key && at.x >= x0 - 0.01f && at.x <= x1 + 0.01f && at.y >= y0 - 0.01f && at.y <= y1 + 0.01f; });
            Assert.IsNotNull(bodies[i], w.key + " is not drawn along its way");
            Assert.Greater(bodies[i].sortingOrder, At(under).sortingOrder, w.key + " over " + under.key);
            from[i] = bodies[i].transform.localPosition;
        }
        float scale = Time.timeScale;
        Time.timeScale = 4f; // the longest wait at a door is 6 s
        try { yield return new WaitForSeconds(10f); }
        finally { Time.timeScale = scale; }
        for (int i = 0; i < walking.Length; i++) Assert.AreNotEqual(from[i], bodies[i].transform.localPosition, walking[i].key + " moves");
    }

    // docs/commercial-atm.md (Aaron 11/9): the walked customer stopping at the street's ATM is reported once; walking
    // past it along the pavement is not a visit. The page's answers show over it: watching stays, others go.
    [UnityTest]
    public IEnumerator TheAtmAnswersTheCustomerAtIt()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        bridge.Play("{\"consumer\":\"consumer-a\"}");
        var t = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        var a = t.atms[0];
        Assert.AreEqual("street", a.id);
        string asked = "atm:" + bridge.scene.Scene + ":0:street";
        float y = a.spot.y + a.spot.h / 2f, scale = Time.timeScale;
        Time.timeScale = 4f;
        try
        {
            Assert.IsTrue(town.GoTo(new Vector2(a.spot.x - 2f, y)));
            Assert.IsFalse(town.GoTo(new Vector2(t.trains[1].inside.x + 1f, t.trains[1].inside.y)), "no way from the street to the beach's car");
            Assert.Greater(town.RouteLeft, 0, "a click with no way there: the walk it was on goes on");
            yield return Until(() => town.RouteLeft == 0, 40f, _ => { });
            Assert.IsTrue(town.GoTo(new Vector2(a.spot.x + a.spot.w + 2f, y)), "along the pavement, past it");
            yield return Until(() => town.RouteLeft == 0, 20f, _ => { });
            Assert.That(bridge.reports, Has.None.EqualTo(asked), "passing is not a visit");

            bridge.Walk(System.FormattableString.Invariant($"{{\"x\":{a.spot.x + a.spot.w / 2f},\"y\":{y + 1.5f}}}")); // a click on the ATM itself
            Assert.AreEqual("walk:0:0:Applied", bridge.reports.Last());
            yield return Until(() => bridge.reports.Contains(asked), 20f, _ => { });
            Assert.IsTrue(a.spot.Holds(town.PlayerAt), "stopped at it: " + town.PlayerAt);
            Assert.That(bridge.reports, Has.Exactly(1).EqualTo(asked), "reported once");
            Assert.AreEqual("", town.AtmWords("street"), "nothing shown until the page answers");

            bridge.Atm("{\"id\":\"street\",\"code\":\"watch\",\"address\":\"0x1234567890abcdef1234567890abcdef12345678\",\"balance\":\"5\"}");
            Assert.AreEqual("atmsay:0:0:street|Applied", bridge.reports.Last());
            Assert.AreEqual("到 Circle faucet 領取\n0x1234…5678\n餘額 5", town.AtmWords("street"));
            yield return new WaitForSeconds(TownView.AtmNote + 0.5f);
            Assert.AreNotEqual("", town.AtmWords("street"), "watching stays");
            Assert.That(bridge.reports, Has.Exactly(1).EqualTo(asked), "not asked again while it stands there");

            bridge.Atm("{\"id\":\"street\",\"code\":\"arrived\",\"amount\":\"10\",\"balance\":\"15\"}");
            Assert.AreEqual("入帳 +10\n餘額 15", town.AtmWords("street"));
            yield return new WaitForSeconds(TownView.AtmNote + 0.5f);
            Assert.AreEqual("", town.AtmWords("street"), "an answer goes");
            bridge.Atm("{\"id\":\"street\",\"code\":\"limit\",\"balance\":\"20.00\"}");
            Assert.AreEqual("今日已達提領上限\n餘額 20.00", town.AtmWords("street"), "the day's most taken (Aaron 11/9)");
            bridge.Atm("{\"id\":\"nowhere\",\"code\":\"minted\"}");
            Assert.AreEqual("atmsay:0:0:nowhere|Invalid", bridge.reports.Last());
        }
        finally { Time.timeScale = scale; }
    }

    // Aaron 11/8 (option B): the customer walks up past the fire station, behind it; the view rises with it, its
    // head in sight, and comes back down to rest on focus once it is back on the pavement.
    [UnityTest]
    public IEnumerator TheViewRisesWithTheCustomer()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        Assert.AreEqual(town.Focus.y, town.LookY, 1e-3f, "at rest on focus");
        bridge.Play("{\"consumer\":\"consumer-a\"}");
        float scale = Time.timeScale, half = Screen.height / (2f * TownView.PPU * town.Scale);
        Time.timeScale = 4f;
        try
        {
            Assert.IsTrue(town.GoTo(new Vector2(122f, 46.5f)), "a way behind the fire station");
            yield return Until(() => town.RouteLeft == 0, 20f, _ => { });
            Assert.AreEqual(46.5f, town.PlayerAt.y, 0.5f, "behind it");
            yield return Until(() => town.LookY + half >= town.PlayerAt.y + 2f, 3f, _ => { });
            Assert.GreaterOrEqual(town.LookY + half, town.PlayerAt.y + 2f, "its head in sight");
            Assert.Greater(town.LookY, town.Focus.y, "the view rose");
            Assert.IsTrue(town.GoTo(new Vector2(122f, 26f)));
            yield return Until(() => town.RouteLeft == 0, 20f, _ => { });
            float rest = Mathf.Max(town.Focus.y, 26f + 3.5f - half); // focus, unless a short window needs the head in sight
            yield return Until(() => Mathf.Abs(town.LookY - rest) < 1e-3f, 3f, _ => { });
            Assert.AreEqual(rest, town.LookY, 1e-3f, "back down to rest");
        }
        finally { Time.timeScale = scale; }
        Crisp();
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

    // At the counter (Aaron 11/14: the customer talks to the clerk): in a room, the person's customer walked up to its
    // counter point is reported with the room, once while it stays there, again after it steps away and back.
    [UnityTest]
    public IEnumerator ThePersonWalksACustomerToTheCounter()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        bridge.Play("{\"consumer\":\"consumer-b\"}");
        Assert.IsTrue(view.UseLayout("books"));
        bridge.View("{\"mode\":\"venue\"}");
        yield return Settle();
        string at = "counter:" + bridge.scene.Scene + ":0:books";
        int Talks() => bridge.reports.Count(r => r == at);
        Assert.AreEqual(0, Talks(), "not at the door");
        var counter = view.P("consumer-b", ShopView.Counter);
        Assert.IsTrue(town.GoInside(counter), "a way to the counter");
        yield return Until(() => Talks() > 0, 15f, _ => { });
        Assert.AreEqual(1, Talks(), "at the counter: the room");
        Assert.Less((view.PositionOf("consumer-b") - counter).magnitude, TownView.CounterReach);
        for (float end = Time.realtimeSinceStartup + 0.5f; Time.realtimeSinceStartup < end;) yield return null;
        Assert.AreEqual(1, Talks(), "once, while it stays there");
        var shelf = view.P("consumer-b", ShopView.Shelf);
        Assert.IsTrue(town.GoInside(shelf));
        yield return Until(() => (view.PositionOf("consumer-b") - shelf).magnitude < 0.3f, 15f, _ => { });
        Assert.Greater((view.PositionOf("consumer-b") - counter).magnitude, TownView.CounterReach, "stepped away");
        Assert.IsTrue(town.GoInside(counter));
        yield return Until(() => Talks() > 1, 15f, _ => { });
        Assert.AreEqual(2, Talks(), "away and back");
        bridge.Play("{\"consumer\":\"\"}");
        Assert.IsTrue(view.UseLayout(ShopView.DefaultLayout));
    }

    // Aaron 11/14: a run started at the counter (the talk with the clerk) goes on where the customer stands: its new
    // slot does not put it back outside, and it neither walks back to the door nor browses at the shelf; it waits at
    // the counter, its task holding it.
    [UnityTest]
    public IEnumerator ARunStartedAtTheCounterGoesOnThere()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        bridge.Play("{\"consumer\":\"consumer-b\"}");
        int sc = bridge.scene.Scene + 1;
        bridge.Receive("{\"v\":2,\"scene\":" + sc + ",\"clear\":true,\"layout\":\"books\"}");
        Assert.AreEqual("books", view.LayoutKey);
        bridge.View("{\"mode\":\"venue\"}");
        yield return Settle();
        var counter = view.P("consumer-b", ShopView.Counter);
        Assert.IsTrue(town.GoInside(counter));
        yield return Until(() => (view.PositionOf("consumer-b") - counter).magnitude < 0.3f, 15f, _ => { });
        var here = view.PositionOf("consumer-b");
        Assert.Less((here - counter).magnitude, 0.3f, "walked up to the counter");
        bridge.Receive(V2(sc, "consumer-b", "entered", 1, "", "books"));
        Assert.AreEqual(here, view.PositionOf("consumer-b"), "its new slot keeps it where it stands");
        Assert.IsFalse(view.CanWalk("consumer-b"), "its task holds it");
        bridge.Receive(V2(sc, "consumer-b", "quoted", 2, "", "books"));
        Assert.AreEqual(here, view.PositionOf("consumer-b"), "no browse at the shelf");
        bridge.Receive(V2(sc, "consumer-b", "waiting", 3, "", "books"));
        Assert.Less((view.PositionOf("consumer-b") - counter).magnitude, 0.3f, "it waits at the counter");
        Assert.IsFalse(view.CanWalk("consumer-b"));
        // Aaron 11/15: the clerk speaks over its own head, right of the customer at the counter.
        bridge.Say("{\"at\":\"clerk\",\"line\":\"Welcome!\",\"reply\":\"1 buy\\n2 later\"}");
        Assert.AreEqual("say:0:0:clerk|Applied", bridge.reports.Last());
        yield return null;
        Assert.IsTrue(town.SpeechOf(false).shown && town.SpeechOf(true).shown, "the clerk and the customer speak");
        Assert.Greater(town.SpeechOf(false).at.x, town.SpeechOf(true).at.x, "the clerk stands right of the customer, and its panel");
        Assert.AreEqual("1 buy\n2 later", town.SpeechOf(true).words);
        Vector2 lo = Camera.main.ViewportToWorldPoint(Vector3.zero), hi = Camera.main.ViewportToWorldPoint(Vector3.one);
        var seen = Rect.MinMaxRect(lo.x, lo.y, hi.x, hi.y);
        foreach (var sp in new[] { town.SpeechOf(false), town.SpeechOf(true) })
            Assert.IsTrue(seen.Contains(sp.at - sp.size / 2f) && seen.Contains(sp.at + sp.size / 2f), "kept in the view: " + sp.at + " " + sp.size + " in " + seen);
        // Each panel's tail points down from under it, the clerk's right of the customer's.
        foreach (var who in new[] { false, true })
        {
            var tail = town.TailOf(who);
            var sp = town.SpeechOf(who);
            Assert.IsTrue(tail.shown, "a tail: " + who);
            Assert.Less(tail.tip.y, sp.at.y - sp.size.y / 2f, "under its panel: " + who);
        }
        Assert.Greater(town.TailOf(false).tip.x, town.TailOf(true).tip.x, "the clerk's tail right of the customer's");
        bridge.Say("{\"at\":\"\"}");
        bridge.Play("{\"consumer\":\"\"}");
        Assert.IsTrue(view.UseLayout(ShopView.DefaultLayout));
    }

    // Aaron 11/14: the guides. Each stands on the pavement; walked near one in the town, its id is reported once and
    // a bubble shows over it; away from every guide, "" once. The page shows the lines and takes the key.
    [UnityTest]
    public IEnumerator TheWalkedCustomerComesNearAGuide()
    {
        bridge.View("{\"mode\":\"town\"}");
        yield return Settle();
        bridge.Play("{\"consumer\":\"consumer-b\"}");
        yield return null;
        foreach (var id in new[] { "atm-guide", "subway-guide", "town-guide" })
        {
            var at = town.GuideAt(id);
            Assert.AreNotEqual(Vector2.zero, at, id + " is in the town");
            Assert.IsTrue(town.Walkable(at), id + " stands on the pavement: " + at);
        }
        Assert.AreEqual("", town.NearBy, "near no one at the start");
        string Near(string id) => "near:" + bridge.scene.Scene + ":0:" + id;
        var guide = town.GuideAt("town-guide");
        Assert.IsTrue(town.GoTo(guide + new Vector2(1f, 0f)), "a way to the guide");
        yield return Until(() => town.NearBy == "town-guide", 20f, _ => { });
        Assert.AreEqual("town-guide", town.NearBy);
        for (int i = 0; i < 10; i++) yield return null;
        Assert.AreEqual(1, bridge.reports.Count(r => r == Near("town-guide")), "once, while it stays near");
        // Aaron 11/15: the talk as pixel-office draws it. The page says who speaks, the line and the answer; the town
        // draws each in a speech panel, the line over the guide and the answer over the customer, opened away from each
        // other; while the guide speaks its dots wait. Anyone else is refused; "" ends it.
        Assert.IsTrue(town.DotsOver("town-guide"), "near: the guide's dots");
        bridge.Say("{\"at\":\"someone\",\"line\":\"x\",\"reply\":\"y\"}");
        Assert.AreEqual("say:0:0:someone|Invalid", bridge.reports.Last());
        bridge.Say("{\"at\":\"guide:town-guide\",\"line\":\"First time here?\",\"reply\":\"Yes!\"}");
        Assert.AreEqual("say:0:0:guide:town-guide|Applied", bridge.reports.Last());
        yield return null;
        var said = town.SpeechOf(false);
        var me = town.SpeechOf(true);
        Assert.IsTrue(said.shown && me.shown, "both speak");
        Assert.AreEqual("First time here?", said.words);
        Assert.AreEqual("Yes!", me.words);
        Assert.IsFalse(town.DotsOver("town-guide"), "its dots wait while it speaks");
        var feet = (Vector2)town.transform.TransformPoint(town.GuideAt("town-guide"));
        Assert.Greater(said.at.y - said.size.y / 2f, feet.y + 1.5f, "the line's panel over the guide's head");
        Assert.Greater(said.size.x, 1f, "sized to its words");
        Assert.Less(said.at.x, me.at.x, "the guide stands left: its panel opens left, the answer right");
        Assert.IsFalse(new Rect(said.at - said.size / 2f, said.size).Overlaps(new Rect(me.at - me.size / 2f, me.size)), "not covering each other");
        // Aaron 11/15: as in a comic, each panel's tail points down at who says it, its tip just over the head.
        var mine = (Vector2)town.transform.TransformPoint(town.PlayerAt);
        var tg = town.TailOf(false);
        var tm = town.TailOf(true);
        Assert.IsTrue(tg.shown && tm.shown, "both point");
        Assert.Less(Mathf.Abs(tg.tip.x - feet.x), 0.2f, "the line's tail at the guide: " + tg.tip + " " + feet);
        Assert.Less(Mathf.Abs(tm.tip.x - mine.x), 0.2f, "the answer's tail at the customer: " + tm.tip + " " + mine);
        Assert.Less(tg.tip.y, said.at.y - said.size.y / 2f, "under its panel");
        Assert.Greater(tg.tip.y, feet.y + 1.5f, "over the guide's head");
        bridge.Say("{\"at\":\"\"}");
        yield return null;
        Assert.IsFalse(town.SpeechOf(false).shown || town.SpeechOf(true).shown, "none: both gone");
        Assert.IsTrue(town.DotsOver("town-guide"), "its dots again");
        Assert.IsTrue(town.GoTo(guide + new Vector2(6f, 0f)));
        yield return Until(() => town.NearBy == "", 20f, _ => { });
        Assert.AreEqual(Near(""), bridge.reports.Last(r => r.StartsWith("near:")), "away: none, once");
        bridge.Play("{\"consumer\":\"\"}");
    }
}
