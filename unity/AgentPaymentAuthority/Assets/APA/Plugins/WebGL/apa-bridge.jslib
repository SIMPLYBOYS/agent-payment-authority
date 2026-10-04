// The shop's reports to the page (ES5, as the Unity manual requires for
// .jslib). Only a fixed kind, two numbers and a short detail are passed;
// the page decides what, if anything, to do with them.
mergeInto(LibraryManager.library, {
  APA_Report: function (kind, gen, seq, detail) {
    if (typeof window !== "undefined" && typeof window.apaUnityReport === "function") {
      window.apaUnityReport(UTF8ToString(kind), gen, seq, UTF8ToString(detail));
    }
  }
});
