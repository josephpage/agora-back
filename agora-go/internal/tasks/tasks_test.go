package tasks

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"agora/internal/app"
	"agora/internal/cache"
)

// fakeLifecycle records the QaG steps (WeeklyTasksHandlerTest's mocks).
type fakeLifecycle struct {
	calls []string
	fail  map[string]error
}

func (f *fakeLifecycle) step(name string) error {
	f.calls = append(f.calls, name)
	return f.fail[name]
}
func (f *fakeLifecycle) PutMostPopularQagInSelectedStatus(context.Context) error {
	return f.step("select")
}
func (f *fakeLifecycle) ArchiveOldQag(context.Context) error { return f.step("archive") }
func (f *fakeLifecycle) AnonymizeOldQag(context.Context) error {
	return f.step("anonymize")
}

// weeklyCase runs runWeekly with the transition answer and the arguments.
func weeklyCase(t *testing.T, inTransition bool, args map[string]string) (*fakeLifecycle, []string, error) {
	t.Helper()
	lc := &fakeLifecycle{}
	var asked []string
	err := runWeekly(context.Background(), weeklyDeps{
		inTransition: func(context.Context) (bool, error) {
			asked = append(asked, "isInTransition")
			return inTransition, nil
		},
		lifecycle: lc,
	}, args)
	return lc, asked, err
}

// WeeklyTasksHandlerTest, WithoutIgnoreTransition: in transition → select, archive, anonymize.
func TestWeeklyInTransitionSelectsArchivesAndAnonymizes(t *testing.T) {
	lc, asked, err := weeklyCase(t, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(asked, []string{"isInTransition"}) {
		t.Errorf("transition checked %v", asked)
	}
	if want := []string{"select", "archive", "anonymize"}; !reflect.DeepEqual(lc.calls, want) {
		t.Errorf("calls = %v, want %v", lc.calls, want)
	}
}

// WithoutIgnoreTransition: not in transition → only the anonymization.
func TestWeeklyNotInTransitionOnlyAnonymizes(t *testing.T) {
	lc, asked, err := weeklyCase(t, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 {
		t.Errorf("transition checked %d times", len(asked))
	}
	if want := []string{"anonymize"}; !reflect.DeepEqual(lc.calls, want) {
		t.Errorf("calls = %v, want %v", lc.calls, want)
	}
}

// WithIgnoreTransition: "true" never asks the transition, always selects and archives.
func TestWeeklyForcedSkipsTheTransitionCheck(t *testing.T) {
	lc, asked, err := weeklyCase(t, false, map[string]string{"force_question_selection": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 0 {
		t.Errorf("transition checked %v, want never", asked)
	}
	if want := []string{"select", "archive", "anonymize"}; !reflect.DeepEqual(lc.calls, want) {
		t.Errorf("calls = %v, want %v", lc.calls, want)
	}
}

// WithIgnoreTransition, "false": the transition is checked.
func TestWeeklyForceFalseChecksTheTransition(t *testing.T) {
	lc, asked, err := weeklyCase(t, false, map[string]string{"force_question_selection": "false"})
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || !reflect.DeepEqual(lc.calls, []string{"anonymize"}) {
		t.Errorf("asked %v calls %v", asked, lc.calls)
	}
}

// The flag is compared to the exact string "true": "TRUE", "1" and " true" do not force.
func TestWeeklyForceFlagIsAnExactString(t *testing.T) {
	for _, v := range []string{"TRUE", "True", "1", " true", "yes"} {
		lc, asked, err := weeklyCase(t, false, map[string]string{"force_question_selection": v})
		if err != nil {
			t.Fatal(err)
		}
		if len(asked) != 1 || !reflect.DeepEqual(lc.calls, []string{"anonymize"}) {
			t.Errorf("flag %q: asked %v calls %v", v, asked, lc.calls)
		}
	}
}

// A failing step stops the task (the Kotlin exception): nothing after it runs.
func TestWeeklyStepFailureStopsTheTask(t *testing.T) {
	boom := errors.New("boom")
	lc := &fakeLifecycle{fail: map[string]error{"select": boom}}
	err := runWeekly(context.Background(), weeklyDeps{
		inTransition: func(context.Context) (bool, error) { return true, nil },
		lifecycle:    lc,
	}, nil)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"select"}; !reflect.DeepEqual(lc.calls, want) {
		t.Errorf("calls = %v, want %v", lc.calls, want)
	}
}

// A failing transition check stops the task before any QaG step.
func TestWeeklyTransitionFailureStopsTheTask(t *testing.T) {
	boom := errors.New("strapi date")
	lc := &fakeLifecycle{}
	err := runWeekly(context.Background(), weeklyDeps{
		inTransition: func(context.Context) (bool, error) { return false, boom },
		lifecycle:    lc,
	}, nil)
	if !errors.Is(err, boom) || len(lc.calls) != 0 {
		t.Fatalf("err = %v, calls = %v", err, lc.calls)
	}
}

// Nothing registered: the weekly task fails loudly (ERROR log, error), it never skips silently.
func TestWeeklyWithoutRegisteredLifecycleFailsLoudly(t *testing.T) {
	saved := qagLifecycleFactory
	t.Cleanup(func() { qagLifecycleFactory = saved })
	qagLifecycleFactory = nil

	var logs bytes.Buffer
	a := &app.App{Log: slog.New(slog.NewTextHandler(&logs, nil))}
	err := Weekly(context.Background(), a, nil)
	if err == nil || !strings.Contains(err.Error(), "no QaG lifecycle is registered") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("no ERROR log: %s", logs.String())
	}
}

// A factory returning nil is the same failure.
func TestWeeklyFactoryReturningNilFailsLoudly(t *testing.T) {
	saved := qagLifecycleFactory
	t.Cleanup(func() { qagLifecycleFactory = saved })
	RegisterQagLifecycle(func(*app.App) QagLifecycle { return nil })

	a := &app.App{Log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	if err := Weekly(context.Background(), a, nil); err == nil {
		t.Fatal("no error")
	}
}

// ParseCustomCommand: the arguments of AgoraCustomCommandHelper.
func TestParseCustomCommand(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		ok      bool
		command string
		params  map[string]string
	}{
		{name: "no command", args: []string{"--server.port=1"}, ok: false},
		{name: "plain", args: []string{"--run-custom-command=dailyTasks"}, ok: true, command: "dailyTasks"},
		{name: "blank command is no custom command", args: []string{"--run-custom-command=   "}, ok: false},
		{name: "empty command is no custom command", args: []string{"--run-custom-command="}, ok: false},
		{name: "command is trimmed", args: []string{"--run-custom-command= weeklyTasks "}, ok: true, command: "weeklyTasks"},
		{name: "first command wins; a second one is a plain --key=value argument", args: []string{"--run-custom-command=dailyTasks", "--run-custom-command=weeklyTasks"}, ok: true, command: "dailyTasks",
			params: map[string]string{"run-custom-command": "weeklyTasks"}},
		{name: "arguments before the command are ignored", args: []string{"--x=1", "--run-custom-command=weeklyTasks", "--force_question_selection=true"}, ok: true, command: "weeklyTasks",
			params: map[string]string{"force_question_selection": "true"}},
		{name: "last value of a key wins", args: []string{"--run-custom-command=weeklyTasks", "--k=1", "--k=2"}, ok: true, command: "weeklyTasks",
			params: map[string]string{"k": "2"}},
		{name: "value keeps later equal signs", args: []string{"--run-custom-command=weeklyTasks", "--k=a=b"}, ok: true, command: "weeklyTasks",
			params: map[string]string{"k": "a=b"}},
		{name: "not a --key=value argument is ignored", args: []string{"--run-custom-command=weeklyTasks", "plain", "--flag", "-k=1", "--=v", "--e="}, ok: true, command: "weeklyTasks",
			params: map[string]string{"": "v", "e": ""}},
		{name: "blank arguments are dropped", args: []string{"--run-custom-command=weeklyTasks", "   ", ""}, ok: true, command: "weeklyTasks"},
		{name: "a newline inside an argument splits it (the Kotlin join and split)", args: []string{"--run-custom-command=weeklyTasks", "--a=1\n--b=2"}, ok: true, command: "weeklyTasks",
			params: map[string]string{"a": "1", "b": "2"}},
		{name: "a no-break space is trimmed (Kotlin isWhitespace includes isSpaceChar)", args: []string{"--run-custom-command= dailyTasks"}, ok: true, command: "dailyTasks"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseCustomCommand(c.args)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if !ok {
				return
			}
			if got.Command != c.command {
				t.Errorf("command = %q, want %q", got.Command, c.command)
			}
			if !reflect.DeepEqual(got.Arguments, c.params) {
				t.Errorf("arguments = %#v, want %#v", got.Arguments, c.params)
			}
		})
	}
}

// LogFeatureFlags logs the flags in declaration order, from the environment when the cache is empty.
func TestLogFeatureFlags(t *testing.T) {
	for _, f := range featureNames {
		t.Setenv(f.feature.Key(), "true")
	}
	t.Setenv("IS_LOGIN_ENABLED", "false")
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	a := &app.App{Log: logger, Cache: cache.New(nil, logger, false)}
	if err := LogFeatureFlags(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	for _, want := range []string{"[SignUp] true", "[Login] false", "[QagAnonymization] true", "Agora FeatureFlags:"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q: %s", want, out)
		}
	}
	if strings.Index(out, "[SignUp]") > strings.Index(out, "[QagAnonymization]") {
		t.Errorf("flags not in declaration order: %s", out)
	}
}

// DeleteSuspiciousSupports does nothing (and touches no repository) when the feature is off.
func TestDeleteSuspiciousSupportsSkipsWhenTheFeatureIsOff(t *testing.T) {
	t.Setenv("IS_DELETE_BANNED_USERS_SUPPORTS_ENABLED", "false")
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	a := &app.App{Log: logger, Cache: cache.New(nil, logger, false)}
	if err := DeleteSuspiciousSupports(context.Background(), a); err != nil {
		t.Fatal(err)
	}
}
