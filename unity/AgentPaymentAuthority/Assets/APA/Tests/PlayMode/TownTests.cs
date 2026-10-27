// PlayMode tests of the town (T2, docs/commercial-town.md; option A, Aaron
// 10/27): the room is a place of its own, the page's View goes there and
// back through a closing and opening circle, passers-by walk, A and B stand
// at their venue's door while they have a task, and a click on a venue is
// reported, never acted on.
using System.Collections;
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

    // At whole screen pixels per art pixel (or the fraction of a canvas smaller than the room).
    void Crisp()
    {
        var cam = Camera.main;
        Assert.IsTrue(town.Scale < 1f || town.Scale == Mathf.Floor(town.Scale), "a whole multiple: " + town.Scale);
        Assert.AreEqual(Screen.height / (2f * TownView.PPU * town.Scale), cam.orthographicSize, 1e-4f);
        if (town.Scale < 1f) return;
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
        var room = TownView.Interior + new Vector2(TownView.RoomCX, TownView.RoomCY);
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
            Assert.Less((c - (TownView.Interior + new Vector2(TownView.RoomCX, TownView.RoomCY))).magnitude, 1f, "the camera on the room: " + c);
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
        Assert.Less((c - (TownView.Interior + new Vector2(TownView.RoomCX, TownView.RoomCY))).magnitude, 1f);
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
        foreach (var p in after) Assert.IsTrue(p.x >= 0f && p.x <= 64f && p.y >= 0f && p.y <= 48f, "on the map: " + p);
    }

    // Aaron 10/27: a clerk and other customers in each shop (illustrative): only that shop's, and never on A's or B's way.
    [UnityTest]
    public IEnumerator EachShopHasItsOwnPeopleOffTheCustomersWay()
    {
        var data = JsonUtility.FromJson<Town>(Resources.Load<TextAsset>("Town/town").text);
        bridge.Receive("{\"v\":2,\"scene\":1,\"clear\":true}"); // the two-customer scene: B's points stand aside (ShopView.P)
        foreach (var layout in new[] { "data", "cafe", "mart" })
        {
            Assert.IsTrue(view.UseLayout(layout));
            yield return null;
            var g = data.inside.First(x => x.layout == layout);
            var shown = town.PeopleInside.ToList();
            Assert.AreEqual(g.people.Length, shown.Count, layout + ": its own people, and only them");
            Assert.GreaterOrEqual(g.people.Length, 3, layout + ": a clerk and customers");
            foreach (var w in g.people)
                foreach (var c in new[] { "consumer-a", "consumer-b" })
                    for (int n = 0; n <= ShopView.Pickup; n++)
                    {
                        var at = view.P(c, n);
                        float d = w.path.Length == 1 ? (w.path[0] - at).magnitude
                            : Enumerable.Range(0, w.path.Length - 1).Min(i => DistanceToSegment(at, w.path[i], w.path[i + 1]));
                        Assert.GreaterOrEqual(d, 0.6f, layout + ": " + w.key + " on " + c + "'s point " + n + " " + at);
                    }
        }
        Assert.IsTrue(view.UseLayout(ShopView.DefaultLayout));
    }

    static float DistanceToSegment(Vector2 p, Vector2 a, Vector2 b)
    {
        var ab = b - a;
        float t = ab.sqrMagnitude > 0f ? Mathf.Clamp01(Vector2.Dot(p - a, ab) / ab.sqrMagnitude) : 0f;
        return (p - (a + ab * t)).magnitude;
    }

    static string V2(int scene, string consumer, string phase, int seq = 3, string end = "") =>
        "{\"v\":2,\"scene\":" + scene + ",\"consumer\":\"" + consumer + "\",\"slot\":1,\"task_id\":\"t-" + consumer + "\",\"seq\":" + seq + ",\"phase\":\"" + phase +
        "\",\"verdict\":\"\",\"reason\":\"\",\"animate\":false,\"cause_seq\":" + seq + ",\"task_end\":\"" + end + "\",\"layout\":\"cafe\"}";

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
        Assert.Less((c - (TownView.Interior + new Vector2(TownView.RoomCX, TownView.RoomCY))).magnitude, 1f, "then the room: " + c);
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
        Assert.AreEqual("cafe", town.VenueAt(town.Origin("cafe") + new Vector2(6f, town.Top("cafe") + 0.5f)), "its name pill too");
        Assert.IsNull(town.VenueAt(o + new Vector2(-1.3f, 0.6f)), "the lane outside the door is no venue");
        int before = bridge.reports.Count;
        town.Clicked("mart");
        Assert.AreEqual("venue:" + bridge.scene.Scene + ":0:mart", bridge.reports.Last(), "with the scene it was clicked in");
        Assert.AreEqual(before + 1, bridge.reports.Count);
        Assert.AreEqual("data", view.LayoutKey, "the room is the page's to change");
        Assert.AreEqual("town", town.Mode);
    }
}
