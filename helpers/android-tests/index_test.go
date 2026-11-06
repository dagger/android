package main

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func report(events ...event) io.Reader {
	var b strings.Builder
	e := json.NewEncoder(&b)
	for _, v := range events {
		if err := e.Encode(v); err != nil {
			panic(err)
		}
	}
	return strings.NewReader(b.String())
}

func classRun(class string, result string, tests ...event) []event {
	var events []event
	for _, t := range tests {
		t.Event, t.Class = "test", class
		events = append(events, t)
	}
	return append(events, event{Event: "suite", Class: class, Name: class, Result: result})
}

func TestSummarizeAttribution(t *testing.T) {
	selected := []string{"com.example.Ok", "com.example.Bad"}
	start := []event{{Event: "config"}, {Event: "start"}}
	ok := classRun("com.example.Ok", "SUCCESS", event{Name: "passes", Result: "SUCCESS"})
	root := func(result string) event {
		return event{Event: "suite", Root: true, Name: "Gradle Test Run", Result: result}
	}
	join := func(parts ...[]event) []event {
		var all []event
		for _, p := range parts {
			all = append(all, p...)
		}
		return all
	}
	for _, tc := range []struct {
		name     string
		events   []event
		exitCode int
		stderr   string
		want     map[string]string // Empty means pass; otherwise the error must contain this text.
	}{
		{"selected failure", join(start, ok,
			[]event{{Event: "output", Class: "com.example.Bad", Output: "bad output"}},
			classRun("com.example.Bad", "FAILURE", event{Name: "fails", Result: "FAILURE", Failure: "AssertionFailedError: boom"}),
			[]event{root("FAILURE")},
		), 1, "", map[string]string{"com.example.Ok": "", "com.example.Bad": "boom"}},
		{"skipped tests pass", join(start, ok,
			classRun("com.example.Bad", "SKIPPED", event{Name: "ignored", Result: "SKIPPED"}),
			[]event{root("SUCCESS")},
		), 0, "", map[string]string{"com.example.Ok": "", "com.example.Bad": ""}},
		{"class setup failure", join(start, ok,
			[]event{{Event: "suite", Class: "com.example.Bad", Name: "com.example.Bad", Result: "FAILURE", Failure: "beforeAll failed"}},
			[]event{root("FAILURE")},
		), 1, "", map[string]string{"com.example.Ok": "", "com.example.Bad": "beforeAll failed"}},
		{"parameterized container does not finish the class", join(start,
			classRun("com.example.Ok", "FAILURE", event{Name: "fails", Result: "FAILURE", Failure: "boom"}),
			[]event{
				{Event: "test", Class: "com.example.Bad", Name: "cases(int)[1]", Result: "SUCCESS"},
				{Event: "suite", Class: "com.example.Bad", Name: "cases(int)", Result: "SUCCESS"},
			},
		), 1, "executor crashed", map[string]string{"com.example.Ok": "boom", "com.example.Bad": "no successful result"}},
		{"unselected class failure", join(start, ok,
			classRun("com.example.Bad", "SUCCESS", event{Name: "passes", Result: "SUCCESS"}),
			classRun("com.example.Hidden", "FAILURE", event{Name: "fails", Result: "FAILURE", Failure: "hidden failure"}),
			[]event{root("FAILURE")},
		), 1, "", map[string]string{"com.example.Ok": "without selected checks: com.example.Hidden", "com.example.Bad": "hidden failure"}},
		{"executor failure", join(start, ok,
			classRun("com.example.Bad", "SUCCESS", event{Name: "passes", Result: "SUCCESS"}),
			[]event{{Event: "suite", Name: "Gradle Test Executor 1", Result: "FAILURE", Failure: "could not start"}},
			[]event{root("FAILURE")},
		), 1, "", map[string]string{"com.example.Ok": "outside a selected test class", "com.example.Bad": "could not start"}},
		{"build failure before tests", nil, 1, "Compilation failed",
			map[string]string{"com.example.Ok": "Compilation failed", "com.example.Bad": "outside the test classes"}},
		{"task failure after passing tests", join(start, ok,
			classRun("com.example.Bad", "SUCCESS", event{Name: "passes", Result: "SUCCESS"}),
			[]event{root("SUCCESS")},
		), 1, "post-test failure", map[string]string{"com.example.Ok": "post-test failure", "com.example.Bad": "post-test failure"}},
		{"selected class never ran", join(start, ok, []event{root("SUCCESS")}), 0, "",
			map[string]string{"com.example.Ok": "", "com.example.Bad": "com.example.Bad did not run"}},
		{"up-to-date task never ran", nil, 0, "",
			map[string]string{"com.example.Ok": "did not run", "com.example.Bad": "did not run"}},
		{"fail fast gap", join([]event{{Event: "config", FailFast: true}, {Event: "start"}},
			classRun("com.example.Ok", "FAILURE", event{Name: "fails", Result: "FAILURE", Failure: "first failure"}),
			[]event{root("FAILURE")},
		), 1, "", map[string]string{"com.example.Ok": "first failure", "com.example.Bad": ""}},
		{"gap without fail fast", join(start,
			classRun("com.example.Ok", "FAILURE", event{Name: "fails", Result: "FAILURE", Failure: "first failure"}),
			[]event{root("FAILURE")},
		), 1, "", map[string]string{"com.example.Ok": "first failure", "com.example.Bad": "did not run"}},
		{"test failures fail the test task", join(start, ok,
			classRun("com.example.Bad", "FAILURE", event{Name: "fails", Result: "FAILURE", Failure: "boom"}),
			[]event{root("FAILURE"), {Event: "task", Name: ":test", Test: true, Failure: "There were failing tests. See the report at: file:///r"}},
		), 1, "", map[string]string{"com.example.Ok": "", "com.example.Bad": "boom"}},
		{"finalizer fails next to a test failure", join(start, ok,
			classRun("com.example.Bad", "FAILURE", event{Name: "fails", Result: "FAILURE", Failure: "boom"}),
			[]event{root("FAILURE"),
				{Event: "task", Name: ":test", Test: true, Failure: "There were failing tests. See the report at: file:///r"},
				{Event: "task", Name: ":cleanup", Failure: "cleanup broke"}},
		), 1, "", map[string]string{"com.example.Ok": "task :cleanup failed", "com.example.Bad": "cleanup broke"}},
		{"test task fails for another reason", join(start, ok,
			classRun("com.example.Bad", "FAILURE", event{Name: "fails", Result: "FAILURE", Failure: "boom"}),
			[]event{root("FAILURE"), {Event: "task", Name: ":test", Test: true, Failure: "Process 'Gradle Test Executor 1' finished with non-zero exit value 137"}},
		), 1, "", map[string]string{"com.example.Ok": "exit value 137", "com.example.Bad": "exit value 137"}},
		{"crash without failures", join(start, ok), 1, "JVM crashed",
			map[string]string{"com.example.Ok": "JVM crashed", "com.example.Bad": "JVM crashed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := summarize(report(tc.events...), selected, tc.exitCode, strings.NewReader(tc.stderr))
			if err != nil {
				t.Fatal(err)
			}
			for key, want := range tc.want {
				value, ok := got[key]
				if !ok || (want == "" && value != "") || (want != "" && !strings.Contains(value, want)) {
					t.Errorf("%v: got %q, want %q", key, value, want)
				}
			}
		})
	}
}

func TestSummarizeBoundsResults(t *testing.T) {
	huge := strings.Repeat("x", 10*diagnosticLimit)
	events := []event{{Event: "start"}}
	for i := 0; i < 20; i++ {
		events = append(events, event{Event: "output", Class: "com.example.Bad", Output: huge})
	}
	events = append(events, classRun("com.example.Bad", "FAILURE", event{Name: "fails", Result: "FAILURE", Failure: huge})...)
	got, err := summarize(report(events...), []string{"com.example.Bad"}, 1, strings.NewReader(huge))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got["com.example.Bad"]); n == 0 || n > 3*diagnosticLimit+64 {
		t.Fatalf("result has %d bytes", n)
	}
}

func TestSummarizeRejectsInvalidSelections(t *testing.T) {
	for _, selected := range [][]string{{""}, {"a", "a"}} {
		if _, err := summarize(report(), selected, 0, strings.NewReader("")); err == nil {
			t.Errorf("accepted %q", selected)
		}
	}
}
