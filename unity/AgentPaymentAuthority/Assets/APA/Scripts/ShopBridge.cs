// ShopBridge: the GameObject "Bridge" the page calls with
// unityInstance.SendMessage("Bridge", "Receive", json). It takes data only
// (a ProjectionMessage), never a method name, script or URL. It reports back
// "bridge" (the versions and customers it can show), "ready", "applied" and
// "rendered" with a generation and sequence; those reports confirm display
// only and are never evidence of a decision. Version 1 is one customer per
// generation (the viewer page, history replay); version 2 is the scene with
// every customer in its own slot (docs/multi-agent-demo-design.md 5.2).
// UX1 (docs/commercial-ux1.md) adds selection, version 2 only and outside
// the projection: "Select" takes {scene, consumer} from the page and only
// shows the ring; a click on a customer is reported as "select" with the
// scene, that customer's slot and its consumer. The page decides; a click
// never selects anything here. UX3 (docs/commercial-scenes.md): a version 2
// message names the scene's layout (none: the default room); the room
// changes only with a new scene generation, and is reported as "layout".
using System.Collections.Generic;
using System.Runtime.InteropServices;
using UnityEngine;

namespace APA
{
    public class ShopBridge : MonoBehaviour
    {
        public ShopView view;
        public ProjectionState state = new ProjectionState();
        public SceneState scene = new SceneState();
        int mode = 1; // the version of the last message taken; a switch resets the scene
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

        // The versions and the customers this build can show, before ready: a
        // page that does not know the kind ignores it.
        void Start()
        {
            view.Picked = Picked;
            var rooms = new List<string>(view.LayoutKeys);
            rooms.Sort(System.StringComparer.Ordinal);
            Report("bridge", 0, 0, "versions=1,2;customers=" + string.Join(",", ProjectionState.Consumers) + ";select=1;layouts=" + string.Join(",", rooms));
            Report("ready", -1, -1, view.UsingPlaceholders ? "placeholder" : "limezu");
        }

        // Version 2: what one customer shows, with its slot.
        void RenderedV2(string consumer)
        {
            var s = scene.Of(consumer);
            Report("rendered", scene.Scene, s.T.Seq, consumer + "|" + s.Gen + "|" + s.TaskId + "|" + s.T.Phase + "|" + s.T.Verdict + "|" + s.T.Reason);
        }

        // Switching versions starts the scene over: neither state's rules
        // may judge the other's messages.
        void Switch(int v)
        {
            if (v == mode) return;
            mode = v;
            state = new ProjectionState();
            scene = new SceneState();
            viewGen = -1;
            view.Multi = v == SceneState.Version;
            if (view.LayoutKey != ShopView.DefaultLayout) view.UseLayout(ShopView.DefaultLayout); // version 1 shows the default room
            else view.ResetAll();
        }

        // What the shop shows: phase, task, verdict and reason code.
        void Rendered() => Report("rendered", state.Gen, state.Seq, state.Phase + "|" + state.TaskId + "|" + state.T.Verdict + "|" + state.T.Reason);

        [System.Serializable]
        public class SelectMessage
        {
            public int scene = -1;
            public string consumer; // "" or null: no one
        }

        // Picked: a click on a customer, in the version 2 scene only.
        void Picked(string consumer)
        {
            if (mode != SceneState.Version || scene.Scene < 0) return;
            Report("select", scene.Scene, scene.Of(consumer).Gen, consumer);
        }

        // Select: the page's choice, for the scene it names; another scene, a
        // customer this build does not show or version 1 changes nothing.
        public void Select(string json)
        {
            SelectMessage m = null;
            try { m = JsonUtility.FromJson<SelectMessage>(json); } catch (System.Exception) { }
            string outcome;
            if (m == null || mode != SceneState.Version) outcome = "Invalid";
            else if (m.scene != scene.Scene) outcome = "Stale";
            else if (string.IsNullOrEmpty(m.consumer)) { view.Highlight(null); outcome = "Applied"; }
            else if (System.Array.IndexOf(ProjectionState.Consumers, m.consumer) < 0) outcome = "Invalid";
            else { view.Highlight(m.consumer); outcome = "Applied"; }
            Report("selected", m != null ? m.scene : -1, 0, (m != null ? m.consumer ?? "" : "") + "|" + outcome);
        }

        public void Receive(string json)
        {
            var m = ProjectionState.Parse(json);
            if (m != null && m.v == SceneState.Version)
            {
                ReceiveV2(m);
                return;
            }
            if (m != null && m.v == ProjectionState.Version) Switch(1);
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

        void ReceiveV2(ProjectionMessage m)
        {
            Switch(2);
            if (string.IsNullOrEmpty(m.layout)) m.layout = ShopView.DefaultLayout;
            // A room this build does not have is refused whole, before anything resets.
            var outcome = view.HasLayout(m.layout) ? scene.Apply(m, out bool animate, out bool sceneReset, out bool slotReset)
                                                   : Invalid(out animate, out sceneReset, out slotReset);
            if (sceneReset)
            {
                if (scene.Layout != view.LayoutKey) view.UseLayout(scene.Layout); // resets every customer too
                else view.ResetAll();
                Report("layout", scene.Scene, 0, view.LayoutKey); // the room each new scene shows
            }
            else if (slotReset) view.Reset(m.consumer);
            if (outcome == Outcome.Applied)
            {
                var s = scene.Of(m.consumer);
                int sc = scene.Scene, gen = s.Gen;
                // rendered only once the customer shows the target, for its
                // newest sequence; a replaced walk or an older scene or slot
                // reports nothing, and never for the other customer.
                if (s.Rule <= 2)
                    view.Go(m.consumer, s.T, s.Browse, animate, gen, g => { if (scene.Scene == sc && scene.Of(m.consumer).Gen == g) RenderedV2(m.consumer); });
                else if (view.Relabel(m.consumer, s.T))
                    RenderedV2(m.consumer);
            }
            Report("applied", m.scene, m.seq, (m.consumer ?? "") + "|" + outcome);
        }

        static Outcome Invalid(out bool animate, out bool sceneReset, out bool slotReset)
        {
            animate = sceneReset = slotReset = false;
            return Outcome.Invalid;
        }
    }
}
