package main

// The index command reads the events of one shared Gradle test run and writes
// a bounded failure message (or an empty passing result) for each selected
// test class check.

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const diagnosticLimit = 32 << 10

// event is one line written by scripts/report-tests.gradle.
type event struct {
	Event    string `json:"event"`
	Root     bool   `json:"root"`
	Class    string `json:"class"`
	Name     string `json:"name"`
	Result   string `json:"result"`
	Failure  string `json:"failure"`
	Output   string `json:"output"`
	FailFast bool   `json:"failFast"`
	// Test marks a task event for the test task itself.
	Test bool `json:"test"`
}

// tail keeps diagnostic memory bounded even when a passing test logs gigabytes.
type tail struct {
	data      []byte
	truncated bool
}

func (t *tail) Write(p []byte) (int, error) {
	n := len(p)
	if n >= diagnosticLimit {
		t.truncated = t.truncated || len(t.data) > 0 || n > diagnosticLimit
		t.data = append(t.data[:0], p[n-diagnosticLimit:]...)
	} else {
		if extra := len(t.data) + n - diagnosticLimit; extra > 0 {
			t.data = append(t.data[:0], t.data[extra:]...)
			t.truncated = true
		}
		t.data = append(t.data, p...)
	}
	return n, nil
}

func (t *tail) add(s string) { _, _ = t.Write([]byte(s)) }

func (t *tail) String() string {
	if t.truncated {
		return "[earlier output omitted; showing diagnostic tail]\n" + string(t.data)
	}
	return string(t.data)
}

type classResult struct {
	// observed: Gradle reported a test or suite of this class.
	// failed: a test or the class itself failed, with failures holding why.
	// finished: the class suite completed without failing.
	observed, failed, finished bool
	failures, log              tail
}

type run struct {
	classes map[string]*classResult
	// started and finished track the test task's root suite.
	started, finished, failFast bool
	rootResult                  string
	// anyFailure: some test or class failed. outside: failures with no class,
	// such as a test executor that could not start.
	anyFailure bool
	outside    tail
	// tasks holds task failures that failing tests do not explain.
	tasks tail
	log   tail
}

func (r *run) class(name string) *classResult {
	c := r.classes[name]
	if c == nil {
		c = &classResult{}
		r.classes[name] = c
	}
	return c
}

func (r *run) add(e event) {
	switch e.Event {
	case "config":
		r.failFast = e.FailFast
	case "start":
		r.started = true
	case "task":
		// The test task fails because tests failed; those failures are
		// attributed through test events. Anything else, in the test task or
		// in another task (a finalizer, or any task under --continue), is a
		// failure no test class owns.
		if e.Test && strings.Contains(e.Failure, "There were failing tests") {
			return
		}
		r.tasks.add("task " + e.Name + " failed\n> " + e.Failure + "\n")
	case "output":
		if e.Class == "" {
			r.log.add(e.Output)
		} else {
			r.class(e.Class).log.add(e.Output)
		}
	case "test":
		failed := e.Result == "FAILURE"
		r.anyFailure = r.anyFailure || failed
		if e.Class == "" {
			if failed {
				r.outside.add(e.Name + " failed\n" + e.Failure + "\n")
			}
			return
		}
		c := r.class(e.Class)
		c.observed = true
		if failed {
			c.failed = true
			c.failures.add(e.Name + " failed\n" + e.Failure + "\n")
		}
	case "suite":
		failed := e.Result == "FAILURE"
		if e.Root {
			r.finished = true
			r.rootResult = e.Result
			if failed && e.Failure != "" {
				r.anyFailure = true
				r.outside.add(e.Failure + "\n")
			}
			return
		}
		if e.Class == "" {
			// Executor and other grouping suites. Their result aggregates
			// their children; only their own exceptions are failures.
			if failed && e.Failure != "" {
				r.anyFailure = true
				r.outside.add(e.Name + " failed\n" + e.Failure + "\n")
			}
			return
		}
		c := r.class(e.Class)
		c.observed = true
		// A class's suite fails whenever one of its tests did. Its own
		// exceptions, such as a failing @BeforeAll, are class failures.
		if failed && e.Failure != "" {
			r.anyFailure = true
			c.failed = true
			c.failures.add(e.Name + " failed\n" + e.Failure + "\n")
		}
		// Parameterized and nested containers carry the class name too; only
		// the class's own suite, named after it, completes the class.
		if e.Name == e.Class && !failed {
			c.finished = true
		}
	}
}

func summarize(report io.Reader, selected []string, exitCode int, stderr io.Reader) (map[string]string, error) {
	r := &run{classes: make(map[string]*classResult)}
	isSelected := make(map[string]bool)
	for _, name := range selected {
		if name == "" {
			return nil, fmt.Errorf("invalid selected test class %q", name)
		}
		if isSelected[name] {
			return nil, fmt.Errorf("duplicate selected test class %q", name)
		}
		isSelected[name] = true
	}
	decoder := json.NewDecoder(bufio.NewReader(report))
	for {
		var e event
		if err := decoder.Decode(&e); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode test events: %w", err)
		}
		r.add(e)
	}
	var stderrTail tail
	if _, err := io.Copy(&stderrTail, stderr); err != nil {
		return nil, fmt.Errorf("read Gradle stderr: %w", err)
	}

	// Failures no selected class can own fail every class sharing the run.
	var unchecked []string
	for name, c := range r.classes {
		if c.failed && !isSelected[name] {
			unchecked = append(unchecked, name)
		}
	}
	sort.Strings(unchecked)
	globalFailure := ""
	switch {
	case len(unchecked) > 0:
		var output tail
		for _, name := range unchecked {
			output.add(r.classes[name].failures.String())
			output.add(r.classes[name].log.String())
		}
		globalFailure = "failures in test classes without selected checks: " + strings.Join(unchecked, ", ") + "\n" + output.String() + stderrTail.String()
	case r.outside.String() != "":
		globalFailure = "Gradle test task failed outside a selected test class\n" + r.outside.String() + r.log.String() + stderrTail.String()
	case r.tasks.String() != "":
		globalFailure = "Gradle failed outside the test classes\n" + r.tasks.String() + stderrTail.String()
	case exitCode != 0 && !r.anyFailure:
		// Compilation, configuration, or a task other than the tests failed.
		globalFailure = fmt.Sprintf("shared Gradle command failed outside the test classes (exit %d)\n%s%s", exitCode, r.log.String(), stderrTail.String())
	}

	results := make(map[string]string, len(selected))
	for _, name := range selected {
		c := r.classes[name]
		if c == nil {
			c = &classResult{}
		}
		failure := globalFailure
		if failure == "" {
			switch {
			case c.failed:
				failure = name + " failed\n" + c.failures.String() + c.log.String()
			case c.finished:
			case !c.observed && r.failFast && r.anyFailure:
				// failFast stops the task at a reported failure, so later
				// classes have no events. That failure does not make them fail.
			case !c.observed && (exitCode == 0 || r.finished):
				// Gradle left the class out: its filter, the project's
				// includes and excludes, or the framework disagreed with
				// discovery.
				failure = fmt.Sprintf("%s did not run: Gradle completed the test task without it (exit %d)\n%s", name, exitCode, r.log.String())
			default:
				failure = fmt.Sprintf("no successful result for %s (Gradle exit %d)\n%s%s%s", name, exitCode, c.log.String(), r.log.String(), stderrTail.String())
			}
		}
		// Headers and combined logs are bounded too, so every result stays
		// comfortably below File.contents' limit.
		if len(failure) > 3*diagnosticLimit {
			failure = failure[:3*diagnosticLimit] + "\n[remaining diagnostics omitted]\n"
		}
		results[name] = failure
	}
	return results, nil
}

func openOptional(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		// Gradle failed before the init script ran.
		return io.NopCloser(strings.NewReader("")), nil
	}
	return f, err
}

func index(args []string) error {
	flags := flag.NewFlagSet("index", flag.ExitOnError)
	reportPath := flags.String("report", "", "test events written by report-tests.gradle")
	selectionPath := flags.String("selection", "", "JSON list of selected test classes")
	stderrPath := flags.String("stderr", "", "Gradle stderr")
	exitCode := flags.Int("exit-code", 0, "Gradle exit code")
	output := flags.String("output", "", "result directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	data, err := os.ReadFile(*selectionPath)
	if err != nil {
		return err
	}
	var selected []string
	if err := json.Unmarshal(data, &selected); err != nil {
		return err
	}
	report, err := openOptional(*reportPath)
	if err != nil {
		return err
	}
	defer report.Close()
	stderr, err := openOptional(*stderrPath)
	if err != nil {
		return err
	}
	defer stderr.Close()
	results, err := summarize(report, selected, *exitCode, stderr)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		return err
	}
	// Match the byte-order ranks used by TestResults.assertPassed. Class names
	// never become file paths, whatever their length or characters.
	sort.Strings(selected)
	for rank, name := range selected {
		file := filepath.Join(*output, fmt.Sprint(rank))
		if err := os.WriteFile(file, []byte(results[name]), 0o644); err != nil {
			return err
		}
	}
	return nil
}
