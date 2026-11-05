package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAccountHealthObservationsAndRecovery(t *testing.T) {
	a := fixture(t)
	helperCLI(t, a)
	ready(t, a, "work")
	first, err := a.readLimits(context.Background(), "work")
	if err != nil || first.Health.Authentication != authAuthenticated || first.Health.LastSuccess != first.Updated || first.Health.Checked == 0 {
		t.Fatal("missing successful observation", first, err)
	}
	for _, tc := range []struct {
		mode, auth, diagnostic string
	}{
		{"failure", authSaved, "Codex query failed"},
		{"expired", authInvalid, "Authentication rejected"},
		{"pending", authInvalid, "Authentication rejected"},
		{"api-key", authAPIKey, "Subscription quotas require"},
		{"unknown-type", authUnsupported, "Subscription quotas require"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv("XSWAP_TEST_AUTH_MODE", tc.mode)
			current, err := a.readLimits(context.Background(), "work")
			if err == nil || current.Health.Authentication != tc.auth || current.Health.LastSuccess != first.Updated || current.Health.StaleSince == 0 || !strings.Contains(current.Health.Error, tc.diagnostic) {
				t.Fatal("incorrect failure observation", current, err)
			}
			stale := reconcileQuotaRecord(first, current, err.Error())
			if stale.Health.Authentication != tc.auth || stale.Updated != first.Updated || !reflect.DeepEqual(stale.Limits, first.Limits) || quotaRecordState(stale) != quotaStale {
				t.Fatal("health update lost the last valid quota snapshot", stale)
			}
			if _, eligible := quotaScore(stale, time.Now().Unix(), 120); eligible {
				t.Fatal("failed query became eligible for rotation")
			}
			if cached := a.savedHealth("work"); cached != current.Health {
				t.Fatal("list would not see the last observation", cached, current.Health)
			}
		})
	}
	recovered, err := a.readLimits(context.Background(), "work")
	if err != nil || recovered.Health.Authentication != authAuthenticated || recovered.Health.Error != "" || recovered.Health.StaleSince != 0 || recovered.Health.LastSuccess != recovered.Updated {
		t.Fatal("account did not recover", recovered, err)
	}
	data, err := os.ReadFile(a.healthPath("work"))
	if err != nil || strings.Contains(string(data), "test@example.com") || strings.Contains(string(data), "usedPercent") || strings.Contains(string(data), "private-test-error-value") {
		t.Fatal("health cache contains data beyond fixed observations", err)
	}
	info, err := os.Stat(a.healthPath("work"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("health cache is not private", err)
	}
}

func TestAccountHealthPendingAndOfflineList(t *testing.T) {
	a := fixture(t)
	helperCLI(t, a)
	if _, err := a.create("pending"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XSWAP_TEST_AUTH_MODE", "pending")
	record, err := a.readLimits(context.Background(), "pending")
	if err == nil || record.Health.Authentication != authPending || record.Health.LastSuccess != 0 || record.Health.StaleSince != 0 {
		t.Fatal("pending login misclassified", record, err)
	}
	if err := os.Remove(filepath.Join(a.Root, "installation.json")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := a.listTo(&out); err != nil {
		t.Fatal("offline list failed", err)
	}
	for _, text := range []string{"original account", "login pending", "Last check:", "Last error: Login required"} {
		if !strings.Contains(out.String(), text) {
			t.Fatalf("list missing %q: %s", text, out.String())
		}
	}
	s, _ := a.settings()
	p := Panel{Mode: "watch", Width: 150, Height: 30, Records: map[string]Record{"pending": record}}
	view := stripANSI(p.render(a, []string{"pending"}, s, time.Now()))
	if !strings.Contains(view, "Status: login pending") || !strings.Contains(view, "Last error: Login required") {
		t.Fatal("watch missing health details", view)
	}
}

func TestAccountHealthFailurePrivacyAndOneShotOutput(t *testing.T) {
	a := fixture(t)
	helperCLI(t, a)
	t.Setenv("XSWAP_TEST_AUTH_MODE", "failure")
	var out bytes.Buffer
	if err := a.showLimitsTo(context.Background(), []string{"default"}, &out); err == nil {
		t.Fatal("failed query reported success")
	}
	data, err := os.ReadFile(a.healthPath("default"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String()+string(data), "private-test-error-value") {
		t.Fatal("remote error payload leaked")
	}
	if !strings.Contains(out.String(), "Last error: Codex query failed") || !strings.Contains(out.String(), "login saved; not verified") {
		t.Fatal("one-shot watch did not explain verification failure", out.String())
	}
}

func TestAccountHealthDiagnosticsAndCacheFailures(t *testing.T) {
	previous := AccountHealth{LastSuccess: 10, StaleSince: 20}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "Quota query timed out"},
		{context.Canceled, "Quota query cancelled"},
		{errIncompleteQuota, "Quota response incomplete"},
		{errors.New("server offline"), "Codex query failed"},
	} {
		health := observeHealth(previous, Record{}, tc.err, 30)
		if health.Error != tc.want || health.StaleSince != 20 || health.LastSuccess != 10 || health.Authentication == authInvalid {
			t.Fatal("incorrect failure classification", health)
		}
	}
	a := fixture(t)
	helperCLI(t, a)
	if err := os.WriteFile(filepath.Join(a.Root, "health"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	record, err := a.readLimits(context.Background(), "default")
	if err != nil || record.Health.Authentication != authAuthenticated {
		t.Fatal("cache failure broke a valid query", record, err)
	}
	if _, eligible := quotaScore(record, time.Now().Unix(), 120); !eligible {
		t.Fatal("cache failure changed rotation eligibility")
	}
}

func TestCorruptHealthCacheIsIgnored(t *testing.T) {
	a := fixture(t)
	if err := atomicWrite(a.healthPath("default"), []byte(`{"authentication":"authenticated","checked":"invalid"}`)); err != nil {
		t.Fatal(err)
	}
	if health := a.savedHealth("default"); health != (AccountHealth{}) {
		t.Fatal("partially decoded health cache was trusted", health)
	}
}

func TestRecreatedAccountDoesNotInheritHealth(t *testing.T) {
	a := fixture(t)
	ready(t, a, "work")
	if err := writeJSON(a.healthPath("work"), AccountHealth{Authentication: authInvalid, Checked: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.remove("work"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.create("work"); err != nil {
		t.Fatal(err)
	}
	if health := a.savedHealth("work"); health.Checked != 0 || health.Authentication != "" {
		t.Fatal("recreated profile inherited health", health)
	}
	if label := a.accountHealthLabel("work", a.savedHealth("work")); label != authPending {
		t.Fatal(label)
	}
}
