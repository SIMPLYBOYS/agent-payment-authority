// PlayMode tests of the shop scene through its bridge: the real scene, the
// real ShopView, messages as the page sends them.
using System.Collections;
using APA;
using NUnit.Framework;
using UnityEngine;
using UnityEngine.SceneManagement;
using UnityEngine.TestTools;

public class ShopSceneTests
{
    ShopBridge bridge;
    ShopView view;

    static string Msg(int gen, string consumer, string task, int seq, string phase, bool animate) =>
        "{\"v\":1,\"gen\":" + gen + ",\"consumer\":\"" + consumer + "\",\"task_id\":\"" + task + "\",\"seq\":" + seq +
        ",\"phase\":\"" + phase + "\",\"verdict\":\"\",\"reason\":\"\",\"animate\":" + (animate ? "true" : "false") + "}";

    [UnitySetUp]
    public IEnumerator Load()
    {
        yield return SceneManager.LoadSceneAsync("Shop");
        bridge = Object.FindAnyObjectByType<ShopBridge>();
        view = Object.FindAnyObjectByType<ShopView>();
        Assert.NotNull(bridge);
        Assert.NotNull(view);
        for (int i = 0; i < 60 && bridge.reports.Count == 0; i++) yield return null;
        Assert.That(bridge.reports, Has.Some.StartsWith("ready:"));
    }

    [UnityTest]
    public IEnumerator AskWalksToTheCounterOnceAndDenyLeaves()
    {
        bridge.Receive(Msg(1, "consumer-a", "task-a", 3, "waiting", true));
        Assert.AreEqual(1, view.AnimationsStarted);
        Assert.IsTrue(view.Moving("consumer-a"));
        bridge.Receive(Msg(1, "consumer-a", "task-a", 3, "waiting", true)); // fetched again
        Assert.AreEqual(1, view.AnimationsStarted, "a duplicate must not replay the walk");
        float t = 0;
        while (view.Moving("consumer-a") && t < 10f) { t += Time.deltaTime; yield return null; }
        Assert.IsFalse(view.Moving("consumer-a"));
        Assert.Greater(view.PositionOf("consumer-a").x, 7f, "Ask: waiting at the counter");
        Assert.That(bridge.reports, Has.Some.EqualTo("rendered:1:3:waiting|task-a||"));
        Assert.AreEqual("empty", view.PhaseOf("consumer-b"), "B is not moved by A's task");

        bridge.Receive("{\"v\":1,\"gen\":2,\"clear\":true}");
        Assert.AreEqual("empty", view.PhaseOf("consumer-a"), "a new generation clears the old customer at once");
        bridge.Receive("{\"v\":1,\"gen\":2,\"consumer\":\"consumer-b\",\"task_id\":\"task-b\",\"seq\":3,\"phase\":\"denied\",\"verdict\":\"Deny\",\"reason\":\"INSUFFICIENT_BUDGET\",\"animate\":false}");
        Assert.AreEqual("denied", view.PhaseOf("consumer-b"));
        Assert.That(bridge.reports, Has.Some.EqualTo("rendered:2:3:denied|task-b|Deny|INSUFFICIENT_BUDGET"), "the reason the shop shows is reported");
        Assert.Less(view.PositionOf("consumer-b").x, 0f, "Deny: outside, leaving");
        Assert.AreEqual("empty", view.PhaseOf("consumer-a"));
    }

    [UnityTest]
    public IEnumerator LateCallbackOfAnOldGenerationIsDropped()
    {
        bridge.Receive(Msg(1, "consumer-a", "task-a", 3, "waiting", true)); // walking...
        bridge.Receive("{\"v\":1,\"gen\":2,\"clear\":true}");               // ...switched before it arrived
        bridge.Receive(Msg(2, "consumer-b", "task-b", 3, "denied", false));
        yield return new WaitForSeconds(4f);
        Assert.That(bridge.reports, Has.None.StartsWith("rendered:1:"), "no report for the old generation");
        Assert.AreEqual("empty", view.PhaseOf("consumer-a"));
        bridge.Receive(Msg(1, "consumer-a", "task-a", 4, "denied", true));
        Assert.AreEqual("empty", view.PhaseOf("consumer-a"), "a late message of the old generation is ignored");
    }

    [Test]
    public void SaysWhetherItUsesLimeZuOrPlaceholders()
    {
        Assert.That(bridge.reports, Has.Some.EqualTo("ready:-1:-1:" + (view.UsingPlaceholders ? "placeholder" : "limezu")));
    }
}
