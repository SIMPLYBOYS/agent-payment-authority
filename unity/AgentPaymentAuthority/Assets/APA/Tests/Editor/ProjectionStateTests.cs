// EditMode tests of the bridge rules (docs/demo-ui.md section 4).
using APA;
using NUnit.Framework;

public class ProjectionStateTests
{
    static ProjectionMessage M(int gen, string consumer, string task, int seq, string phase, bool animate = true) =>
        new ProjectionMessage { v = ProjectionState.Version, gen = gen, consumer = consumer, task_id = task, seq = seq, phase = phase, animate = animate };

    [Test]
    public void AppliesAnAskAndAnimatesEachSequenceOnce()
    {
        var s = new ProjectionState();
        Assert.AreEqual(Outcome.Applied, s.Apply(M(1, "consumer-a", "task-a", 3, "waiting"), out var anim));
        Assert.IsTrue(anim);
        Assert.AreEqual("waiting", s.Phase);
        Assert.AreEqual(Outcome.Duplicate, s.Apply(M(1, "consumer-a", "task-a", 3, "waiting"), out anim));
        Assert.IsFalse(anim, "the same event fetched again must not animate again");
    }

    [Test]
    public void FirstLoadShowsWithoutAnimation()
    {
        var s = new ProjectionState();
        Assert.AreEqual(Outcome.Applied, s.Apply(M(1, "consumer-b", "task-b", 3, "denied", animate: false), out var anim));
        Assert.IsFalse(anim);
        Assert.AreEqual(Outcome.Duplicate, s.Apply(M(1, "consumer-b", "task-b", 3, "denied", animate: true), out anim));
        Assert.IsFalse(anim, "a stored snapshot shown once is not news later");
    }

    [Test]
    public void OlderGenerationAndOlderSequenceAreDropped()
    {
        var s = new ProjectionState();
        s.Apply(M(2, "consumer-a", "task-a", 3, "waiting"), out _);
        Assert.AreEqual(Outcome.Stale, s.Apply(M(1, "consumer-b", "task-b", 9, "denied"), out _));
        Assert.AreEqual("consumer-a", s.Consumer);
        Assert.AreEqual(Outcome.Older, s.Apply(M(2, "consumer-a", "task-a", 2, "quoted"), out _));
        Assert.AreEqual("waiting", s.Phase, "a late older snapshot must not move the state back");
    }

    [Test]
    public void NewGenerationClearsEverything()
    {
        var s = new ProjectionState();
        s.Apply(M(1, "consumer-a", "task-a", 3, "waiting"), out _);
        var clear = new ProjectionMessage { v = ProjectionState.Version, gen = 2, clear = true };
        Assert.AreEqual(Outcome.Cleared, s.Apply(clear, out _));
        Assert.IsNull(s.Consumer);
        Assert.AreEqual("empty", s.Phase);
        Assert.AreEqual(Outcome.Applied, s.Apply(M(2, "consumer-b", "task-b", 3, "denied"), out var anim));
        Assert.IsTrue(anim, "sequence 3 of a new generation is a new event to show");
    }

    [Test]
    public void OneTaskPerGenerationAndOnlyKnownConsumers()
    {
        var s = new ProjectionState();
        s.Apply(M(1, "consumer-a", "task-a", 3, "waiting"), out _);
        Assert.AreEqual(Outcome.Invalid, s.Apply(M(1, "consumer-b", "task-b", 4, "denied"), out _), "no customer switch inside a generation");
        Assert.AreEqual(Outcome.Invalid, s.Apply(M(1, "consumer-a", "task-a-next", 4, "waiting"), out _));
        Assert.AreEqual(Outcome.Invalid, s.Apply(M(3, "consumer-c", "task-c", 1, "waiting"), out _));
        Assert.AreEqual("consumer-a", s.Consumer);
    }

    [Test]
    public void BadInputIsInvalidData()
    {
        var s = new ProjectionState();
        Assert.AreEqual(Outcome.Invalid, s.Apply(ProjectionState.Parse("not json"), out _));
        Assert.AreEqual(Outcome.Invalid, s.Apply(ProjectionState.Parse("{\"v\":2,\"gen\":1}"), out _), "another bridge version");
        Assert.IsNull(ProjectionState.Parse(new string('x', 5000)));
        var m = ProjectionState.Parse("{\"v\":1,\"gen\":1,\"consumer\":\"consumer-a\",\"task_id\":\"task-a\",\"seq\":1,\"phase\":\"<script>pay()</script>\",\"verdict\":\"Settled\"}");
        Assert.AreEqual(Outcome.Applied, s.Apply(m, out _));
        Assert.AreEqual("unknown", s.Phase, "an unknown phase is shown as unknown, never as anything paid");
    }
}
