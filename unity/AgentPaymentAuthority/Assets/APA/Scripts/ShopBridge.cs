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

        public void Receive(string json)
        {
            var m = ProjectionState.Parse(json);
            var outcome = state.Apply(m, out bool animate);
            switch (outcome)
            {
                case Outcome.Cleared:
                    view.ResetAll();
                    break;
                case Outcome.Applied:
                    if (state.Gen == m.gen && m.seq >= 0)
                    {
                        int gen = m.gen, seq = m.seq;
                        view.Show(m.consumer, state.Phase, m.verdict, m.reason, animate, gen, g =>
                        {
                            // A late callback of an older generation is dropped.
                            if (g == state.Gen) Report("rendered", g, seq, state.Phase);
                        });
                    }
                    break;
            }
            Report("applied", m != null ? m.gen : -1, m != null ? m.seq : -1, outcome.ToString());
        }
    }
}
