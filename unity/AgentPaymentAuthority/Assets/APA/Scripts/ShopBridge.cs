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
// T2 (docs/commercial-town.md): "View" takes {mode: town | venue, safe} from
// the page and moves the camera (TownView); in the town view a click on a
// venue is reported as "venue" with its layout. The page decides what a
// click starts; the town only shows. V4 (docs/town-v2.md): "Pan" takes
// {to: "tour"}: the town's camera from the west end to the east end and back.
// Transit (docs/commercial-transit.md): "Gates" takes {on} from a page whose
// Host takes subway fares; the walked customer stopped at a shut gate going in
// is reported as "gate" with its station; "OpenGate" takes the page's answer
// {station, open, code, amount}. The page decides; the gate only shows.
// ATM (docs/commercial-atm.md): the walked customer at the street's ATM is
// reported as "atm" with its id; "Atm" takes the page's answer {id, code,
// amount, balance, address}. The Host decides; the ATM only shows.
// "Walk" takes {x, y, to}: the walked customer to a point, as a click there
// does (to: "subway" down the stairs, or a venue's layout in at its door);
// the video's director (scripts/video/film.mjs) uses it. Reported as "walk".
using System.Collections.Generic;
using System.Runtime.InteropServices;
using UnityEngine;

namespace APA
{
    public class ShopBridge : MonoBehaviour
    {
        public ShopView view;
        public TownView town; // null: a build without the town (no View, no venue clicks)
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
            bool hasTown = town != null && town.HasTown;
            if (hasTown) town.Clicked = layout => Report("venue", scene.Scene, 0, layout);
            if (hasTown) town.Gated = station => Report("gate", scene.Scene, 0, station);
            if (hasTown) town.AtAtm = id => Report("atm", scene.Scene, 0, id);
            var rooms = new List<string>(view.LayoutKeys);
            rooms.Sort(System.StringComparer.Ordinal);
            // rooms: each room's venue view in tiles (TownView.RoomView), so the page sizes the canvas to whole multiples of it.
            var sizes = rooms.ConvertAll(k => { var v = TownView.RoomView(view.SizeOf(k)); return k + ":" + Mathf.RoundToInt(v.x * 2f) / 2f + "x" + Mathf.RoundToInt(v.y); });
            Report("bridge", 0, 0, "versions=1,2;customers=" + string.Join(",", ProjectionState.Consumers) + ";select=1;layouts=" + string.Join(",", rooms) + (hasTown ? ";town=1;play=1;pan=1" + (town.HasGates ? ";gates=1" : "") : "") + ";rooms=" + string.Join(",", sizes));
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
            if (town != null && town.Mode == "town") return; // under a roof: the click is the venue's (TownView)
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

        [System.Serializable]
        public class ViewMessage
        {
            public string mode;  // town | venue
            public Area safe = new Area(); // the canvas's uncovered part, in its own pixels (Screen's) from its top left; zero: all of it
        }

        // Area: {x, y, width, height} as the page sends it (JsonUtility does not fill a Rect from those names).
        [System.Serializable]
        public class Area { public float x, y, width, height; }

        // View: the page's camera choice; only what is shown changes.
        public void View(string json)
        {
            ViewMessage m = null;
            try { m = JsonUtility.FromJson<ViewMessage>(json); } catch (System.Exception) { }
            bool ok = m != null && town != null && town.Show(m.mode, new Rect(m.safe.x, m.safe.y, m.safe.width, m.safe.height));
            Report("view", 0, 0, (m != null ? m.mode ?? "" : "") + "|" + (ok ? "Applied" : "Invalid"));
        }

        // Play and Steer (the game, Aaron 10/29): the customer the person walks ("" none) and the keys
        // held. Only the town changes: where the customer walks; a door is reported as "venue".
        [System.Serializable]
        public class PlayMessage { public string consumer; public float x, y; }

        public void Play(string json)
        {
            PlayMessage m = null;
            try { m = JsonUtility.FromJson<PlayMessage>(json); } catch (System.Exception) { }
            bool ok = m != null && town != null && town.Play(m.consumer);
            Report("play", 0, 0, (m != null ? m.consumer ?? "" : "") + "|" + (ok ? "Applied" : "Invalid"));
        }

        // Pan: {to: "tour"}, the video's prologue: the town's camera pans the whole street once (V4).
        [System.Serializable]
        public class PanMessage { public string to; }

        public void Pan(string json)
        {
            PanMessage m = null;
            try { m = JsonUtility.FromJson<PanMessage>(json); } catch (System.Exception) { }
            bool ok = m != null && m.to == "tour" && town != null && town.Tour();
            Report("pan", 0, 0, (m != null ? m.to ?? "" : "") + "|" + (ok ? "Applied" : "Invalid"));
        }

        [System.Serializable]
        public class GatesMessage { public bool on; }
        [System.Serializable]
        public class GateMessage { public string station, code, amount; public bool open; }

        public void Gates(string json)
        {
            GatesMessage m = null;
            try { m = JsonUtility.FromJson<GatesMessage>(json); } catch (System.Exception) { }
            bool ok = m != null && town != null && town.HasTown;
            if (ok) town.Gates(m.on);
            Report("gates", 0, 0, (m != null && m.on ? "on" : "off") + "|" + (ok ? "Applied" : "Invalid"));
        }

        public void OpenGate(string json)
        {
            GateMessage m = null;
            try { m = JsonUtility.FromJson<GateMessage>(json); } catch (System.Exception) { }
            bool ok = m != null && town != null && town.OpenGate(m.station, m.open, m.code, m.amount);
            Report("opengate", 0, 0, (m != null ? m.station ?? "" : "") + "|" + (ok ? "Applied" : "Invalid"));
        }

        [System.Serializable]
        public class AtmMessage { public string id, code, amount, balance, address; }

        public void Atm(string json)
        {
            AtmMessage m = null;
            try { m = JsonUtility.FromJson<AtmMessage>(json); } catch (System.Exception) { }
            bool ok = m != null && town != null && town.AtmSay(m.id, m.code, m.amount, m.balance, m.address);
            Report("atmsay", 0, 0, (m != null ? m.id ?? "" : "") + "|" + (ok ? "Applied" : "Invalid"));
        }

        [System.Serializable]
        public class WalkMessage { public float x, y; public string to; }

        public void Walk(string json)
        {
            WalkMessage m = null;
            try { m = JsonUtility.FromJson<WalkMessage>(json); } catch (System.Exception) { }
            bool ok = m != null && town != null && town.HasTown && town.GoTo(new Vector2(m.x, m.y), string.IsNullOrEmpty(m.to) ? null : m.to);
            Report("walk", 0, 0, ok ? "Applied" : "Invalid");
        }

        public void Steer(string json)
        {
            PlayMessage m = null;
            try { m = JsonUtility.FromJson<PlayMessage>(json); } catch (System.Exception) { }
            if (m != null && town != null) town.Steer(new Vector2(Mathf.Clamp(m.x, -1f, 1f), Mathf.Clamp(m.y, -1f, 1f))); // every key: no report
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
                    view.Payments(SceneRules.Paying(m.payments));
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

        string scenePays = ""; // version 2: how this scene generation's host pays ("" not at all)

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
            if (sceneReset)
            {
                scenePays = "";
                view.Payments(""); // a cleared scene says nothing of payments until a customer does
            }
            if (outcome == Outcome.Applied)
            {
                // Scene-wide: one customer's pay task says so for the scene; an M1
                // task beside it (no payments) does not take it back.
                if (SceneRules.Paying(m.payments) != "") scenePays = SceneRules.Paying(m.payments);
                view.Payments(scenePays);
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
