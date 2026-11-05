package main

import (
	"errors"
	"sort"
	"time"
)

type sessionTimeFilter string

const (
	sessionFilterAll    sessionTimeFilter = ""
	sessionFilterRecent sessionTimeFilter = "recent"
	sessionFilterDay    sessionTimeFilter = "24h"
	sessionFilterWeek   sessionTimeFilter = "7d"
	recentSessionLimit                    = 20
)

func parseSessionTimeFilter(value string) (sessionTimeFilter, error) {
	switch value {
	case "all":
		return sessionFilterAll, nil
	case "recent", "24h", "7d":
		return sessionTimeFilter(value), nil
	default:
		return sessionFilterAll, errors.New("--sessions must be recent, 24h, 7d, or all")
	}
}

func (filter sessionTimeFilter) label() string {
	switch filter {
	case sessionFilterRecent:
		return "Recent sessions (latest 20)"
	case sessionFilterDay:
		return "Last 24 hours"
	case sessionFilterWeek:
		return "Last 7 days"
	default:
		return "All sessions"
	}
}

func filterSessions(sessions []codexSession, filter sessionTimeFilter, now time.Time) []codexSession {
	var cutoff time.Time
	switch filter {
	case sessionFilterDay:
		cutoff = now.Add(-24 * time.Hour)
	case sessionFilterWeek:
		cutoff = now.Add(-7 * 24 * time.Hour)
	}
	result := make([]codexSession, 0, len(sessions))
	for _, session := range sessions {
		if cutoff.IsZero() || !session.Updated.Before(cutoff) {
			result = append(result, session)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Updated.Equal(result[j].Updated) {
			return result[i].ID < result[j].ID
		}
		return result[i].Updated.After(result[j].Updated)
	})
	if filter == sessionFilterRecent && len(result) > recentSessionLimit {
		result = result[:recentSessionLimit]
	}
	return result
}

func (p *Panel) setSessionTimeFilter(filter sessionTimeFilter) {
	if p.HandoffAllSessions == nil {
		p.HandoffAllSessions = append([]codexSession{}, p.HandoffSessions...)
	}
	if p.HandoffFilterTime.IsZero() {
		p.HandoffFilterTime = time.Now()
	}
	p.HandoffTimeFilter = filter
	p.HandoffSessions = filterSessions(p.HandoffAllSessions, filter, p.HandoffFilterTime)
	p.HandoffSelected = selectedSessionMap(p.HandoffSessions)
	p.HandoffSources = sessionAccounts(p.HandoffSessions)
	p.Cursor, p.Offset = 0, 0
	p.Message = ""
}

func (a *App) planFilteredProjectHandoff(directory, target string, filter sessionTimeFilter, now time.Time) (projectHandoffPlan, error) {
	if filter == sessionFilterAll {
		return a.planProjectHandoff(directory, target)
	}
	source, err := a.projectHandoffSource(directory)
	if err != nil {
		return projectHandoffPlan{}, err
	}
	sessions := filterSessions(source.Sessions, filter, now)
	if len(sessions) == 0 {
		return projectHandoffPlan{}, errors.New("no sessions match the filter; use --sessions all to include older conversations")
	}
	return a.planSelectedProjectHandoff(directory, target, selectedSessionMap(sessions))
}
