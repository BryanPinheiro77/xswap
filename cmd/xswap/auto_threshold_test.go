package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPerWindowTriggerLandingAndMargin(t *testing.T) {
	now := time.Now().Unix()
	for _, tc := range []struct {
		name                                   string
		global                                 int
		short, week                            *int
		active5h, active7d, target5h, target7d float64
		want                                   bool
	}{
		{"weekly triggers below global", 90, pointer(90), pointer(80), 30, 80, 20, 70, true},
		{"short triggers independently", 90, pointer(60), pointer(80), 60, 20, 50, 20, true},
		{"both explicit limits override global", 70, pointer(90), pointer(95), 85, 80, 10, 20, false},
		{"global fallback triggers short", 90, nil, pointer(80), 90, 20, 80, 20, true},
		{"global fallback triggers weekly", 85, pointer(90), nil, 20, 85, 10, 75, true},
		{"weekly landing at limit rejected", 90, pointer(90), pointer(80), 30, 85, 10, 80, false},
		{"short landing at limit rejected", 90, pointer(70), pointer(80), 80, 20, 70, 10, false},
		{"five points of worst-window headroom", 90, pointer(90), pointer(80), 30, 80, 85, 10, true},
		{"four points are insufficient", 90, pointer(90), pointer(80), 30, 80, 86, 10, false},
		{"both active windows below limits", 90, pointer(90), pointer(80), 89, 79, 10, 20, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Settings{Auto: AutoConfig{Threshold: tc.global, Threshold5h: tc.short, Threshold7d: tc.week, Interval: 60, Cooldown: 300, Margin: 5}}
			records := map[string]Record{"default": quota(tc.active5h, tc.active7d, now), "work": quota(tc.target5h, tc.target7d, now)}
			target, _ := decide(records, "default", s, now, 0)
			if (target == "work") != tc.want {
				t.Fatal("unexpected per-window decision", target)
			}
			if target, _ := decide(records, "default", s, now, now); target != "" {
				t.Fatal("per-window trigger bypassed cooldown")
			}
		})
	}
}

func TestPerWindowCandidateRankingAndTies(t *testing.T) {
	now := time.Now().Unix()
	s := Settings{Auto: AutoConfig{Threshold: 90, Threshold5h: pointer(90), Threshold7d: pointer(80), Interval: 60, Cooldown: 300, Margin: 5}}
	records := map[string]Record{"default": quota(30, 80, now), "a": quota(40, 75, now), "z": quota(80, 60, now)}
	if target, _ := decide(records, "default", s, now, 0); target != "z" {
		t.Fatal("did not choose greatest minimum headroom", target)
	}
	records["z"] = quota(85, 60, now)
	if target, _ := decide(records, "default", s, now, 0); target != "a" {
		t.Fatal("equal headroom did not break ties by name", target)
	}
	s.Disabled = []string{"a", "z"}
	if target, _ := decide(records, "default", s, now, 0); target != "" {
		t.Fatal("disabled account became a candidate")
	}
}

func TestPerWindowEligibilityAndDurationMapping(t *testing.T) {
	now := time.Now().Unix()
	config := AutoConfig{Threshold: 90, Threshold5h: pointer(60), Threshold7d: pointer(80), Interval: 60}
	record := quota(55, 70, now)
	record.Limits.Main.Primary, record.Limits.Main.Secondary = record.Limits.Main.Secondary, record.Limits.Main.Primary
	if score, valid := rotationPressure(record, config, now, 120); !valid || score != -5 {
		t.Fatal("window rules followed slots rather than durations", score, valid)
	}
	for _, mutate := range []func(*Record){
		func(r *Record) { r.Limits.Main.Primary = nil },
		func(r *Record) { r.Limits.Main.Secondary.Minutes = pointer(60) },
		func(r *Record) { r.Limits.Main.Primary.Resets = pointer(now) },
		func(r *Record) { r.State = quotaStale },
		func(r *Record) { r.Updated = now - 121 },
		func(r *Record) { r.Error = "offline" },
		func(r *Record) { r.Account.Type = "apiKey" },
	} {
		r := quota(20, 30, now)
		mutate(&r)
		if _, valid := rotationPressure(r, config, now, 120); valid {
			t.Fatal("accepted unsafe quota evidence", r)
		}
	}
	config.Threshold5h, config.Threshold7d = nil, nil
	record = quota(75, 10, now)
	record.Limits.Main.Primary.Minutes = pointer(60)
	if score, valid := rotationPressure(record, config, now, 120); !valid || score != -15 {
		t.Fatal("unrecognized duration did not use global threshold", score, valid)
	}
	for _, short := range []float64{0, 25, 89, 90, 95, 100} {
		for _, week := range []float64{0, 25, 89, 90, 95, 100} {
			r := quota(short, week, now)
			pressure, valid := rotationPressure(r, config, now, 120)
			legacy, _ := quotaScore(r, now, 120)
			if !valid || pressure != legacy-90 {
				t.Fatal("legacy global scoring changed", pressure, legacy)
			}
		}
	}
}

func TestWindowThresholdSettingsValidation(t *testing.T) {
	for _, data := range []string{
		`{"auto":{"threshold5h":0}}`, `{"auto":{"threshold5h":101}}`,
		`{"auto":{"threshold7d":-1}}`, `{"auto":{"threshold7d":101}}`,
		`{"auto":{"threshold5h":1.5}}`, `{"auto":{"threshold7d":"80"}}`,
	} {
		a := fixture(t)
		path := filepath.Join(a.Root, "settings.json")
		if err := atomicWrite(path, []byte(data)); err != nil {
			t.Fatal(err)
		}
		if _, err := a.settings(); err == nil {
			t.Fatal("accepted invalid window threshold", data)
		}
		if err := a.configureAuto(false, 80, 0); err == nil {
			t.Fatal("overwrote invalid settings")
		}
		after, _ := os.ReadFile(path)
		if string(after) != data {
			t.Fatal("invalid settings were changed")
		}
	}
	a := fixture(t)
	if err := atomicWrite(filepath.Join(a.Root, "settings.json"), []byte(`{"auto":{"threshold":85,"threshold5h":null,"threshold7d":70}}`)); err != nil {
		t.Fatal(err)
	}
	s, err := a.settings()
	if err != nil || s.Auto.thresholdForWindow(300) != 85 || s.Auto.thresholdForWindow(10080) != 70 || len(s.UnknownKeys) != 0 {
		t.Fatal("fallback or supported settings keys not recognized", s, err)
	}
}

func TestWindowThresholdCLIChangesAndReset(t *testing.T) {
	a := fixture(t)
	for _, tc := range []struct {
		args                []string
		short, week, global int
	}{
		{[]string{"--threshold", "80", "--threshold-5h", "90", "--threshold-7d", "70"}, 90, 70, 80},
		{[]string{"--threshold", "60"}, 90, 70, 60},
		{[]string{"--threshold-5h", "global"}, 60, 70, 60},
		{[]string{"--threshold-7d", "global"}, 60, 60, 60},
	} {
		if err := a.run(append([]string{"auto", "status"}, tc.args...)); err != nil {
			t.Fatal(err)
		}
		s, err := a.settings()
		if err != nil || s.Auto.Enabled || s.Auto.thresholdForWindow(300) != tc.short || s.Auto.thresholdForWindow(10080) != tc.week || s.Auto.Threshold != tc.global {
			t.Fatal("wrong persistent thresholds", s, err)
		}
	}
	s, _ := a.settings()
	if s.Auto.Threshold5h != nil || s.Auto.Threshold7d != nil {
		t.Fatal("global reset did not clear overrides")
	}
	before, _ := os.ReadFile(filepath.Join(a.Root, "settings.json"))
	for _, args := range [][]string{
		{"auto", "status", "--threshold-5h", "0"}, {"auto", "status", "--threshold-7d", "101"},
		{"auto", "status", "--threshold-5h", "bad"}, {"auto", "status", "--threshold-7d"},
		{"auto", "status", "--threshold-5h", ""}, {"auto", "view", "--threshold-7d", "80"},
		{"list", "--threshold-5h", "80"}, {"project", "current", "--threshold-7d", "80"},
	} {
		if err := a.run(args); err == nil {
			t.Fatal("accepted invalid or misplaced thresholds", args)
		}
	}
	after, _ := os.ReadFile(filepath.Join(a.Root, "settings.json"))
	if string(before) != string(after) {
		t.Fatal("invalid CLI options changed settings")
	}
}

func TestWindowThresholdOneShotOverridesStayTemporary(t *testing.T) {
	a := fixture(t)
	helperCLI(t, a)
	if err := a.run([]string{"auto", "status", "--threshold-7d", "80"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(a.Root, "settings.json"))
	for _, tc := range []struct{ value, message string }{
		{"60", "No eligible account"}, {"global", "7d 90%"},
	} {
		if err := a.run([]string{"auto", "--once", "--dry-run", "--threshold-7d", tc.value}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(a.autoState().Message, tc.message) || a.selected() != "default" {
			t.Fatal("one-shot did not use temporary window rules", a.autoState())
		}
	}
	after, _ := os.ReadFile(filepath.Join(a.Root, "settings.json"))
	if string(before) != string(after) {
		t.Fatal("one-shot changed persistent configuration")
	}
}

func TestWindowThresholdEvaluationUsesLatestLockedSettings(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	now := time.Now().Unix()
	records := map[string]Record{"default": quota(30, 82, now), "work": quota(20, 70, now)}
	s, _ := a.settings()
	s.Auto.Enabled = true
	s.Auto.Threshold7d = pointer(85)
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), s); err != nil {
		t.Fatal(err)
	}
	result, err := a.evaluate(records, "default", false, false, autoOverrides{Threshold: 95})
	if err != nil || result.Switched || result.Target != "" {
		t.Fatal("temporary global setting discarded latest per-window rule", result, err)
	}
	s.Auto.Threshold7d = pointer(80)
	s.Auto.Cooldown = 1200
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), s); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(a.Root, "last-switch"), []byte(strconv.FormatInt(now-600, 10))); err != nil {
		t.Fatal(err)
	}
	result, err = a.evaluate(records, "default", false, false, autoOverrides{Threshold: 95})
	if err != nil || result.Switched || !strings.Contains(result.Message, "1200s cooldown") {
		t.Fatal("temporary override discarded latest cooldown", result, err)
	}
	s.Auto.Enabled = false
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), s); err != nil {
		t.Fatal(err)
	}
	result, err = a.evaluate(records, "default", false, false, autoOverrides{Windows: windowThresholdOverrides{"7d": pointer(80)}})
	if err != nil || result.Switched || result.Target != "" || a.selected() != "default" {
		t.Fatal("window override bypassed the disabled monitor", result, err)
	}
}

func TestWindowThresholdAutoViewAndGlobalKeys(t *testing.T) {
	a := fixture(t)
	now := time.Now()
	s, _ := a.settings()
	s.Auto.Threshold5h, s.Auto.Threshold7d = pointer(90), pointer(80)
	if err := writeJSON(filepath.Join(a.Root, "settings.json"), s); err != nil {
		t.Fatal(err)
	}
	p := Panel{Mode: "auto", Width: 100, Height: 24, Records: map[string]Record{"default": quota(30, 80, now.Unix())}}
	view := stripANSI(p.render(a, a.names(), s, now))
	for _, text := range []string{"5h 90%", "7d 80%", "global 90%", "at window limit", "+/- Global"} {
		if !strings.Contains(view, text) {
			t.Fatal("view did not use effective window rules", text, view)
		}
	}
	if _, err := p.key(a, a.names(), "+"); err != nil {
		t.Fatal(err)
	}
	s, err := a.settings()
	if err != nil || s.Auto.Threshold != 95 || *s.Auto.Threshold5h != 90 || *s.Auto.Threshold7d != 80 {
		t.Fatal("global adjustment changed explicit window limits", s, err)
	}
}
