// PlayMode tests of the town (T2, docs/commercial-town.md): the room stands
// in its venue, the page's View moves only the camera and the roofs, and a
// click on a venue is reported, never acted on.
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
        for (float end = Time.realtimeSinceStartup + 3f; town.Moving && Time.realtimeSinceStartup < end;) yield return null; // a move is TownView.Step seconds of real time
        Assert.IsFalse(town.Moving, "the move ends");
    }

    // At whole screen pixels per art pixel (or the fraction of a canvas smaller than the room).
    void Crisp()
    {
        var cam = Camera.main;
        Assert.IsTrue(town.Scale < 1f || town.Scale == Mathf.Floor(town.Scale), "a whole multiple: " + town.Scale);
        Assert.AreEqual(Screen.height / (2f * TownView.PPU * town.Scale), cam.orthographicSize, 1e-4f);
    }

    // The ground is drawn in its file's order (the grass first, the road over it), never in an order left to the renderer.
    [Test]
    public void TheGroundIsDrawnInFileOrder()
    {
        var ground = town.GetComponentsInChildren<SpriteRenderer>().Where(r => r.transform.parent == town.transform && r.sortingOrder < -500).ToList();
        Assert.Greater(ground.Count, 1);
        Assert.AreEqual(ground.Count, ground.Select(r => r.sortingOrder).Distinct().Count(), "each ground piece its own order");
        Assert.AreEqual("x-grass", ground.OrderBy(r => r.sortingOrder).First().name, "the grass under everything");
    }

    bool NotesDrawn() => view.GetComponentsInChildren<TextMesh>().Where(t => t.name == "note").Any(t => t.GetComponent<MeshRenderer>().enabled);

    [UnityTest]
    public IEnumerator TheRoomStandsInItsVenueAndItsRoofIsLifted()
    {
        Assert.AreEqual("venue", town.Mode, "the page's usual view: the room, as before");
        Assert.AreEqual(town.Origin("data"), (Vector2)view.transform.position);
        Assert.AreEqual(0f, town.Alpha("data"), "the room's own roof is off");
        Assert.AreEqual(1f, town.Alpha("cafe"), "a neighbour keeps its roof");
        Crisp();
        Assert.IsTrue(view.UseLayout("cafe"));
        yield return null;
        Assert.AreEqual(town.Origin("cafe"), (Vector2)view.transform.position, "another room stands in its own venue");
        yield return Settle();
        Assert.AreEqual(0f, town.Alpha("cafe"));
        Assert.AreEqual(1f, town.Alpha("data"));
        Crisp();
        // The customers' places are the room's own, wherever it stands.
        Assert.AreEqual(view.Point("consumer-a", ShopView.Out), view.PositionOf("consumer-a"));
        Assert.IsTrue(view.UseLayout(ShopView.DefaultLayout));
    }

    [UnityTest]
    public IEnumerator TheTownViewAndBack()
    {
        bridge.View("{\"mode\":\"town\"}");
        Assert.AreEqual("view:0:0:town|Applied", bridge.reports.Last());
        yield return Settle();
        Assert.AreEqual("town", town.Mode);
        foreach (var k in new[] { "data", "cafe", "mart" }) Assert.AreEqual(1f, town.Alpha(k), k + ": every roof on in the town");
        Crisp();
        Assert.IsFalse(view.WordsShown || NotesDrawn(), "the room's title and notes are off under its roof");
        Assert.IsTrue(view.UseLayout("cafe"));
        yield return null;
        Assert.IsFalse(NotesDrawn(), "a room built in the town view hides its notes too");
        Assert.IsTrue(view.UseLayout(ShopView.DefaultLayout));
        bridge.View("{\"mode\":\"venue\",\"safe\":{\"x\":0,\"y\":0,\"width\":0,\"height\":0}}");
        Assert.IsTrue(town.Moving);
        yield return Settle();
        Assert.AreEqual(0f, town.Alpha("data"), "the roof lifts in place");
        Assert.IsTrue(view.WordsShown && NotesDrawn());
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
        Assert.AreEqual("cafe", town.VenueAt(town.Origin("cafe") + new Vector2(6f, 10f)), "its name pill too");
        Assert.IsNull(town.VenueAt(o + new Vector2(-1.3f, 0.6f)), "the lane outside the door is no venue");
        int before = bridge.reports.Count;
        town.Clicked("mart");
        Assert.AreEqual("venue:" + bridge.scene.Scene + ":0:mart", bridge.reports.Last(), "with the scene it was clicked in");
        Assert.AreEqual(before + 1, bridge.reports.Count);
        Assert.AreEqual("data", view.LayoutKey, "the room is the page's to change");
        Assert.AreEqual("town", town.Mode);
    }
}
