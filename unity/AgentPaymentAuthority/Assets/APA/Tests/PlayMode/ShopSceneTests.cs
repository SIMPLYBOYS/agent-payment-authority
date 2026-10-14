// PlayMode tests of the shop scene through its bridge: the real scene, the
// real ShopView, messages as the page sends them.
using System.Collections;
using System.Collections.Generic;
using System.Linq;
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

    // D2: with cause_seq and task_end (docs/demo-presentation-design.md 16.4).
    static string D2(int gen, string consumer, int seq, string phase, int cause, string end = "", bool animate = true, string reason = "") =>
        "{\"v\":1,\"gen\":" + gen + ",\"consumer\":\"" + consumer + "\",\"task_id\":\"task-" + consumer.Substring(9) + "\",\"seq\":" + seq +
        ",\"phase\":\"" + phase + "\",\"verdict\":\"\",\"reason\":\"" + reason + "\",\"animate\":" + (animate ? "true" : "false") +
        ",\"cause_seq\":" + cause + ",\"task_end\":\"" + end + "\"}";

    List<string> Rendered() => bridge.reports.Where(r => r.StartsWith("rendered:")).ToList();

    IEnumerator Arrive(string consumer, float limit = 4f)
    {
        float t = 0;
        while (view.Moving(consumer) && t < limit) { t += Time.deltaTime; yield return null; }
        Assert.IsFalse(view.Moving(consumer), consumer + " did not arrive in " + limit + " s");
    }

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
        Assert.NotNull(bridge);
        Assert.NotNull(view);
        for (int i = 0; i < 60 && bridge.reports.Count == 0; i++) yield return null;
        Assert.That(bridge.reports, Has.Some.StartsWith("ready:"));
    }

    // The scene's own text says nothing of where the agent's actions come
    // from: the same build shows the fake demo and the history replay of a
    // recorded real run (docs/cli-poc.md 54.2); the page says which.
    [Test]
    public void SceneTextIsNeutralAboutTheAgent()
    {
        var texts = Object.FindObjectsByType<TextMesh>(FindObjectsSortMode.None).Select(t => t.text).ToList();
        Assert.That(texts, Has.Some.StartsWith("M1 / no payment"));
        Assert.That(texts, Has.Some.EqualTo("Customer A"));
        Assert.That(texts, Has.Some.EqualTo("Customer B"));
        foreach (var t in texts)
        {
            StringAssert.DoesNotContain("fake", t.ToLowerInvariant());
            StringAssert.DoesNotContain("sim agent", t.ToLowerInvariant());
            StringAssert.DoesNotContain("real", t.ToLowerInvariant());
        }
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

    // ---- D2 (docs/demo-presentation-design.md 16.9 items 7, 9, 12, 13) ----

    // A customer's feet: the lowest 8 px rows of every LimeZu frame span 28 px.
    const float FeetHalf = 14f / 32f, FeetHigh = 8f / 32f;

    static Rect Feet(Vector2 p) => Rect.MinMaxRect(p.x - FeetHalf, p.y, p.x + FeetHalf, p.y + FeetHigh);

    // The props' drawn (non-transparent) pixels, in room units, where the
    // room's layout placed them: each sprite's rect of its file (a
    // placeholder is its whole box).
    List<Rect> PropRects()
    {
        var rects = new List<Rect>();
        foreach (var (_, at, sprite) in view.Placed)
        {
            var sr = sprite.rect;
            int w = (int)sr.width, h = (int)sr.height, x0 = 0, x1 = w - 1, y0 = 0, y1 = h - 1;
#if UNITY_EDITOR
            var path = UnityEditor.AssetDatabase.GetAssetPath(sprite.texture);
            if (path != "")
            {
                var copy = new Texture2D(2, 2);
                copy.LoadImage(System.IO.File.ReadAllBytes(path));
                var px = copy.GetPixels32();
                x0 = w; x1 = -1; y0 = h; y1 = -1;
                for (int y = 0; y < h; y++) for (int x = 0; x < w; x++)
                    if (px[((int)sr.y + y) * copy.width + (int)sr.x + x].a > 0) { x0 = Mathf.Min(x0, x); x1 = Mathf.Max(x1, x); y0 = Mathf.Min(y0, y); y1 = Mathf.Max(y1, y); }
            }
#endif
            rects.Add(Rect.MinMaxRect(at.x - w / 64f + x0 / 32f, at.y + y0 / 32f, at.x - w / 64f + (x1 + 1) / 32f, at.y + (y1 + 1) / 32f));
        }
        return rects;
    }

    // UX3: in every room, each customer's corridor (with B's version 2 step
    // aside too) is horizontal or vertical and its feet never touch a prop.
    [Test]
    public void CorridorsKeepClearInEveryRoom()
    {
        foreach (var key in view.LayoutKeys.ToList())
        {
            Assert.IsTrue(view.UseLayout(key));
            var props = PropRects();
            Assert.IsNotEmpty(props, key);
            foreach (var multi in new[] { false, true })
            {
                view.Multi = multi;
                foreach (var who in new[] { "consumer-a", "consumer-b" })
                    for (int e = 0; e < ShopView.Lines.GetLength(0); e++)
                        CheckRoute(key + " " + who + (multi ? " v2" : " v1"), new List<Vector2> { view.P(who, ShopView.Lines[e, 0]), view.P(who, ShopView.Lines[e, 1]) }, props);
            }
            view.Multi = false;
        }
        Debug.Log("APA ShopSceneTests: corridors clear in rooms " + string.Join(",", view.LayoutKeys));
    }

    // Every line of a route is horizontal or vertical, and the feet swept
    // along the whole line never touch a prop.
    static void CheckRoute(string what, List<Vector2> pts, List<Rect> props)
    {
        for (int i = 1; i < pts.Count; i++)
        {
            Vector2 a = pts[i - 1], b = pts[i];
            Assert.IsTrue(Mathf.Abs(a.x - b.x) < 1e-4f || Mathf.Abs(a.y - b.y) < 1e-4f, what + ": " + a + " -> " + b + " is not horizontal or vertical");
            var swept = Rect.MinMaxRect(Mathf.Min(a.x, b.x) - FeetHalf, Mathf.Min(a.y, b.y), Mathf.Max(a.x, b.x) + FeetHalf, Mathf.Max(a.y, b.y) + FeetHigh);
            foreach (var r in props) Assert.IsFalse(swept.Overlaps(r), what + ": " + a + " -> " + b + " crosses a prop at " + r);
        }
    }

    [UnityTest]
    public IEnumerator RoutesKeepClearOfPropsInFourDirectionsWithAsciiLabels()
    {
        var facings = new HashSet<string>();
        var labels = new HashSet<string>();
        int g = 0; // each room plays from a new generation
        foreach (var key in view.LayoutKeys.ToList())
        {
            Assert.IsTrue(view.UseLayout(key));
            var props = PropRects();
            Assert.IsNotEmpty(props, key);
            var plays = new[]
            {
                ("", "{\"v\":1,\"gen\":" + (g + 1) + ",\"clear\":true}"),
                ("consumer-a", D2(g + 1, "consumer-a", 1, "entered", 1)), ("consumer-a", D2(g + 1, "consumer-a", 2, "quoted", 2)),
                ("consumer-a", D2(g + 1, "consumer-a", 3, "waiting", 3)), ("consumer-a", D2(g + 1, "consumer-a", 4, "expired", 4)),
                ("", "{\"v\":1,\"gen\":" + (g + 2) + ",\"clear\":true}"),
                ("consumer-b", D2(g + 2, "consumer-b", 1, "entered", 1)), ("consumer-b", D2(g + 2, "consumer-b", 3, "waiting", 3)),
                ("consumer-b", D2(g + 2, "consumer-b", 6, "waiting", 6)), ("consumer-b", D2(g + 2, "consumer-b", 9, "denied", 9, reason: "INSUFFICIENT_BUDGET")),
                ("", "{\"v\":1,\"gen\":" + (g + 3) + ",\"clear\":true}"),
                ("consumer-a", D2(g + 3, "consumer-a", 3, "waiting", 3)), ("consumer-a", D2(g + 3, "consumer-a", 4, "withdrawn", 4)),
                ("", "{\"v\":1,\"gen\":" + (g + 4) + ",\"clear\":true}"),
                ("consumer-b", D2(g + 4, "consumer-b", 3, "waiting", 3)), ("consumer-b", D2(g + 4, "consumer-b", 4, "expired", 4)),
            };
            g += 4;
            foreach (var (who, json) in plays)
            {
                bridge.Receive(json);
                if (who == "") continue;
                CheckRoute(who + " " + json, view.RouteOf(who), props);
                float t = 0;
                while (view.Moving(who) && t < 4f)
                {
                    var feet = Feet(view.PositionOf(who));
                    foreach (var r in props) Assert.IsFalse(feet.Overlaps(r), who + " stands on a prop at " + view.PositionOf(who));
                    facings.Add(view.FacingOf(who));
                    labels.Add(view.LabelOf(who));
                    t += Time.deltaTime;
                    yield return null;
                }
                Assert.IsFalse(view.Moving(who));
                labels.Add(view.LabelOf(who));
            }
            Assert.Less(view.PositionOf("consumer-b").x, 0f, key + ": an expired Ask ends outside");
        }
        CollectionAssert.IsSupersetOf(facings, new[] { "right", "up", "left", "down" }, "four walking directions");
        foreach (var l in labels)
        {
            foreach (var ch in l) Assert.Less((int)ch, 128, "ASCII only: " + l);
            if (l.Contains("browsing") || l.Contains("to counter")) StringAssert.StartsWith("[anim] ", l, "a display segment says so");
        }
        CollectionAssert.IsSupersetOf(labels, new[]
        {
            "entered (#1)", "request proposed (#2)", "[anim] browsing - no event", "[anim] to counter - quote bound in request",
            "ASK (#3) - waiting for a human", "ask expired (#4) - leaving", "ASK (#6) - waiting for a human",
            "DENY (#9, INSUFFICIENT_BUDGET) - leaving", "ask withdrawn (#4) - leaving",
        });
        Debug.Log("APA ShopSceneTests: routes clear in rooms " + string.Join(",", view.LayoutKeys) + "; facings " + string.Join(",", facings) + "; labels " + string.Join(" | ", labels));
    }

    // 16.5 rule 4 and item 7: from outside to the counter is the longest
    // move; it is sped up and still arrives within 2.5 s.
    [UnityTest]
    public IEnumerator TheLongestMoveArrivesWithinTheLimit()
    {
        foreach (var who in new[] { "consumer-a", "consumer-b" })
        {
            int gen = who == "consumer-a" ? 1 : 2;
            bridge.Receive("{\"v\":1,\"gen\":" + gen + ",\"clear\":true}");
            float start = Time.realtimeSinceStartup;
            bridge.Receive(D2(gen, who, 3, "waiting", 3));
            var want = "rendered:" + gen + ":3:waiting|task-" + who.Substring(9) + "||";
            yield return Until(() => bridge.reports.Contains(want), who + " rendered", 4f);
            float took = Time.realtimeSinceStartup - start;
            Assert.LessOrEqual(took, 2.5f + 0.1f, who + " took " + took);
            Assert.Greater(took, 1.5f, who + ": walked, not jumped");
            Assert.AreEqual(view.Point(who, ShopView.Counter), view.PositionOf(who));
            Assert.AreEqual("right", view.FacingOf(who), "at the counter it faces the counter");
        }
    }

    // 16.5 rule 1: animate=false jumps; the replaced move never reports.
    [UnityTest]
    public IEnumerator NotAnimatedJumpsAndTheReplacedMoveNeverReports()
    {
        bridge.Receive(D2(1, "consumer-a", 3, "waiting", 3));
        yield return new WaitForSeconds(0.4f);
        Assert.IsTrue(view.Moving("consumer-a"));
        bridge.Receive(D2(1, "consumer-a", 4, "expired", 4, animate: false));
        Assert.IsFalse(view.Moving("consumer-a"));
        Assert.AreEqual(view.Point("consumer-a", ShopView.Out), view.PositionOf("consumer-a"));
        Assert.AreEqual("rendered:1:4:expired|task-a||", Rendered().Last());
        yield return new WaitForSeconds(3f);
        Assert.That(bridge.reports, Has.None.StartsWith("rendered:1:3:"), "the replaced move is void");
    }

    // 16.5 rule 2: the new end is ahead on the way: twice the speed.
    [UnityTest]
    public IEnumerator AnEndAheadOnTheWayIsReachedAtTwiceTheSpeed()
    {
        bridge.Receive(D2(1, "consumer-a", 1, "entered", 1));
        yield return Arrive("consumer-a");
        bridge.Receive(D2(1, "consumer-a", 3, "waiting", 3));
        yield return Until(() => view.LabelOf("consumer-a").StartsWith("[anim] to counter"), "left the shelf");
        var route = view.RouteOf("consumer-a");
        float len = 0;
        for (int i = 1; i < route.Count; i++) len += (route[i] - route[i - 1]).magnitude;
        float dl = view.DeadlineOf("consumer-a"), start = Time.realtimeSinceStartup;
        bridge.Receive(D2(1, "consumer-a", 4, "allowed_unpaid", 0)); // another phase, its cause unknown: no new purchase
        Assert.Greater(view.DeadlineOf("consumer-a"), dl, "a new target starts its own 2.5 s");
        yield return Arrive("consumer-a");
        float took = Time.realtimeSinceStartup - start;
        Assert.Less(took, len / (ShopView.Walk * 1.5f), "twice the walking speed over " + len + " units, took " + took);
        Assert.AreEqual("ALLOW - no payment capability", view.LabelOf("consumer-a"));
        Assert.AreEqual(new[] { "rendered:1:1:entered|task-a||", "rendered:1:4:allowed_unpaid|task-a||" }, Rendered().ToArray());
    }

    // 16.5 rule 3: an end off the way: the corridor again, at the usual pace.
    [UnityTest]
    public IEnumerator AnEndOffTheWayTakesTheCorridorAgain()
    {
        var props = PropRects();
        bridge.Receive(D2(1, "consumer-b", 3, "waiting", 3));
        yield return Until(() => view.LabelOf("consumer-b").StartsWith("[anim] to counter"), "on the way to the counter");
        yield return new WaitForSeconds(0.2f);
        float start = Time.realtimeSinceStartup;
        bridge.Receive(D2(1, "consumer-b", 4, "expired", 4));
        var route = view.RouteOf("consumer-b");
        CheckRoute("expired on the way", route, props);
        Assert.AreEqual(view.Point("consumer-b", ShopView.Out), route.Last());
        yield return Arrive("consumer-b");
        Assert.LessOrEqual(Time.realtimeSinceStartup - start, 2.5f + 0.1f);
        Assert.AreEqual(new[] { "rendered:1:4:expired|task-b||" }, Rendered().ToArray(), "only the newest, never the replaced #3");
    }

    // 16.5 rule 4 and item 12.2: a move that cannot make it jumps to its end
    // 2.5 s after the message that set it; updates on the way do not move
    // that limit and are not reported early.
    [UnityTest]
    public IEnumerator TheLimitIsKeptWhateverUpdatesComeOnTheWay()
    {
        view.Pace = 0.2f;
        try
        {
            float start = Time.realtimeSinceStartup;
            bridge.Receive(D2(1, "consumer-a", 3, "waiting", 3));
            float dl = view.DeadlineOf("consumer-a");
            Assert.AreEqual(start + ShopView.MaxTransition, dl, 0.05f);
            yield return new WaitForSeconds(1.2f);
            bridge.Receive(D2(1, "consumer-a", 4, "waiting", 3));
            bridge.Receive(D2(1, "consumer-a", 5, "waiting", 3, "timed_out"));
            Assert.AreEqual(dl, view.DeadlineOf("consumer-a"), "updates keep the limit");
            Assert.IsEmpty(Rendered(), "not reported on the way");
            yield return Arrive("consumer-a");
            float took = Time.realtimeSinceStartup - start;
            Assert.AreEqual(2.5f, took, 0.15f, "jumped at the limit");
            Assert.AreEqual(view.Point("consumer-a", ShopView.Counter), view.PositionOf("consumer-a"));
            Assert.AreEqual(new[] { "rendered:1:5:waiting|task-a||" }, Rendered().ToArray(), "once, for the newest sequence");
            Assert.AreEqual("CLI timeout - ask still pending", view.LabelOf("consumer-a"));
        }
        finally { view.Pace = 1f; }
    }

    // Items 12.1, 12.3, 13.2 and 13.3: the same target with a newer sequence
    // on the way changes the target's data only; once arrived it is reported
    // at once, without a move.
    [UnityTest]
    public IEnumerator TheSameTargetOnTheWayAndAfterArriving()
    {
        bridge.Receive(D2(1, "consumer-a", 3, "waiting", 3));
        int started = view.AnimationsStarted;
        yield return new WaitForSeconds(0.3f);
        bridge.Receive(D2(1, "consumer-a", 4, "waiting", 0));   // 3 -> 0
        Assert.AreEqual("ASK - waiting for a human", bridge.state.T.Label, "no stale #3");
        bridge.Receive(D2(1, "consumer-a", 5, "waiting", 3));   // 0 -> 3
        Assert.AreEqual("ASK (#3) - waiting for a human", bridge.state.T.Label);
        Assert.AreEqual(started, view.AnimationsStarted, "nothing played again");
        Assert.IsTrue(view.Moving("consumer-a"));
        Assert.IsEmpty(Rendered());
        yield return Arrive("consumer-a");
        Assert.AreEqual(new[] { "rendered:1:5:waiting|task-a||" }, Rendered().ToArray());
        Assert.AreEqual("ASK (#3) - waiting for a human", view.LabelOf("consumer-a"));

        var at = view.PositionOf("consumer-a");
        bridge.Receive(D2(1, "consumer-a", 6, "waiting", 3));   // another purchase's event
        Assert.AreEqual("rendered:1:6:waiting|task-a||", Rendered().Last(), "arrived: reported at once");
        bridge.Receive(D2(1, "consumer-a", 7, "waiting", 3, "timed_out"));
        Assert.AreEqual("rendered:1:7:waiting|task-a||", Rendered().Last());
        Assert.AreEqual("CLI timeout - ask still pending", view.LabelOf("consumer-a"), "11.1: stays at the counter");
        Assert.IsFalse(view.Moving("consumer-a"));
        Assert.AreEqual(at, view.PositionOf("consumer-a"));
        Assert.AreEqual(started, view.AnimationsStarted);
    }

    // Item 13.1 and 13.3: 0 -> 3 fills in on the way; then 3 -> 6 is a new
    // purchase: it browses again and only #6 is reported.
    [UnityTest]
    public IEnumerator ZeroThreeSixOnTheWay()
    {
        bridge.Receive(D2(1, "consumer-b", 3, "waiting", 0));
        int started = view.AnimationsStarted;
        yield return new WaitForSeconds(0.3f);
        bridge.Receive(D2(1, "consumer-b", 4, "waiting", 3));
        Assert.AreEqual(started, view.AnimationsStarted, "0 -> 3 is no new purchase");
        Assert.AreEqual("ASK (#3) - waiting for a human", bridge.state.T.Label);
        yield return new WaitForSeconds(0.3f);
        bridge.Receive(D2(1, "consumer-b", 6, "waiting", 6));
        Assert.AreEqual(2, bridge.state.Rule);
        Assert.AreEqual(started + 1, view.AnimationsStarted, "3 -> 6 browses again");
        yield return Arrive("consumer-b");
        Assert.AreEqual("ASK (#6) - waiting for a human", view.LabelOf("consumer-b"));
        Assert.AreEqual(new[] { "rendered:1:6:waiting|task-b||" }, Rendered().ToArray());
    }

    // A slow browser: Unity caps a frame's deltaTime, so game time falls
    // behind the real clock (seen in headless Chrome under load, bc79677).
    // The 2.5 s limit is on the real clock and still holds.
    [UnityTest]
    public IEnumerator TheLimitHoldsOnSlowFrames()
    {
        float cap = Time.maximumDeltaTime;
        Time.maximumDeltaTime = 0.05f;
        try
        {
            float start = Time.realtimeSinceStartup;
            bridge.Receive(D2(1, "consumer-a", 3, "waiting", 3));
            for (int i = 0; i < 40 && view.Moving("consumer-a"); i++)
            {
                System.Threading.Thread.Sleep(200); // a 200 ms frame; deltaTime says 50 ms
                yield return null;
            }
            float took = Time.realtimeSinceStartup - start;
            Assert.IsFalse(view.Moving("consumer-a"));
            Assert.LessOrEqual(took, 2.5f + 0.45f, "real seconds, frames of 200 ms");
            Assert.AreEqual(view.Point("consumer-a", ShopView.Counter), view.PositionOf("consumer-a"));
            Assert.AreEqual(new[] { "rendered:1:3:waiting|task-a||" }, Rendered().ToArray());
        }
        finally { Time.maximumDeltaTime = cap; }
    }

    // Review of d7070af: a clear, or another customer's generation, while a
    // customer is browsing leaves no "[anim]" label behind on it.
    [UnityTest]
    public IEnumerator AClearDuringABrowseLeavesNoLabel()
    {
        bridge.Receive(D2(1, "consumer-a", 3, "waiting", 3));
        Assert.IsTrue(view.Moving("consumer-a"));
        StringAssert.StartsWith("[anim]", view.LabelOf("consumer-a"));
        bridge.Receive("{\"v\":1,\"gen\":2,\"clear\":true}");
        Assert.IsFalse(view.Moving("consumer-a"));
        Assert.AreEqual("empty", view.PhaseOf("consumer-a"));
        Assert.AreEqual("", view.LabelOf("consumer-a"), "no label left on the cleared customer");
        yield return null;
        Assert.AreEqual("", view.LabelOf("consumer-a"));
    }

    [UnityTest]
    public IEnumerator AnotherCustomersGenerationDuringABrowseLeavesNoLabel()
    {
        bridge.Receive(D2(1, "consumer-a", 3, "waiting", 3));
        StringAssert.StartsWith("[anim]", view.LabelOf("consumer-a"));
        bridge.Receive(D2(2, "consumer-b", 3, "denied", 3, animate: false, reason: "INSUFFICIENT_BUDGET")); // no clear first
        Assert.IsFalse(view.Moving("consumer-a"));
        Assert.AreEqual("empty", view.PhaseOf("consumer-a"));
        Assert.That(bridge.reports, Has.Some.StartsWith("rendered:2:3:denied"));
        Assert.AreEqual("", view.LabelOf("consumer-a"), "the inactive customer does not say it is browsing");
        Assert.AreEqual("DENY (#3, INSUFFICIENT_BUDGET) - leaving", view.LabelOf("consumer-b"));
        yield return null;
        Assert.AreEqual("", view.LabelOf("consumer-a"));
    }
}
