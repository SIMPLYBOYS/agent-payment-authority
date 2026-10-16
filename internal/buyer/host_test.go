package buyer

import (
	"context"
	"slices"
	"strings"
	"testing"
)

// AE1: the task prompt keeps Prompt's rules and takes only the case's task;
// the model is Model unless an evaluation asks for one of EvalModels.
func TestTaskPromptAndModel(t *testing.T) {
	p, err := TaskPrompt("Order the 12:30 lunch set.")
	if err != nil || !strings.HasSuffix(p, "The task: Order the 12:30 lunch set.") || !strings.Contains(p, "You cannot pay and will not receive the content; the host records the request and a human decides.") {
		t.Fatalf("%q %v", p, err)
	}
	// The rules are Prompt's: its last sentence, word for word.
	if rules := Prompt[strings.LastIndex(Prompt, "You cannot pay"):]; !strings.Contains(p, rules+"\n\nThe task: ") {
		t.Fatalf("rules differ from Prompt's: %q", p)
	}
	for _, bad := range []string{"", "two\nlines", strings.Repeat("x", 301), "bell\x07", "c1\u0085", "zw\u200b", "ls\u2028", "bad\xff"} {
		if _, err := TaskPrompt(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if a := Argv("p", "c"); !slices.Contains(a, Model) {
		t.Fatalf("default argv %v", a)
	}
	if a := argv("p", "c", "haiku"); a[slices.Index(a, "--model")+1] != "haiku" || a[slices.Index(a, "--effort")+1] != Effort {
		t.Fatalf("eval argv %v", a)
	}
	if _, err := (&Host{Model: "opus"}).Run(context.Background(), "task-a", "p", nil); err == nil || !strings.Contains(err.Error(), "not one the host may ask for") {
		t.Fatalf("a model outside EvalModels: %v", err)
	}
}
