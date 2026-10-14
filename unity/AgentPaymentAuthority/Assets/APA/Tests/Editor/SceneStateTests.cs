// EditMode tests of the version 2 rule (docs/multi-agent-demo-design.md
// 5.2): one scene generation, a slot per customer; one customer's message
// never moves, clears or completes the other.
using APA;
using NUnit.Framework;

public class SceneStateTests
{
    static ProjectionMessage M(int scene, string consumer, int slot, string task, int seq, string phase, bool animate = true) =>
        ProjectionState.Parse("{\"v\":2,\"scene\":" + scene + ",\"consumer\":\"" + consumer + "\",\"slot\":" + slot + ",\"task_id\":\"" + task +
            "\",\"seq\":" + seq + ",\"phase\":\"" + phase + "\",\"verdict\":\"\",\"reason\":\"\",\"animate\":" + (animate ? "true" : "false") + "}");

    static Outcome Apply(SceneState s, ProjectionMessage m) => s.Apply(m, out _, out _, out _);


    [Test]
    public void TwoCustomersInOneScene()
    {
        var s = new SceneState();
        Assert.AreEqual(Outcome.Applied, s.Apply(M(1, "consumer-a", 1, "show-a-01", 2, "quoted"), out bool animA, out bool resetScene, out _));
        Assert.IsTrue(animA);
        Assert.IsTrue(resetScene); // the first scene
        Assert.AreEqual(Outcome.Applied, s.Apply(M(1, "consumer-b", 1, "show-b-01", 2, "quoted"), out bool animB, out resetScene, out bool resetSlot));
        Assert.IsTrue(animB, "B's sequence 2 is its own: A's does not suppress it");
        Assert.IsFalse(resetScene);
        Assert.IsTrue(resetSlot);
        Assert.AreEqual("quoted", s.Of("consumer-a").T.Phase);
        Assert.AreEqual("show-b-01", s.Of("consumer-b").TaskId);
        // A moves on; B stays.
        Assert.AreEqual(Outcome.Applied, Apply(s, M(1, "consumer-a", 1, "show-a-01", 3, "waiting")));
        Assert.AreEqual(2, s.Of("consumer-b").T.Seq);
    }

    [Test]
    public void ASlotChangeResetsThatCustomerOnly()
    {
        var s = new SceneState();
        Apply(s, M(1, "consumer-a", 1, "show-a-01", 3, "waiting"));
        Apply(s, M(1, "consumer-b", 1, "show-b-01", 2, "quoted"));
        Assert.AreEqual(Outcome.Applied, s.Apply(M(1, "consumer-a", 2, "show-a-02", 1, "entered"), out _, out bool resetScene, out bool resetSlot));
        Assert.IsFalse(resetScene);
        Assert.IsTrue(resetSlot);
        Assert.AreEqual("show-a-02", s.Of("consumer-a").TaskId);
        Assert.AreEqual("quoted", s.Of("consumer-b").T.Phase, "B untouched");
        // A's old slot is stale now; B's slot 1 still holds.
        Assert.AreEqual(Outcome.Stale, Apply(s, M(1, "consumer-a", 1, "show-a-01", 4, "expired")));
        Assert.AreEqual(Outcome.Applied, Apply(s, M(1, "consumer-b", 1, "show-b-01", 3, "waiting")));
    }

    [Test]
    public void ClearsOfTheSceneAndOfASlot()
    {
        var s = new SceneState();
        Apply(s, M(1, "consumer-a", 1, "show-a-01", 3, "waiting"));
        Apply(s, M(1, "consumer-b", 1, "show-b-01", 2, "quoted"));
        // A clear of B's slot: B only.
        Assert.AreEqual(Outcome.Cleared, s.Apply(ProjectionState.Parse("{\"v\":2,\"scene\":1,\"consumer\":\"consumer-b\",\"slot\":2,\"clear\":true}"), out _, out bool rs, out bool rsl));
        Assert.IsFalse(rs);
        Assert.IsTrue(rsl);
        Assert.AreEqual("empty", s.Of("consumer-b").T.Phase);
        Assert.AreEqual("waiting", s.Of("consumer-a").T.Phase);
        Assert.AreEqual(Outcome.Duplicate, Apply(s, ProjectionState.Parse("{\"v\":2,\"scene\":1,\"consumer\":\"consumer-b\",\"slot\":2,\"clear\":true}")));
        // A scene clear: everyone.
        Assert.AreEqual(Outcome.Cleared, s.Apply(ProjectionState.Parse("{\"v\":2,\"scene\":2,\"clear\":true}"), out _, out rs, out _));
        Assert.IsTrue(rs);
        Assert.AreEqual("empty", s.Of("consumer-a").T.Phase);
        Assert.AreEqual(Outcome.Duplicate, Apply(s, ProjectionState.Parse("{\"v\":2,\"scene\":2,\"clear\":true}")));
        Assert.AreEqual(Outcome.Stale, Apply(s, M(1, "consumer-a", 9, "show-a-09", 1, "entered")), "an older scene");
    }

    [Test]
    public void WhatIsRefused()
    {
        var s = new SceneState();
        Apply(s, M(1, "consumer-a", 1, "show-a-01", 3, "waiting"));
        Assert.AreEqual(Outcome.Invalid, Apply(s, M(1, "consumer-a", 1, "show-a-02", 4, "waiting")), "another task in one slot generation");
        Assert.AreEqual(Outcome.Older, Apply(s, M(1, "consumer-a", 1, "show-a-01", 2, "quoted")));
        Assert.AreEqual(Outcome.Duplicate, Apply(s, M(1, "consumer-a", 1, "show-a-01", 3, "waiting")));
        Assert.AreEqual(Outcome.Invalid, Apply(s, M(1, "consumer-c", 1, "show-c-01", 1, "entered")), "an unknown customer");
        Assert.AreEqual(Outcome.Invalid, Apply(s, M(1, "consumer-a", -1, "show-a-01", 4, "waiting")), "no slot");
        Assert.AreEqual(Outcome.Invalid, Apply(s, ProjectionState.Parse("{\"v\":1,\"gen\":5,\"consumer\":\"consumer-a\",\"task_id\":\"t\",\"seq\":1,\"phase\":\"entered\"}")), "a version 1 message");
        // Invalid before any reset: a newer scene with an unknown customer clears nothing.
        Assert.AreEqual(Outcome.Invalid, Apply(s, M(9, "consumer-x", 1, "t", 1, "entered")));
        Assert.AreEqual(1, s.Scene);
        Assert.AreEqual("waiting", s.Of("consumer-a").T.Phase);
    }

    // UX3: a scene generation has one layout; another room needs a new
    // scene, and a mismatch is refused whole (nothing resets).
    [Test]
    public void OneLayoutPerScene()
    {
        var s = new SceneState();
        var cafe = M(1, "consumer-a", 1, "show-a-01", 2, "quoted");
        cafe.layout = "cafe";
        Assert.AreEqual(Outcome.Applied, Apply(s, cafe));
        Assert.AreEqual("cafe", s.Layout);
        var mart = M(1, "consumer-b", 1, "show-b-01", 2, "quoted");
        mart.layout = "mart";
        Assert.AreEqual(Outcome.Invalid, s.Apply(mart, out _, out bool rs, out bool rsl));
        Assert.IsFalse(rs || rsl);
        Assert.AreEqual("empty", s.Of("consumer-b").T.Phase);
        var next = ProjectionState.Parse("{\"v\":2,\"scene\":2,\"clear\":true,\"layout\":\"mart\"}");
        Assert.AreEqual(Outcome.Cleared, s.Apply(next, out _, out rs, out _));
        Assert.IsTrue(rs);
        Assert.AreEqual("mart", s.Layout);
        mart.scene = 2;
        Assert.AreEqual(Outcome.Applied, Apply(s, mart));
    }
}
