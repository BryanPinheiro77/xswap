package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
)

type SwitchEvent struct {
	At   int64  `json:"at"`
	From string `json:"from"`
	To   string `json:"to"`
}
type AutoState struct {
	Checked  int64             `json:"checked"`
	Active   string            `json:"active"`
	Target   string            `json:"target"`
	Switched bool              `json:"switched"`
	DryRun   bool              `json:"dryRun"`
	Message  string            `json:"message"`
	Records  map[string]Record `json:"records"`
	History  []SwitchEvent     `json:"history"`
}

func (a *App) autoState() AutoState {
	var state AutoState
	data, _ := os.ReadFile(filepath.Join(a.Root, "auto-state.json"))
	json.Unmarshal(data, &state)
	return state
}
func quotaScore(record Record, now int64, freshness int) (float64, bool) {
	if record.Error != "" || record.Updated == 0 || now-record.Updated > int64(freshness) || record.Account.Type != "chatgpt" {
		return 0, false
	}
	bucket, ok := allBuckets(record.Limits)["codex"]
	if !ok {
		return 0, false
	}
	score := 0.0
	found := false
	for _, window := range []*Window{bucket.Primary, bucket.Secondary} {
		if window == nil {
			continue
		}
		if window.Used == nil || math.IsNaN(*window.Used) || math.IsInf(*window.Used, 0) || *window.Used < 0 || window.Resets == nil || *window.Resets <= now {
			return 0, false
		}
		score = max(score, *window.Used)
		found = true
	}
	return score, found
}
func decide(records map[string]Record, active string, s Settings, now, last int64) (string, string) {
	if disabled(s, active) {
		return "", "Active account is disabled for rotation; selection held."
	}
	current, ok := quotaScore(records[active], now, max(120, s.Auto.Interval*2))
	if !ok {
		return "", "Active account quotas unavailable; selection held."
	}
	if current < float64(s.Auto.Threshold) {
		return "", fmt.Sprintf("Active account is below the %d%% threshold.", s.Auto.Threshold)
	}
	if now-last < int64(s.Auto.Cooldown) {
		return "", "Waiting for the 5-minute cooldown between switches."
	}
	type candidate struct {
		name  string
		score float64
	}
	candidates := []candidate{}
	for name, record := range records {
		if name == active || disabled(s, name) {
			continue
		}
		score, ok := quotaScore(record, now, max(120, s.Auto.Interval*2))
		if ok && score < float64(s.Auto.Threshold) && current-score >= float64(s.Auto.Margin) {
			candidates = append(candidates, candidate{name, score})
		}
	}
	if len(candidates) == 0 {
		return "", "No eligible account with sufficient remaining quota."
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].name < candidates[j].name
		}
		return candidates[i].score < candidates[j].score
	})
	target := candidates[0].name
	return target, fmt.Sprintf("%s reached %g%% usage; best alternative: %s.", active, current, target)
}
func (a *App) collect(ctx context.Context) map[string]Record {
	records := map[string]Record{}
	s, err := a.settings()
	if err != nil {
		return records
	}
	for _, name := range a.names() {
		if ctx.Err() != nil {
			break
		}
		if disabled(s, name) {
			continue
		}
		record, err := a.readLimits(ctx, name)
		if err != nil {
			record = Record{Error: err.Error(), Updated: time.Now().Unix()}
		}
		records[name] = record
	}
	return records
}
func (a *App) evaluate(records map[string]Record, active string, dryRun, force bool, overrides ...AutoConfig) (AutoState, error) {
	result := AutoState{}
	err := a.withState(func() error {
		s, err := a.settings()
		if err != nil {
			return err
		}
		if !force && !s.Auto.Enabled {
			result.Message = "Auto-switch is off."
			return nil
		}
		if len(overrides) > 0 {
			s.Auto = overrides[0]
		}
		if a.selected() != active {
			result.Message = "Selection changed during the query; waiting for a fresh check."
			return nil
		}
		now := time.Now().Unix()
		lastData, _ := os.ReadFile(filepath.Join(a.Root, "last-switch"))
		last, _ := strconv.ParseInt(stringTrim(lastData), 10, 64)
		target, message := decide(records, active, s, now, last)
		previous := a.autoState()
		history := previous.History
		if len(history) > 10 {
			history = history[len(history)-10:]
		}
		result = AutoState{Checked: now, Active: active, Target: target, DryRun: dryRun, Message: message, Records: records, History: history}
		if target != "" && !dryRun {
			home, err := a.require(target)
			if err != nil {
				return err
			}
			if target != "default" && !exists(filepath.Join(home, "auth.json")) {
				result.Target = ""
				result.Message = "Candidate login unavailable; selection held."
			} else {
				if err = atomicWrite(filepath.Join(a.Root, "active"), []byte(target+"\n")); err != nil {
					return err
				}
				if err = atomicWrite(filepath.Join(a.Root, "last-switch"), []byte(strconv.FormatInt(now, 10))); err != nil {
					return err
				}
				result.Active = target
				result.Switched = true
				result.History = append(result.History, SwitchEvent{now, active, target})
				if len(result.History) > 10 {
					result.History = result.History[len(result.History)-10:]
				}
			}
		}
		return writeJSON(filepath.Join(a.Root, "auto-state.json"), result)
	})
	return result, err
}
func (a *App) configureAuto(enabled bool, threshold, interval int) error {
	if threshold != 0 && (threshold < 1 || threshold > 100) {
		return errors.New("threshold must be between 1 and 100%")
	}
	if interval != 0 && interval < 10 {
		return errors.New("minimum refresh interval is 10 seconds")
	}
	err := a.withState(func() error {
		s, err := a.settings()
		if err != nil {
			return err
		}
		s.Auto.Enabled = enabled
		if threshold != 0 {
			s.Auto.Threshold = threshold
		}
		if interval != 0 {
			s.Auto.Interval = interval
		}
		return writeJSON(filepath.Join(a.Root, "settings.json"), s)
	})
	if err == nil && enabled {
		a.ensureDaemon()
	}
	return err
}
func (a *App) daemonRunning() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	unlock, err := a.lock(ctx, "auto-daemon.lock")
	if err != nil {
		return errors.Is(err, context.DeadlineExceeded)
	}
	unlock()
	return false
}
func (a *App) ensureDaemon() {
	s, err := a.settings()
	if err != nil || !s.Auto.Enabled || a.daemonRunning() {
		return
	}
	cmd := exec.Command(a.Binary, "__daemon")
	cmd.Env = envWith(os.Environ(), "CODEX_SWAP_HOME", a.Root)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err == nil {
		cmd.Process.Release()
	}
}
func (a *App) daemon() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	try, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	unlock, err := a.lock(try, "auto-daemon.lock")
	stop()
	if err != nil {
		return nil
	}
	defer unlock()
	defer os.Remove(filepath.Join(a.Root, "auto-daemon.json"))
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s, err := a.settings()
				if err != nil || !s.Auto.Enabled {
					cancel()
					return
				}
			}
		}
	}()
	for ctx.Err() == nil {
		s, err := a.settings()
		if err != nil {
			return err
		}
		if !s.Auto.Enabled {
			return nil
		}
		writeJSON(filepath.Join(a.Root, "auto-daemon.json"), map[string]any{"pid": os.Getpid(), "heartbeat": time.Now().Unix()})
		active := a.selected()
		records := a.collect(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if _, err = a.evaluate(records, active, false, false); err != nil {
			return err
		}
		writeJSON(filepath.Join(a.Root, "auto-daemon.json"), map[string]any{"pid": os.Getpid(), "heartbeat": time.Now().Unix()})
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Duration(s.Auto.Interval) * time.Second):
		}
	}
	return nil
}
func (a *App) autoCommand(operation string, o Options) error {
	if operation == "" {
		operation = "status"
	}
	if o.Flags["once"] {
		operation = "once"
	}
	if operation == "view" {
		return a.dashboard("auto", "", 60)
	}
	threshold, err := o.integer("threshold", 0)
	if err != nil {
		return err
	}
	interval, err := o.integer("interval", 0)
	if err != nil {
		return err
	}
	if threshold < 0 || threshold > 100 || (o.Values["threshold"] != "" && threshold == 0) {
		return errors.New("threshold must be between 1 and 100%")
	}
	if interval < 0 || (o.Values["interval"] != "" && interval < 10) {
		return errors.New("minimum refresh interval is 10 seconds")
	}
	s, err := a.settings()
	if err != nil {
		return err
	}
	switch operation {
	case "on", "off":
		err = a.configureAuto(operation == "on", threshold, interval)
	case "once":
		// Single checks use temporary overrides and never alter monitor settings.
	case "status":
		if threshold != 0 || interval != 0 {
			err = a.configureAuto(s.Auto.Enabled, threshold, interval)
		}
	default:
		return errors.New("usage: xswap auto on|off|status|view or xswap auto --once --dry-run")
	}
	if err != nil {
		return err
	}
	if operation == "once" {
		active := a.selected()
		effective := s.Auto
		if threshold != 0 {
			effective.Threshold = threshold
		}
		if interval != 0 {
			effective.Interval = interval
		}
		result, err := a.evaluate(a.collect(context.Background()), active, o.Flags["dry-run"], true, effective)
		if err != nil {
			return err
		}
		prefix := ""
		if o.Flags["dry-run"] {
			prefix = "Dry run: "
		}
		fmt.Println(prefix + result.Message)
		if result.Switched {
			fmt.Printf("Selected: %s. Applies to new Codex processes.\n", result.Active)
		}
		return nil
	}
	s, _ = a.settings()
	status := "off"
	if s.Auto.Enabled {
		status = "on"
	}
	fmt.Printf("Auto-switch: %s · threshold %d%% · refresh %ds\n", status, s.Auto.Threshold, s.Auto.Interval)
	if s.Auto.Enabled {
		fmt.Printf("Background monitor running: %t\n", a.daemonRunning())
	}
	fmt.Println("New Codex processes use the selected account; existing processes keep their account.")
	state := a.autoState()
	if state.Message != "" {
		fmt.Println(clean(state.Message))
	}
	return nil
}
