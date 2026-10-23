// Projection: the display message the Dashboard's JavaScript adapter sends
// to the shop, and the rules for taking it (docs/demo-ui.md section 4).
// The shop holds no token and reads no API: it only shows what it is sent.
// Nothing here can decide, pay, expire or record anything.
using System;
using System.Collections.Generic;
using System.Text.RegularExpressions;
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
        // Optional (D2, docs/demo-presentation-design.md 16.4); v stays 1.
        // A missing or bad value is unknown: 0 and "".
        public int cause_seq;    // the event that set the phase; shown as #n
        public string task_end;  // "" | cancelled | timed_out
        // Version 2 only (docs/multi-agent-demo-design.md 5.2): the scene's
        // generation and the customer's own slot generation. A clear without
        // a consumer clears the scene; with a consumer and slot, that
        // customer only. -1 when missing.
        public int scene = -1;
        public int slot = -1;
        // UX3, version 2 only: the scene's room (a layout key). A scene
        // generation has one layout; another room is a new scene.
        public string layout;
        // P2, optional (v stays): the payment capability when the host pays,
        // "local_anvil" (a local anvil) or "base_sepolia" (the testnet);
        // anything else is no payment (SceneRules.Paying).
        public string payments;
    }

    // Target: what the shop is to show for a customer (16.4 "T"). The page's
    // adapter.js keeps the same rules (shared vectors in
    // testdata/demo/scene-transitions.json).
    public class Target
    {
        public string Phase = "empty";
        public int Cause;            // 0: unknown
        public string End = "";
        public int Seq = -1;         // the latest snapshot sequence taken
        public string Verdict = "", Reason = "";
        public string Label => SceneRules.Label(Phase, Cause, End, Reason);
    }

    // SceneRules: the transition table (16.4), first match wins:
    // 1 another phase; 2 the same counter phase, both causes valid and
    // different (a new purchase); 3 only the label kind changed; 4 the same.
    public static class SceneRules
    {
        // Paying: the message's payments when it names a capability that pays, else "".
        public static string Paying(string payments) => payments == "local_anvil" || payments == "base_sepolia" ? payments : "";

        // The decisions at the counter. A payment's own phases (P2) stand at
        // the counter too until paid (then at the delivery zone), but are not
        // decisions: each of its steps has a new cause and is never a new purchase.
        public static bool Counter(string phase) => phase == "waiting" || phase == "denied" || phase == "allowed_unpaid" || phase == "allowed" || phase == "approved";
        public static int Cause(int cause, int seq) => cause > 0 && cause <= seq ? cause : 0;
        public static string End(string end) => end == "cancelled" || end == "timed_out" ? end : "";
        // Only a waiting Ask says the CLI timed out; a Deny, an expiry or a
        // withdrawal keeps its own label.
        static string Kind(string phase, string end) => phase == "waiting" && end == "timed_out" ? "waiting/timed_out" : phase;

        public static int Rule(Target t, string phase, int cause, string end)
        {
            if (t.Phase != phase) return 1;
            if (Counter(phase) && t.Cause > 0 && cause > 0 && t.Cause != cause) return 2;
            return Kind(t.Phase, t.End) != Kind(phase, end) ? 3 : 4;
        }

        // Browse: whether the move plays "（動畫） browsing" at the shelf.
        public static bool Browse(Target t, int rule, string phase, int cause)
        {
            if (rule == 2) return true;
            if (rule != 1) return false;
            if (phase == "quoted") return true;
            if (!Counter(phase) || t.Phase == "quoted") return false;
            if (Counter(t.Phase)) return t.Cause > 0 && cause > 0 && t.Cause != cause;
            return true;
        }

        // Code keeps a fixed reason code (A-Z, 0-9, _) and drops anything else.
        static string Code(string r) => !string.IsNullOrEmpty(r) && r.Length <= 40 && Regex.IsMatch(r, "^[A-Z0-9_]+$") ? r : "";

        // Plain: a reason as the audience reads it (the page's PLAIN, web/adapter.js); codes stay on the host page.
        static string Plain(string reason)
        {
            switch (Code(reason))
            {
                case "INSUFFICIENT_BUDGET": return "超出預算";
                case "MERCHANT_NOT_ALLOWED": return "不在允許的商店";
                case "ASSET_NOT_ALLOWED": return "不是允許的幣別";
                case "CREDENTIAL_INVALID": return "憑證檢查沒通過";
                case "CREDENTIAL_REVOKED": return "憑證已撤銷";
                case "CREDENTIAL_STATUS_UNAVAILABLE": return "憑證狀態無法確認";
                case "AUTHORIZATION_EXPIRED": return "授權或報價已過期";
                case "CONFIG_MISSING": return "缺少設定或憑證";
                case "TASK_NOT_ACTIVE": return "任務已結束";
                case "CONTRACT_VERSION_MISMATCH": return "版本不符";
                case "OWNERSHIP_MISMATCH": return "不是這位客人的請購";
                case "PAYMENT_CAPABILITY_UNAVAILABLE": return "這場展示不能付款";
                case "APPROVER_UNAVAILABLE": return "沒有可核准的人";
                case "VOICE_CHALLENGE_FAILED": return "唸的內容或聲音不符";
                case "APPROVER_DENIED": return "核准者拒絕";
                default: return "";
            }
        }

        // Label: the customer's label in plain words (gap review B3, GD3):
        // no code, no event number. The cause still decides what is new
        // (Rule); it is no longer printed.
        public static string Label(string phase, int cause, string end, string reason)
        {
            switch (phase)
            {
                case "empty": return "";
                case "entered": return "進店";
                case "quoted": return "選好商品，送出請購";
                case "waiting": return end == "timed_out" ? "Agent 已逾時，請購仍在等核准" : "超過自動範圍，等核准";
                case "denied":
                    var why = Plain(reason);
                    return (why != "" ? why + "，" : "") + "已拒絕，離店";
                // allowed_unpaid is also where a simulated approval stands (the page's UNITY_PHASE): true of both.
                case "allowed_unpaid": return "已放行，這筆不會付款";
                // P2 (docs/commercial-p2-host.md 5). Until a payment event:
                // true whether or not a payment ever starts.
                case "allowed": return "在範圍內放行，尚未付款";
                case "approved": return "已核准，尚未付款";
                case "paying": return "付款中：預留預算";
                case "confirming": return "付款已送出，等鏈上確認";
                case "paid": return "已付款，在交付區等交付";
                case "delivered": return "已交付，帶著收據離店";
                case "delivery_failed": return "已付款，交付失敗（待人工處理）";
                case "not_paid": return "沒有付款（沒有花錢），離店";
                case "payment_review": return "付款結果未確認，待人工處理";
                case "expired": return "核准期限已過，離店";
                case "withdrawn": return "已取消，離店";
                default: return "狀態未確認";
            }
        }
    }

    public enum Outcome { Applied, Cleared, Duplicate, Stale, Older, Invalid }

    // ProjectionState decides what to do with each message. Pure logic, no
    // scene: the EditMode tests drive it directly.
    public class ProjectionState
    {
        public const int Version = 1;
        public static readonly string[] Phases = { "empty", "entered", "quoted", "waiting", "denied", "allowed_unpaid", "expired", "withdrawn", "unknown",
            "allowed", "approved", "paying", "confirming", "paid", "delivered", "delivery_failed", "not_paid", "payment_review" };
        public static readonly string[] Consumers = { "consumer-a", "consumer-b" };

        public int Gen { get; private set; } = -1;
        public string Consumer { get; private set; }
        public string TaskId { get; private set; }
        public Target T { get; private set; } = new Target();
        public int Seq => T.Seq;
        public string Phase => T.Phase;
        public int AnimatedSeq { get; private set; } = -1;
        public int Rule { get; private set; }     // of the last Applied message
        public bool Browse { get; private set; }

        // JsonUtility turns "3" or 3.5 into 3: the two optional fields are
        // taken only when written once, as an integer and as one of the two
        // ending words; anything else is unknown (16.4).
        static readonly Regex CauseKey = new Regex("[{,]\\s*\"cause_seq\"\\s*:"), CauseInt = new Regex("[{,]\\s*\"cause_seq\"\\s*:\\s*-?[0-9]+\\s*[,}]");
        static readonly Regex EndKey = new Regex("[{,]\\s*\"task_end\"\\s*:"), EndWord = new Regex("[{,]\\s*\"task_end\"\\s*:\\s*\"(cancelled|timed_out)\"\\s*[,}]");

        // Parse reads a message as data only; anything malformed is Invalid.
        public static ProjectionMessage Parse(string json)
        {
            if (string.IsNullOrEmpty(json) || json.Length > 4096) return null;
            ProjectionMessage m;
            try { m = JsonUtility.FromJson<ProjectionMessage>(json); }
            catch (ArgumentException) { return null; }
            if (m == null) return null;
            if (CauseKey.Matches(json).Count != 1 || !CauseInt.IsMatch(json)) m.cause_seq = 0;
            if (EndKey.Matches(json).Count != 1 || !EndWord.IsMatch(json)) m.task_end = "";
            return m;
        }

        // Apply takes one message. animate is true only for an event sequence
        // not animated before in this generation. An Applied message is
        // always compared with the target T, never with what was last
        // shown; Rule and Browse say what it changes.
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
                Gen = m.gen; Consumer = null; TaskId = null; T = new Target(); AnimatedSeq = -1;
                if (m.clear) return Outcome.Cleared;
            }
            else if (m.clear) return Outcome.Duplicate;
            if (TaskId != null && (m.task_id != TaskId || m.consumer != Consumer)) return Outcome.Invalid; // one task per generation
            if (m.seq < Seq) return Outcome.Older;
            var phase = Array.IndexOf(Phases, m.phase) >= 0 ? m.phase : "unknown";
            if (m.seq == Seq && phase == Phase) return Outcome.Duplicate;
            int cause = SceneRules.Cause(m.cause_seq, m.seq);
            var end = SceneRules.End(m.task_end);
            Rule = SceneRules.Rule(T, phase, cause, end);
            Browse = SceneRules.Browse(T, Rule, phase, cause);
            Consumer = m.consumer; TaskId = m.task_id;
            T = new Target { Phase = phase, Cause = cause, End = end, Seq = m.seq, Verdict = m.verdict ?? "", Reason = m.reason ?? "" };
            if (m.animate && m.seq > AnimatedSeq) animate = true;
            if (m.seq > AnimatedSeq) AnimatedSeq = m.seq; // never animate the same sequence twice
            return Outcome.Applied;
        }
    }

    // SceneState is the version 2 rule (docs/multi-agent-demo-design.md 5.2):
    // one scene generation and, inside it, one slot per customer with its own
    // slot generation, task, target and animated sequences. A message for one
    // customer never moves, clears or completes the other. Pure logic, no
    // scene: the EditMode tests drive it directly.
    public class SceneState
    {
        public const int Version = 2;

        public class Slot
        {
            public int Gen = -1;            // the slot generation
            public string TaskId;
            public Target T = new Target();
            public int AnimatedSeq = -1;
            public int Rule;                // of the last Applied message
            public bool Browse;
        }

        public int Scene { get; private set; } = -1;
        public string Layout { get; private set; } = ""; // the scene generation's layout, as sent
        readonly Dictionary<string, Slot> slots = new Dictionary<string, Slot>();

        public Slot Of(string consumer)
        {
            if (!slots.TryGetValue(consumer, out var s)) slots[consumer] = s = new Slot();
            return s;
        }

        // Apply takes one message. sceneReset: every customer is to be reset
        // first; slotReset: this customer only.
        public Outcome Apply(ProjectionMessage m, out bool animate, out bool sceneReset, out bool slotReset)
        {
            animate = sceneReset = slotReset = false;
            if (m == null || m.v != Version || m.scene < 0) return Outcome.Invalid;
            bool sceneClear = m.clear && string.IsNullOrEmpty(m.consumer);
            // Checked whole before anything may reset: an invalid message never clears.
            if (!sceneClear)
            {
                if (Array.IndexOf(ProjectionState.Consumers, m.consumer) < 0 || m.slot < 0) return Outcome.Invalid;
                if (!m.clear && (string.IsNullOrEmpty(m.task_id) || m.seq < 0)) return Outcome.Invalid;
            }
            if (m.scene < Scene) return Outcome.Stale;
            if (m.scene == Scene && (m.layout ?? "") != Layout) return Outcome.Invalid; // one layout per scene generation
            if (m.scene > Scene)
            {
                Scene = m.scene;
                Layout = m.layout ?? "";
                slots.Clear();
                sceneReset = true;
                if (sceneClear) return Outcome.Cleared;
            }
            else if (sceneClear) return Outcome.Duplicate;
            var s = Of(m.consumer);
            if (m.slot < s.Gen) return Outcome.Stale;
            if (m.slot > s.Gen)
            {
                slots[m.consumer] = s = new Slot { Gen = m.slot };
                slotReset = true;
                if (m.clear) return Outcome.Cleared;
            }
            else if (m.clear) return Outcome.Duplicate;
            if (s.TaskId != null && m.task_id != s.TaskId) return Outcome.Invalid; // one task per slot generation
            if (m.seq < s.T.Seq) return Outcome.Older;
            var phase = Array.IndexOf(ProjectionState.Phases, m.phase) >= 0 ? m.phase : "unknown";
            if (m.seq == s.T.Seq && phase == s.T.Phase) return Outcome.Duplicate;
            int cause = SceneRules.Cause(m.cause_seq, m.seq);
            var end = SceneRules.End(m.task_end);
            s.Rule = SceneRules.Rule(s.T, phase, cause, end);
            s.Browse = SceneRules.Browse(s.T, s.Rule, phase, cause);
            s.TaskId = m.task_id;
            s.T = new Target { Phase = phase, Cause = cause, End = end, Seq = m.seq, Verdict = m.verdict ?? "", Reason = m.reason ?? "" };
            if (m.animate && m.seq > s.AnimatedSeq) animate = true;
            if (m.seq > s.AnimatedSeq) s.AnimatedSeq = m.seq; // per customer: never animate the same sequence twice
            return Outcome.Applied;
        }
    }
}
