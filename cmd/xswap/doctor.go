package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type doctorStatus string

const (
	doctorPass    doctorStatus = "PASS"
	doctorWarning doctorStatus = "WARN"
	doctorFail    doctorStatus = "FAIL"
)

type doctorCheck struct {
	Name   string
	Status doctorStatus
	Detail string
	Fix    string
}

type doctorReport struct {
	Checks   []doctorCheck
	ExitCode int
}

type doctorExitError int

func (e doctorExitError) Error() string { return "doctor reported issues" }
func (e doctorExitError) ExitCode() int { return int(e) }

func (a *App) doctorReport() doctorReport {
	checks := []doctorCheck{
		{Name: "version", Status: doctorPass, Detail: fmt.Sprintf("XSwap %s (commit %s, built %s)", version, commit, buildDate)},
		platformDoctorCheck(),
		a.codexCLIDoctorCheck(),
		a.wrapperDoctorCheck(),
		codeHomeDoctorCheck(),
		a.managerDirectoryDoctorCheck(),
		a.profileDoctorCheck(),
		a.configurationDoctorCheck(),
		a.autoMonitorDoctorCheck(),
	}
	report := doctorReport{Checks: checks}
	for _, check := range checks {
		switch check.Status {
		case doctorFail:
			report.ExitCode = 2
		case doctorWarning:
			if report.ExitCode == 0 {
				report.ExitCode = 1
			}
		}
	}
	return report
}

func (a *App) doctor(out io.Writer) int {
	report := a.doctorReport()
	for _, check := range report.Checks {
		fmt.Fprintf(out, "[%s] %s: %s\n", check.Status, check.Name, check.Detail)
		if check.Fix != "" {
			fmt.Fprintf(out, "       Fix: %s\n", check.Fix)
		}
	}
	switch report.ExitCode {
	case 0:
		fmt.Fprintln(out, "Doctor result: healthy.")
	case 1:
		fmt.Fprintln(out, "Doctor result: warnings found.")
	default:
		fmt.Fprintln(out, "Doctor result: failed checks found.")
	}
	return report.ExitCode
}

func platformDoctorCheck() doctorCheck {
	check := doctorCheck{Name: "platform", Status: doctorPass, Detail: runtime.GOOS + "/" + runtime.GOARCH}
	supported := map[string]bool{
		"darwin/amd64": true, "darwin/arm64": true,
		"linux/amd64": true, "linux/arm64": true,
		"windows/amd64": true, "windows/arm64": true,
	}
	if !supported[runtime.GOOS+"/"+runtime.GOARCH] {
		check.Status = doctorWarning
		check.Detail += " is not a release target"
		check.Fix = "check the supported platforms in the XSwap documentation before installing a release build"
	} else {
		check.Detail += " is supported"
	}
	return check
}

func (a *App) codexCLIDoctorCheck() doctorCheck {
	check := doctorCheck{Name: "Codex CLI"}
	path, err := a.original()
	if err != nil {
		check.Status = doctorFail
		check.Detail = "the official Codex CLI is missing or its saved path is unavailable"
		check.Fix = "install the official Codex CLI, then run: xswap install"
		return check
	}
	check.Status = doctorPass
	check.Detail = path
	return check
}

func codeHomeDoctorCheck() doctorCheck {
	check := doctorCheck{Name: "CODEX_HOME", Status: doctorPass}
	if os.Getenv("CODEX_HOME") != "" {
		check.Detail = "explicitly set; it takes precedence over XSwap account and project selections"
	} else {
		check.Detail = "not set; XSwap account and project selections apply"
	}
	return check
}

func (a *App) managerDirectoryDoctorCheck() doctorCheck {
	check := doctorCheck{Name: "manager directory", Status: doctorPass}
	info, err := os.Stat(a.Root)
	if os.IsNotExist(err) {
		check.Detail = "not created yet; XSwap creates it when account data is first saved"
		return check
	}
	if err != nil {
		check.Status = doctorFail
		check.Detail = "cannot access the manager directory"
		check.Fix = "check the directory owner and permissions, then rerun: xswap doctor"
		return check
	}
	if !info.IsDir() {
		check.Status = doctorFail
		check.Detail = "the manager path exists but is not a directory"
		check.Fix = "move the conflicting file out of the way, then rerun: xswap doctor"
		return check
	}
	if _, err = os.ReadDir(a.Root); err != nil {
		check.Status = doctorFail
		check.Detail = "the manager directory cannot be listed"
		check.Fix = "check the directory owner and permissions, then rerun: xswap doctor"
		return check
	}
	if status, detail, fix := managerPermissionDoctorCheck(a.Root, info); status != doctorPass {
		check.Status, check.Detail, check.Fix = status, detail, fix
		return check
	} else if detail != "" {
		check.Detail = detail
		return check
	}
	check.Detail = "accessible"
	return check
}

func (a *App) profileDoctorCheck() doctorCheck {
	check := doctorCheck{Name: "profiles", Status: doctorPass}
	pending := []string{}
	for _, name := range a.names() {
		if name == "default" {
			continue
		}
		home, err := a.profile(name)
		if err != nil {
			continue
		}
		if _, err = os.Stat(filepath.Join(home, "auth.json")); os.IsNotExist(err) {
			pending = append(pending, name)
		} else if err != nil {
			check.Status = doctorFail
			check.Detail = "could not inspect saved-login state for a named profile"
			check.Fix = "check profile directory permissions, then rerun: xswap doctor"
			return check
		}
	}
	if len(pending) == 0 {
		check.Detail = fmt.Sprintf("%d named account profile(s); no pending logins", len(a.names())-1)
		return check
	}
	check.Status = doctorWarning
	check.Detail = fmt.Sprintf("%d named account profile(s) still need login: %v", len(pending), pending)
	check.Fix = "complete login for each pending account with: xswap login NAME"
	return check
}

func (a *App) configurationDoctorCheck() doctorCheck {
	check := doctorCheck{Name: "configuration"}
	if _, err := a.settings(); err != nil {
		check.Status = doctorFail
		check.Detail = "settings.json is unreadable, invalid JSON, or contains out-of-range values"
		check.Fix = fmt.Sprintf("back up and repair %q; auto-switch threshold must be 1–100, interval at least 10, cooldown and margin nonnegative", filepath.Join(a.Root, "settings.json"))
		return check
	}
	check.Status = doctorPass
	check.Detail = "settings are valid"
	return check
}

func (a *App) autoMonitorDoctorCheck() doctorCheck {
	check := doctorCheck{Name: "auto-switch monitor", Status: doctorPass}
	settings, err := a.settings()
	if err != nil {
		check.Status = doctorWarning
		check.Detail = "cannot inspect monitor state until settings are repaired"
		check.Fix = "repair the XSwap settings and rerun: xswap doctor"
		return check
	}
	if !settings.Auto.Enabled {
		check.Detail = "disabled"
		return check
	}
	var metadata struct {
		PID       int   `json:"pid"`
		Heartbeat int64 `json:"heartbeat"`
	}
	data, err := os.ReadFile(filepath.Join(a.Root, "auto-daemon.json"))
	if err == nil {
		err = json.Unmarshal(data, &metadata)
	}
	maxAge := max(120, settings.Auto.Interval*3)
	age := time.Now().Unix() - metadata.Heartbeat
	if err == nil && metadata.PID > 0 && metadata.Heartbeat > 0 && age >= -30 && age <= int64(maxAge) && managedProcessAlive(metadata.PID) {
		check.Detail = "enabled and running"
		return check
	}
	check.Status = doctorWarning
	check.Detail = "enabled, but no live monitor with a recent heartbeat was found"
	check.Fix = "restart the monitor with: xswap auto on"
	return check
}
