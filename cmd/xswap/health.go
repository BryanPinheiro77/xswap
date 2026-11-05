package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	authUnknown       = "not checked"
	authPending       = "login pending"
	authSaved         = "login saved; not verified"
	authAuthenticated = "authenticated"
	authInvalid       = "expired or invalid login"
	authAPIKey        = "API key; subscription quotas unsupported"
	authUnsupported   = "unsupported account type"
)

// AccountHealth contains observations only: never credentials or server payloads.
type AccountHealth struct {
	Authentication string `json:"authentication"`
	Checked        int64  `json:"checked,omitempty"`
	LastSuccess    int64  `json:"lastSuccess,omitempty"`
	StaleSince     int64  `json:"staleSince,omitempty"`
	Error          string `json:"error,omitempty"`
}

func (a *App) healthPath(name string) string {
	return filepath.Join(a.Root, "health", name+".json")
}

func (a *App) savedHealth(name string) AccountHealth {
	var health AccountHealth
	if validate(name) == nil {
		if readJSON(a.healthPath(name), &health) != nil {
			return AccountHealth{}
		}
	}
	return health
}

func (a *App) localAuthentication(name string) string {
	home, err := a.profile(name)
	if err != nil {
		return authUnknown
	}
	info, err := os.Stat(filepath.Join(home, "auth.json"))
	if err == nil && info.Mode().IsRegular() {
		return authSaved
	}
	if os.IsNotExist(err) && name != "default" {
		return authPending
	}
	// The original account may use the OS credential store.
	return authUnknown
}

func authenticationRejected(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	var remote *appServerError
	if errors.As(err, &remote) {
		message = strings.ToLower(remote.message)
	}
	for _, evidence := range []string{"401 unauthorized", "token expired", "token has expired", "refresh token has already been used", "refresh token has been revoked", "authentication required to read rate limits"} {
		if strings.Contains(message, evidence) {
			return true
		}
	}
	return false
}

func observeHealth(previous AccountHealth, current Record, err error, now int64) AccountHealth {
	health := current.Health
	health.Checked = now
	health.LastSuccess = previous.LastSuccess
	if health.Authentication == "" {
		health.Authentication = authUnknown
	}
	if err == nil {
		health.LastSuccess = current.Updated
		return health
	}
	if authenticationRejected(err) {
		health.Authentication = authInvalid
	}
	// Persist only fixed diagnostic categories, never arbitrary CLI error text.
	switch {
	case health.Authentication == authPending:
		health.Error = "Login required"
	case health.Authentication == authInvalid:
		health.Error = "Authentication rejected; log in again"
	case health.Authentication == authAPIKey || health.Authentication == authUnsupported:
		health.Error = "Subscription quotas require a ChatGPT account"
	case errors.Is(err, context.DeadlineExceeded):
		health.Error = "Quota query timed out"
	case errors.Is(err, context.Canceled):
		health.Error = "Quota query cancelled"
	case errors.Is(err, errIncompleteQuota):
		health.Error = "Quota response incomplete"
	default:
		health.Error = "Codex query failed"
	}
	if health.LastSuccess > 0 {
		health.StaleSince = previous.StaleSince
		if health.StaleSince == 0 {
			health.StaleSince = now
		}
	}
	return health
}

func (a *App) accountHealthLabel(name string, health AccountHealth) string {
	status := health.Authentication
	if status == "" {
		status = a.localAuthentication(name)
	}
	if name == "default" {
		status = "original account · " + status
	}
	return status
}

func healthDetails(health AccountHealth) []string {
	var details []string
	if health.Checked > 0 {
		details = append(details, "Last check: "+healthTime(health.Checked))
	}
	if health.LastSuccess > 0 {
		details = append(details, "Last successful quota query: "+healthTime(health.LastSuccess))
	}
	if health.StaleSince > 0 {
		details = append(details, "Quota data stale since "+healthTime(health.StaleSince))
	}
	if health.Error != "" {
		details = append(details, "Last error: "+clean(health.Error))
	}
	return details
}

func healthTime(timestamp int64) string {
	return time.Unix(timestamp, 0).Format("2006-01-02 15:04:05 MST")
}

func (a *App) printHealth(out io.Writer, name string, health AccountHealth) {
	fmt.Fprintln(out, "  Status:", a.accountHealthLabel(name, health))
	for _, detail := range healthDetails(health) {
		fmt.Fprintln(out, "  "+detail)
	}
}
