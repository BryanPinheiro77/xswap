package main

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

const (
	fiveHourMinutes = 300
	sevenDayMinutes = 10080
)

func (config AutoConfig) validate() error {
	if config.Threshold < 1 || config.Threshold > 100 || config.Interval < 10 || config.Cooldown < 0 || config.Margin < 0 {
		return errors.New("invalid auto-switch configuration")
	}
	for _, threshold := range []*int{config.Threshold5h, config.Threshold7d} {
		if threshold != nil && (*threshold < 1 || *threshold > 100) {
			return errors.New("auto-switch window thresholds must be between 1 and 100%")
		}
	}
	return nil
}

func (config AutoConfig) thresholdForWindow(minutes int) int {
	switch minutes {
	case fiveHourMinutes:
		if config.Threshold5h != nil {
			return *config.Threshold5h
		}
	case sevenDayMinutes:
		if config.Threshold7d != nil {
			return *config.Threshold7d
		}
	}
	return config.Threshold
}

func (config AutoConfig) thresholdSummary() string {
	return fmt.Sprintf("5h %d%% · 7d %d%% · global %d%%", config.thresholdForWindow(fiveHourMinutes), config.thresholdForWindow(sevenDayMinutes), config.Threshold)
}

// rotationPressure is the negative of the smallest remaining headroom, in
// percentage points. Equal thresholds preserve the legacy max-usage ordering
// and improvement margin. A negative value means every window is below its limit.
func rotationPressure(record Record, config AutoConfig, now int64, freshness int) (float64, bool) {
	if _, valid := quotaScore(record, now, freshness); !valid {
		return 0, false
	}
	bucket := allBuckets(record.Limits)["codex"]
	pressure := math.Inf(-1)
	found5h, found7d := false, false
	for _, window := range []*Window{bucket.Primary, bucket.Secondary} {
		if window == nil {
			continue
		}
		minutes := *window.Minutes
		found5h = found5h || minutes == fiveHourMinutes
		found7d = found7d || minutes == sevenDayMinutes
		pressure = max(pressure, *window.Used-float64(config.thresholdForWindow(minutes)))
	}
	// An explicit rule cannot be evaluated without its matching window.
	if (config.Threshold5h != nil && !found5h) || (config.Threshold7d != nil && !found7d) {
		return 0, false
	}
	return pressure, true
}

// Map membership distinguishes leaving a setting alone from clearing it (nil).
type windowThresholdOverrides map[string]*int

func parseWindowThresholdOverrides(o Options) (windowThresholdOverrides, error) {
	overrides := windowThresholdOverrides{}
	for _, window := range []string{"5h", "7d"} {
		key := "threshold-" + window
		value, present := o.Values[key]
		if !present {
			continue
		}
		if value == "global" {
			overrides[window] = nil
			continue
		}
		threshold, err := strconv.Atoi(value)
		if err != nil || threshold < 1 || threshold > 100 {
			return nil, fmt.Errorf("--%s must be between 1 and 100, or global", key)
		}
		overrides[window] = &threshold
	}
	return overrides, nil
}

type autoOverrides struct {
	Threshold int
	Interval  int
	Windows   windowThresholdOverrides
}

func (overrides autoOverrides) apply(config *AutoConfig) {
	if overrides.Threshold != 0 {
		config.Threshold = overrides.Threshold
	}
	if overrides.Interval != 0 {
		config.Interval = overrides.Interval
	}
	if threshold, present := overrides.Windows["5h"]; present {
		config.Threshold5h = threshold
	}
	if threshold, present := overrides.Windows["7d"]; present {
		config.Threshold7d = threshold
	}
}
