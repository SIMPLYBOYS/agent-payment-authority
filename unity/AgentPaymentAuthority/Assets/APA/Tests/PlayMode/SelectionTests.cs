// PlayMode tests of UX1 selection (docs/commercial-ux1.md): a click names a
// customer with its scene and slot, and only the page's Select of the
// current scene shows the ring. Neither changes a projection.
using System.Collections;
using System.Linq;
using APA;
using NUnit.Framework;
using UnityEngine;
using UnityEngine.SceneManagement;
using UnityEngine.TestTools;

public class SelectionTests
{
    ShopBridge bridge;
    ShopView view;

    static string V2(int scene, string consumer, int slot, string task, int seq, string phase) =>
        "{\"v\":2,\"scene\":" + scene + ",\"consumer\":\"" + consumer + "\",\"slot\":" + slot + ",\"task_id\":\"" + task + "\",\"seq\":" + seq +
        ",\"phase\":\"" + phase + "\",\"verdict\":\"\",\"reason\":\"\",\"animate\":false,\"cause_seq\":" + seq + ",\"task_end\":\"\"}";

    static string Sel(int scene, string consumer) => "{\"scene\":" + scene + ",\"consumer\":\"" + consumer + "\"}";

    string Last(string kind) => bridge.reports.LastOrDefault(r => r.StartsWith(kind + ":"));

    [UnitySetUp]
    public IEnumerator Load()
    {
        yield return SceneManager.LoadSceneAsync("Shop");
        bridge = Object.FindAnyObjectByType<ShopBridge>();
        view = Object.FindAnyObjectByType<ShopView>();
        for (int i = 0; i < 60 && bridge.reports.Count == 0; i++) yield return null;
        Assert.That(bridge.reports, Has.Some.StartsWith("ready:"));
    }

    // A point on a customer's body names it; beside it, nobody.
    [Test]
    public void PickAtNamesTheCustomerUnderThePoint()
    {
        bridge.Receive(V2(1, "consumer-a", 1, "show-a-01", 3, "waiting"));
        bridge.Receive(V2(1, "consumer-b", 1, "show-b-01", 1, "entered"));
        var a = view.PositionOf("consumer-a");
        var b = view.PositionOf("consumer-b");
        Assert.AreEqual("consumer-a", view.PickAt(a + new Vector2(0f, 1f)));
        Assert.AreEqual("consumer-b", view.PickAt(b + new Vector2(0.2f, 0.5f)));
        Assert.IsNull(view.PickAt(a + new Vector2(1.5f, 1f)));
        Assert.IsNull(view.PickAt(a + new Vector2(0f, 3f)));
    }

    // A click is reported with the scene, the customer's own slot and its
    // name, and selects nothing by itself.
    [Test]
    public void AClickIsReportedNotTaken()
    {
        bridge.Receive(V2(4, "consumer-a", 2, "show-a-02", 3, "waiting"));
        bridge.Receive(V2(4, "consumer-b", 5, "show-b-05", 1, "entered"));
        view.Picked("consumer-b");
        Assert.AreEqual("select:4:5:consumer-b", Last("select"));
        Assert.IsNull(view.Highlighted, "a click alone shows no ring");
    }

    // Only a Select of the current scene, for a customer this build shows,
    // moves the ring; a new scene drops it.
    [Test]
    public void OnlyTheCurrentScenesSelectShowsTheRing()
    {
        bridge.Receive(V2(2, "consumer-a", 1, "show-a-01", 3, "waiting"));
        bridge.Select(Sel(2, "consumer-a"));
        Assert.AreEqual("consumer-a", view.Highlighted);
        StringAssert.EndsWith("consumer-a|Applied", Last("selected"));
        bridge.Select(Sel(1, "consumer-b"));
        Assert.AreEqual("consumer-a", view.Highlighted, "an older scene's Select");
        StringAssert.EndsWith("consumer-b|Stale", Last("selected"));
        bridge.Select(Sel(2, "consumer-z"));
        Assert.AreEqual("consumer-a", view.Highlighted, "a customer this build does not show");
        bridge.Select("not json");
        Assert.AreEqual("consumer-a", view.Highlighted, "not a message");
        bridge.Select(Sel(2, ""));
        Assert.IsNull(view.Highlighted, "no one");
        bridge.Select(Sel(2, "consumer-b"));
        Assert.AreEqual("consumer-b", view.Highlighted);
        bridge.Receive("{\"v\":2,\"scene\":3,\"clear\":true}");
        Assert.IsNull(view.Highlighted, "a new scene starts without a ring");
    }

    // Version 1 (the viewer page, history replay) has no selection: a click
    // is not reported and a Select changes nothing.
    [Test]
    public void VersionOneHasNoSelection()
    {
        bridge.Receive("{\"v\":1,\"gen\":1,\"consumer\":\"consumer-a\",\"task_id\":\"task-a\",\"seq\":3,\"phase\":\"waiting\",\"verdict\":\"Ask\",\"reason\":\"\",\"animate\":false}");
        view.Picked("consumer-a");
        Assert.IsNull(Last("select"));
        bridge.Select(Sel(1, "consumer-a"));
        Assert.IsNull(view.Highlighted);
        StringAssert.EndsWith("|Invalid", Last("selected"));
    }

    // Each label sits on a plate that covers it, so it reads over the shelves.
    [UnityTest]
    public IEnumerator LabelsSitOnPlates()
    {
        bridge.Receive(V2(1, "consumer-a", 1, "show-a-01", 3, "waiting"));
        bridge.Receive(V2(1, "consumer-b", 1, "show-b-01", 2, "quoted"));
        yield return null;
        foreach (var who in new[] { "consumer-a", "consumer-b" })
        {
            var tag = GameObject.Find(who + "-tag").GetComponent<MeshRenderer>().bounds;
            var plate = GameObject.Find(who + "-plate").GetComponent<SpriteRenderer>().bounds;
            Assert.IsTrue(plate.Contains(new Vector3(tag.min.x, tag.min.y, plate.center.z)) && plate.Contains(new Vector3(tag.max.x, tag.max.y, plate.center.z)), who + "'s label is not on its plate");
            Assert.Greater(GameObject.Find(who + "-plate").GetComponent<SpriteRenderer>().color.a, 0.8f);
        }
    }
}
