// PlayMode tests of version 2 (docs/multi-agent-demo-design.md 5.2-5.3): the
// real scene with two customers at once, each in its own slot.
using System.Collections;
using System.Collections.Generic;
using System.Linq;
using APA;
using NUnit.Framework;
using UnityEngine;
using UnityEngine.SceneManagement;
using UnityEngine.TestTools;

public class DualCustomerTests
{
    ShopBridge bridge;
    ShopView view;

    static string V2(int scene, string consumer, int slot, string task, int seq, string phase, bool animate = true) =>
        "{\"v\":2,\"scene\":" + scene + ",\"consumer\":\"" + consumer + "\",\"slot\":" + slot + ",\"task_id\":\"" + task + "\",\"seq\":" + seq +
        ",\"phase\":\"" + phase + "\",\"verdict\":\"\",\"reason\":\"\",\"animate\":" + (animate ? "true" : "false") + ",\"cause_seq\":" + seq + ",\"task_end\":\"\"}";

    List<string> Rendered() => bridge.reports.Where(r => r.StartsWith("rendered:")).ToList();

    IEnumerator Until(System.Func<bool> cond, string what, float limit = 4f)
    {
        float t = 0;
        while (!cond() && t < limit) { t += Time.deltaTime; yield return null; }
        Assert.IsTrue(cond(), what);
    }

    [UnitySetUp]
    public IEnumerator Load()
    {
        yield return SceneManager.LoadSceneAsync("Shop");
        bridge = Object.FindAnyObjectByType<ShopBridge>();
        view = Object.FindAnyObjectByType<ShopView>();
        for (int i = 0; i < 60 && bridge.reports.Count == 0; i++) yield return null;
        Assert.That(bridge.reports, Has.Some.StartsWith("ready:"));
    }

    // The build says what it can show before it is ready.
    [Test]
    public void BridgeReportComesFirst()
    {
        var rooms = view.LayoutKeys.ToList();
        rooms.Sort(System.StringComparer.Ordinal); // every layout file, whatever is added (review S4)
        Assert.AreEqual("bridge:0:0:versions=1,2;customers=consumer-a,consumer-b;select=1;layouts=" + string.Join(",", rooms) + ";town=1;play=1", bridge.reports[0]); // T2: the build has the town; the game (Aaron 10/29)
        CollectionAssert.IsSupersetOf(rooms, new[] { "cafe", "data", "mart" });
        StringAssert.StartsWith("ready:", bridge.reports[1]);
    }

    // Both customers walk at once, at full brightness, and each reports its
    // own arrival with its own slot; the status has a line each.
    [UnityTest]
    public IEnumerator TwoCustomersAtOnce()
    {
        bridge.Receive(V2(1, "consumer-a", 1, "show-a-01", 3, "waiting"));
        bridge.Receive(V2(1, "consumer-b", 1, "show-b-01", 2, "quoted"));
        Assert.IsTrue(view.Moving("consumer-a") && view.Moving("consumer-b"), "both walk at once");
        Assert.AreEqual(1f, view.AlphaOf("consumer-a"));
        Assert.AreEqual(1f, view.AlphaOf("consumer-b"));
        yield return Until(() => !view.Moving("consumer-a") && !view.Moving("consumer-b"), "both arrived", 4f);
        CollectionAssert.IsSupersetOf(Rendered(), new[] {
            "rendered:1:3:consumer-a|1|show-a-01|waiting||",
            "rendered:1:2:consumer-b|1|show-b-01|quoted||" });
        StringAssert.Contains("A：超過自動範圍", view.StatusText);
        StringAssert.Contains("B：選好商品", view.StatusText);
        // Two customers never stand on one spot inside the shop.
        Assert.AreNotEqual(view.P("consumer-a", ShopView.Counter), view.P("consumer-b", ShopView.Counter));
        Assert.AreEqual(view.P("consumer-a", ShopView.Counter), view.PositionOf("consumer-a"));
    }

    // A's new task resets A only: B keeps its place and target, and A's
    // dropped walk reports nothing.
    [UnityTest]
    public IEnumerator ANewTaskLeavesBAlone()
    {
        bridge.Receive(V2(1, "consumer-b", 1, "show-b-01", 3, "waiting", animate: false));
        var bAt = view.PositionOf("consumer-b");
        bridge.Receive(V2(1, "consumer-a", 1, "show-a-01", 3, "waiting"));
        Assert.IsTrue(view.Moving("consumer-a"));
        bridge.Receive(V2(1, "consumer-a", 2, "show-a-02", 1, "entered", animate: false));
        yield return Until(() => !view.Moving("consumer-a"), "A shows its new task", 4f);
        yield return null;
        Assert.IsFalse(Rendered().Any(r => r.Contains("consumer-a|1|show-a-01")), "the replaced walk reported");
        Assert.That(Rendered(), Has.Some.EqualTo("rendered:1:1:consumer-a|2|show-a-02|entered||"));
        Assert.AreEqual("waiting", view.PhaseOf("consumer-b"));
        Assert.AreEqual(bAt, view.PositionOf("consumer-b"));
    }

    // A message of the old scene or of B's old slot moves nothing.
    [UnityTest]
    public IEnumerator OldMessagesMoveNothing()
    {
        bridge.Receive(V2(2, "consumer-b", 3, "show-b-03", 2, "quoted", animate: false));
        yield return null;
        var before = Rendered().Count;
        bridge.Receive(V2(1, "consumer-b", 9, "show-b-09", 5, "waiting"));
        bridge.Receive(V2(2, "consumer-b", 2, "show-b-02", 5, "waiting"));
        yield return null;
        Assert.AreEqual("quoted", view.PhaseOf("consumer-b"));
        Assert.AreEqual(before, Rendered().Count);
        Assert.That(bridge.reports, Has.Some.EqualTo("applied:1:5:consumer-b|Stale"));
        Assert.That(bridge.reports, Has.Some.EqualTo("applied:2:5:consumer-b|Stale"));
    }

    // Back to version 1 (the viewer page, a replay): the scene starts over
    // and one customer is shown, the other dimmed, as before.
    [UnityTest]
    public IEnumerator BackToVersion1()
    {
        bridge.Receive(V2(1, "consumer-a", 1, "show-a-01", 3, "waiting", animate: false));
        bridge.Receive(V2(1, "consumer-b", 1, "show-b-01", 2, "quoted", animate: false));
        bridge.Receive("{\"v\":1,\"gen\":1,\"consumer\":\"consumer-a\",\"task_id\":\"task-a\",\"seq\":2,\"phase\":\"quoted\",\"verdict\":\"\",\"reason\":\"\",\"animate\":false}");
        yield return null;
        Assert.IsFalse(view.Multi);
        Assert.AreEqual("empty", view.PhaseOf("consumer-b"));
        Assert.Less(view.AlphaOf("consumer-b"), 0.9f);
        Assert.AreEqual(view.Point("consumer-a", ShopView.Shelf), view.PositionOf("consumer-a"));
    }

    // UX3: a new scene names its room; the room is reported, and changes
    // only with a new scene. An unknown room, or another room in the same
    // scene, is refused whole: nothing moves or resets.
    [Test]
    public void EachSceneShowsItsRoom()
    {
        Assert.AreEqual(ShopView.DefaultLayout, view.LayoutKey);
        bridge.Receive(V2(1, "consumer-a", 1, "show-a-01", 3, "waiting", animate: false).Replace("{", "{\"layout\":\"cafe\","));
        Assert.That(bridge.reports, Has.Some.EqualTo("layout:1:0:cafe"));
        Assert.AreEqual("cafe", view.LayoutKey);
        Assert.AreEqual("waiting", view.PhaseOf("consumer-a"));
        var texts = Object.FindObjectsByType<TextMesh>(FindObjectsSortMode.None).Select(t => t.text).ToList();
        Assert.That(texts, Has.Some.StartsWith("這場不付款・街角咖啡"));
        Assert.That(texts, Has.Some.EqualTo("取餐區・付款後才交付"));
        Assert.That(texts, Has.None.EqualTo("下載區・付款後才交付"), "the old room's notes are gone");

        bridge.Receive(V2(1, "consumer-b", 1, "show-b-01", 2, "quoted").Replace("{", "{\"layout\":\"mart\","));
        Assert.That(bridge.reports.Last(), Is.EqualTo("applied:1:2:consumer-b|Invalid"));
        bridge.Receive(V2(2, "consumer-b", 1, "show-b-01", 2, "quoted").Replace("{", "{\"layout\":\"zoo\","));
        Assert.That(bridge.reports.Last(), Is.EqualTo("applied:2:2:consumer-b|Invalid"));
        Assert.AreEqual("cafe", view.LayoutKey);
        Assert.AreEqual("waiting", view.PhaseOf("consumer-a"), "a refused room resets nothing");

        bridge.Receive("{\"v\":2,\"scene\":3,\"clear\":true}"); // no layout: the default room
        Assert.That(bridge.reports, Has.Some.EqualTo("layout:3:0:" + ShopView.DefaultLayout));
        Assert.AreEqual(ShopView.DefaultLayout, view.LayoutKey);
        Assert.AreEqual("empty", view.PhaseOf("consumer-a"));
    }
}
