// Projection: the display message the Dashboard's JavaScript adapter sends
// to the shop, and the rules for taking it (docs/demo-ui.md section 4).
// The shop holds no token and reads no API: it only shows what it is sent.
// Nothing here can decide, pay, expire or record anything.
using System;
using UnityEngine;

namespace APA
{
    [Serializable]
    public class ProjectionMessage
    {
        public int v;            // bridge version, must be Version
        public int gen;          // the page's viewer/task generation
        public bool clear;       // a new generation with nothing to show yet
        public string consumer;  // consumer-a | consumer-b
        public string task_id;
        public int seq;          // the snapshot's as_of_sequence
        public string phase;     // one of Phases
        public string verdict;   // Ask | Deny | Allow | "" (display only)
        public string reason;
        public bool animate;     // false for a first load: a stored snapshot is not news
    }

    public enum Outcome { Applied, Cleared, Duplicate, Stale, Older, Invalid }

    // ProjectionState decides what to do with each message. Pure logic, no
    // scene: the EditMode tests drive it directly.
    public class ProjectionState
    {
        public const int Version = 1;
        public static readonly string[] Phases = { "empty", "entered", "quoted", "waiting", "denied", "allowed_unpaid", "expired", "withdrawn", "unknown" };
        public static readonly string[] Consumers = { "consumer-a", "consumer-b" };

        public int Gen { get; private set; } = -1;
        public string Consumer { get; private set; }
        public string TaskId { get; private set; }
        public int Seq { get; private set; } = -1;
        public string Phase { get; private set; } = "empty";
        public int AnimatedSeq { get; private set; } = -1;

        // Parse reads a message as data only; anything malformed is Invalid.
        public static ProjectionMessage Parse(string json)
        {
            if (string.IsNullOrEmpty(json) || json.Length > 4096) return null;
            try { return JsonUtility.FromJson<ProjectionMessage>(json); }
            catch (ArgumentException) { return null; }
        }

        // Apply takes one message. animate is true only for an event sequence
        // not animated before in this generation.
        public Outcome Apply(ProjectionMessage m, out bool animate)
        {
            animate = false;
            if (m == null || m.v != Version || m.gen < 0) return Outcome.Invalid;
            // A message is checked whole before it may reset anything: an
            // invalid one never clears what is shown.
            if (!m.clear && (Array.IndexOf(Consumers, m.consumer) < 0 || string.IsNullOrEmpty(m.task_id) || m.seq < 0)) return Outcome.Invalid;
            if (m.gen < Gen) return Outcome.Stale;
            if (m.gen > Gen)
            {
                // A new viewer or task: forget everything of the old one first.
                Gen = m.gen; Consumer = null; TaskId = null; Seq = -1; Phase = "empty"; AnimatedSeq = -1;
                if (m.clear) return Outcome.Cleared;
            }
            else if (m.clear) return Outcome.Duplicate;
            if (TaskId != null && (m.task_id != TaskId || m.consumer != Consumer)) return Outcome.Invalid; // one task per generation
            if (m.seq < Seq) return Outcome.Older;
            var phase = Array.IndexOf(Phases, m.phase) >= 0 ? m.phase : "unknown";
            if (m.seq == Seq && phase == Phase) return Outcome.Duplicate;
            Consumer = m.consumer; TaskId = m.task_id; Seq = m.seq; Phase = phase;
            if (m.animate && m.seq > AnimatedSeq) animate = true;
            if (m.seq > AnimatedSeq) AnimatedSeq = m.seq; // never animate the same sequence twice
            return Outcome.Applied;
        }
    }
}
