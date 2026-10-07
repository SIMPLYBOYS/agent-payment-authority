// ShopBridge: the GameObject "Bridge" the page calls with
// unityInstance.SendMessage("Bridge", "Receive", json). It takes data only
// (a ProjectionMessage), never a method name, script or URL. It reports back
// "ready", "applied" and "rendered" with a generation and sequence; those
// reports confirm display only and are never evidence of a decision.
using System.Collections.Generic;
using System.Runtime.InteropServices;
using UnityEngine;

namespace APA
{
    public class ShopBridge : MonoBehaviour
    {
        public ShopView view;
        public readonly ProjectionState state = new ProjectionState();
        public readonly List<string> reports = new List<string>(); // the last reports, for the PlayMode tests
        int viewGen = -1; // the generation the view was last reset for

#if UNITY_WEBGL && !UNITY_EDITOR
        [DllImport("__Internal")] static extern void APA_Report(string kind, int gen, int seq, string detail);
#else
        static void APA_Report(string kind, int gen, int seq, string detail) { }
#endif

        void Report(string kind, int gen, int seq, string detail)
        {
            reports.Add(kind + ":" + gen + ":" + seq + ":" + detail);
            if (reports.Count > 64) reports.RemoveAt(0);
            APA_Report(kind, gen, seq, detail);
        }

        void Start() => Report("ready", -1, -1, view.UsingPlaceholders ? "placeholder" : "limezu");

        // What the shop shows: phase, task, verdict and reason code.
        void Rendered() => Report("rendered", state.Gen, state.Seq, state.Phase + "|" + state.TaskId + "|" + state.T.Verdict + "|" + state.T.Reason);

        public void Receive(string json)
        {
            var m = ProjectionState.Parse(json);
            var outcome = state.Apply(m, out bool animate);
            switch (outcome)
            {
                case Outcome.Cleared:
                    view.ResetAll();
                    viewGen = m.gen;
                    break;
                case Outcome.Applied:
                    if (state.Gen == m.gen && m.seq >= 0)
                    {
                        // A new generation without a clear also drops the old one's walks.
                        if (viewGen != m.gen) { view.ResetAll(); viewGen = m.gen; }
                        // rendered is reported only once the customer shows
                        // the target, for the target's newest sequence, never
                        // for a sequence taken while it was still on its way
                        // (16.4). A transition that was replaced, or a late
                        // callback of an older generation, reports nothing.
                        if (state.Rule <= 2)
                            view.Go(m.consumer, state.T, state.Browse, animate, m.gen, g => { if (g == state.Gen) Rendered(); });
                        else if (view.Relabel(m.consumer, state.T))
                            Rendered();
                    }
                    break;
            }
            Report("applied", m != null ? m.gen : -1, m != null ? m.seq : -1, outcome.ToString());
        }
    }
}
