// EditMode tests of the D2 transition table (docs/demo-presentation-design.md
// 16.4, 16.9 items 5, 10, 11, 13). The cases are the shared vectors that
// web/adapter.js is tested with too, so the two tables cannot drift apart.
using System;
using System.IO;
using APA;
using NUnit.Framework;
using UnityEngine;

public class SceneRulesTests
{
    [Serializable] public class VMsg { public string phase; public int cause_seq; public string task_end; public int seq; public string reason; }
    [Serializable] public class VStep { public VMsg m; public int rule; public bool browse; public string label; public int cause; public string end; }
    [Serializable] public class VCase { public string name; public VStep[] steps; }
    [Serializable] public class VFile { public VCase[] cases; }

    static string Json(int seq, string phase, string extra) =>
        "{\"v\":1,\"gen\":1,\"consumer\":\"consumer-a\",\"task_id\":\"task-a\",\"seq\":" + seq + ",\"phase\":\"" + phase + "\",\"verdict\":\"Ask\",\"reason\":\"HUMAN_APPROVAL_REQUIRED\",\"animate\":true" + extra + "}";

    [Test]
    public void SharedVectorsGiveTheSameRulesAndLabels()
    {
        var path = Path.Combine(Application.dataPath, "../../../testdata/demo/scene-transitions.json");
        var file = JsonUtility.FromJson<VFile>(File.ReadAllText(path));
        Assert.Greater(file.cases.Length, 10);
        int steps = 0;
        foreach (var c in file.cases)
        {
            var s = new ProjectionState();
            foreach (var st in c.steps)
            {
                var m = new ProjectionMessage { v = 1, gen = 1, consumer = "consumer-a", task_id = "task-a", seq = st.m.seq, phase = st.m.phase, cause_seq = st.m.cause_seq, task_end = st.m.task_end, reason = st.m.reason, animate = true };
                Assert.AreEqual(Outcome.Applied, s.Apply(m, out _), c.name);
                Assert.AreEqual(st.rule, s.Rule, c.name + " #" + st.m.seq + " rule");
                Assert.AreEqual(st.browse, s.Browse, c.name + " #" + st.m.seq + " browse");
                Assert.AreEqual(st.label, s.T.Label, c.name + " #" + st.m.seq + " label");
                Assert.AreEqual(st.cause, s.T.Cause, c.name + " #" + st.m.seq + " cause"); // the label no longer shows it (gap review B3)
                Assert.AreEqual(st.end, s.T.End, c.name + " #" + st.m.seq + " end");
                steps++;
            }
        }
        Debug.Log("APA SceneRulesTests: " + file.cases.Length + " cases, " + steps + " steps");
    }

    // Item 5: without the two fields, or with bad ones, the shop falls back
    // to plan A: the same phase never browses again and no number is shown.
    [Test]
    public void MissingOrBadFieldsAreUnknown()
    {
        var s = new ProjectionState();
        Assert.AreEqual(Outcome.Applied, s.Apply(ProjectionState.Parse(Json(3, "waiting", "")), out _), "an old page's message");
        Assert.AreEqual(0, s.T.Cause);
        Assert.AreEqual("", s.T.End);
        Assert.AreEqual("超過自動範圍，等核准", s.T.Label);
        string[] bad = { ",\"cause_seq\":-1", ",\"cause_seq\":99", ",\"cause_seq\":0", ",\"task_end\":\"paid\"", ",\"task_end\":\"\"" };
        int seq = 3;
        foreach (var extra in bad)
        {
            Assert.AreEqual(Outcome.Applied, s.Apply(ProjectionState.Parse(Json(++seq, "waiting", extra)), out _), extra);
            Assert.AreEqual(4, s.Rule, extra + ": the same place, nothing played again");
            Assert.AreEqual(0, s.T.Cause, extra);
            Assert.AreEqual("", s.T.End, extra);
            Assert.AreEqual("超過自動範圍，等核准", s.T.Label, extra);
        }
    }

    // A value of the wrong JSON type, or written twice: JsonUtility would
    // read "3" or 3.5 as 3; Parse keeps neither, so no number shows that was
    // not sent as an integer, and the message is still shown (plan A).
    [Test]
    public void WrongTypesAreUnknownNeverANumber()
    {
        string[] wrong = { ",\"cause_seq\":\"3\"", ",\"cause_seq\":3.5", ",\"cause_seq\":3e0", ",\"cause_seq\":true", ",\"cause_seq\":[3]",
            ",\"cause_seq\":3,\"cause_seq\":4", ",\"task_end\":5", ",\"task_end\":[\"timed_out\"]", ",\"task_end\":\"timed_out\",\"task_end\":\"\"",
            ",\"note\":\"\\\"cause_seq\\\":3\"" };
        foreach (var extra in wrong)
        {
            var s = new ProjectionState();
            var m = ProjectionState.Parse(Json(5, "waiting", extra));
            Assert.NotNull(m, extra);
            Assert.AreEqual(Outcome.Applied, s.Apply(m, out _), extra);
            Assert.AreEqual(0, s.T.Cause, extra);
            Assert.AreEqual("", s.T.End, extra);
            Assert.AreEqual("超過自動範圍，等核准", s.T.Label, extra);
        }
        var ok = new ProjectionState();
        ok.Apply(ProjectionState.Parse(Json(5, "waiting", ",\"cause_seq\": 3 ,\"task_end\": \"timed_out\"")), out _);
        Assert.AreEqual(3, ok.T.Cause);
        Assert.AreEqual("Agent 已逾時，請購仍在等核准", ok.T.Label);
    }

    [Test]
    public void LabelsAreInTheFontAndDisplaySegmentsAreMarked()
    {
        string[] phases = { "entered", "quoted", "waiting", "denied", "allowed_unpaid", "expired", "withdrawn", "unknown" };
        foreach (var p in phases)
            foreach (var end in new[] { "", "cancelled", "timed_out" })
            {
                var l = SceneRules.Label(p, 3, end, "INSUFFICIENT_BUDGET");
                LayoutTests.InFont(l);
                foreach (var paid in new[] { "已付款", "付款中", "付款已送出", "已交付" }) StringAssert.DoesNotContain(paid, l, "a decision never says a payment happened");
                StringAssert.DoesNotContain("#", l, "no event number for the audience (gap review B3)");
                StringAssert.DoesNotContain(ShopView.AnimMark, l, "an event's label is not a display segment: " + l);
            }
    }

    // P2 (docs/commercial-p2-host.md 5): the payment's phases are known and
    // say what is true of them: in the scene's font, never "this demo does
    // not pay", and a decision never says paid.
    [Test]
    public void PaymentPhasesAreKnownAndTrue()
    {
        string[] phases = { "allowed", "approved", "paying", "confirming", "paid", "delivered", "delivery_failed", "refunded", "not_paid", "payment_review" };
        foreach (var p in phases)
        {
            CollectionAssert.Contains(ProjectionState.Phases, p);
            var l = SceneRules.Label(p, 7, "", "");
            LayoutTests.InFont(l);
            StringAssert.DoesNotContain("#", l);
            StringAssert.DoesNotContain("不付款", l);
            Assert.AreNotEqual("狀態未確認", l);
        }
        StringAssert.EndsWith("尚未付款", SceneRules.Label("allowed", 3, "", ""));
        StringAssert.EndsWith("尚未付款", SceneRules.Label("approved", 4, "", ""));
        // Each step of one payment is never a new purchase at the counter.
        var s = new ProjectionState();
        int seq = 3;
        foreach (var p in new[] { "allowed", "paying", "paying", "confirming", "paid" })
        {
            s.Apply(new ProjectionMessage { v = 1, gen = 1, consumer = "consumer-a", task_id = "task-a", seq = seq, phase = p, cause_seq = seq, animate = true }, out _);
            if (p != "allowed") Assert.IsFalse(s.Browse, p + " #" + seq + " browses");
            seq++;
        }
    }
}
