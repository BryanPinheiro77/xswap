package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var sessionIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type codexSession struct {
	ID      string
	CWD     string
	Path    string
	Preview string
	Created time.Time
	Updated time.Time
}

type sessionEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type sessionMeta struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
}

func previewFromEnvelope(line []byte) string {
	var envelope sessionEnvelope
	if json.Unmarshal(line, &envelope) != nil {
		return ""
	}
	var payload struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Message string `json:"message"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(envelope.Payload, &payload) != nil {
		return ""
	}
	if payload.Type == "user_message" && strings.TrimSpace(payload.Message) != "" {
		return userSessionPreview(payload.Message)
	}
	if payload.Role == "user" {
		parts := []string{}
		for _, item := range payload.Content {
			if (item.Type == "input_text" || item.Type == "text") && strings.TrimSpace(item.Text) != "" {
				parts = append(parts, strings.TrimSpace(item.Text))
			}
		}
		return userSessionPreview(strings.Join(parts, " "))
	}
	return ""
}

func userSessionPreview(message string) string {
	message = strings.TrimSpace(message)
	// Codex can record runtime metadata and injected repository instructions as
	// user messages before the first real prompt. They describe the execution
	// environment, not the conversation.
	technicalPrefixes := []string{
		"<environment_context>",
		"<permissions instructions>",
		"<skills_instructions>",
		"<collaboration_mode>",
		"<apps_instructions>",
		"<plugins_instructions>",
		"<turn_aborted>",
		"# AGENTS.md instructions for ",
		"The following is the Codex agent history whose request action",
	}
	for _, prefix := range technicalPrefixes {
		if strings.HasPrefix(message, prefix) {
			return ""
		}
	}
	return message
}

func pathWithin(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) && !filepath.IsAbs(relative)
}

func readSession(path string) (codexSession, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return codexSession{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return codexSession{}, errors.New("session must be a regular file")
	}
	if info.Size() > 512<<20 {
		return codexSession{}, errors.New("session is too large")
	}
	file, err := os.Open(path)
	if err != nil {
		return codexSession{}, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(io.LimitReader(file, 2<<20), 64<<10)
	line, err := reader.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return codexSession{}, err
	}
	if len(line) > 1<<20 {
		return codexSession{}, errors.New("session metadata line is too large")
	}
	var envelope sessionEnvelope
	if err = json.Unmarshal(line, &envelope); err != nil || envelope.Type != "session_meta" {
		return codexSession{}, errors.New("session metadata is invalid")
	}
	var metadata sessionMeta
	if err = json.Unmarshal(envelope.Payload, &metadata); err != nil {
		return codexSession{}, errors.New("session metadata is invalid")
	}
	id := metadata.ID
	if id == "" {
		id = metadata.SessionID
	}
	if !sessionIDPattern.MatchString(id) {
		return codexSession{}, errors.New("session id is invalid")
	}
	if metadata.CWD == "" || !filepath.IsAbs(metadata.CWD) {
		return codexSession{}, errors.New("session working directory is invalid")
	}
	created, parseErr := time.Parse(time.RFC3339Nano, metadata.Timestamp)
	if parseErr != nil {
		created = info.ModTime()
	}
	preview := ""
	// Initial instructions can precede the first user message. Scan a bounded
	// local prefix so the picker can show a useful title without loading a whole
	// conversation into memory.
	remaining := 1 << 20
	for preview == "" && remaining > 0 {
		line, lineErr := reader.ReadBytes('\n')
		remaining -= len(line)
		if len(line) > 0 {
			preview = previewFromEnvelope(line)
		}
		if lineErr != nil {
			break
		}
	}
	return codexSession{ID: strings.ToLower(id), CWD: filepath.Clean(metadata.CWD), Path: path, Preview: preview, Created: created, Updated: info.ModTime()}, nil
}

func sessionsInProject(home, project string) ([]codexSession, error) {
	sessionsRoot := filepath.Join(home, "sessions")
	root, err := filepath.Abs(project)
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(sessionsRoot); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	found := map[string]codexSession{}
	err = filepath.WalkDir(sessionsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("session path is a symlink: %s", path)
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		if !pathWithin(sessionsRoot, path) {
			return fmt.Errorf("session escaped its profile: %s", path)
		}
		session, readErr := readSession(path)
		if readErr != nil {
			return fmt.Errorf("read session %s: %w", path, readErr)
		}
		if !pathWithin(root, session.CWD) {
			return nil
		}
		if _, duplicate := found[session.ID]; duplicate {
			return fmt.Errorf("duplicate session id %s", session.ID)
		}
		found[session.ID] = session
		return nil
	})
	if err != nil {
		return nil, err
	}
	sessions := make([]codexSession, 0, len(found))
	for _, session := range found {
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Updated.After(sessions[j].Updated) })
	return sessions, nil
}

func fileIsPrefix(prefixPath, fullPath string) (bool, error) {
	prefix, err := os.Open(prefixPath)
	if err != nil {
		return false, err
	}
	defer prefix.Close()
	full, err := os.Open(fullPath)
	if err != nil {
		return false, err
	}
	defer full.Close()
	prefixInfo, err := prefix.Stat()
	if err != nil {
		return false, err
	}
	fullInfo, err := full.Stat()
	if err != nil {
		return false, err
	}
	if prefixInfo.Size() > fullInfo.Size() {
		return false, nil
	}
	left := make([]byte, 64<<10)
	right := make([]byte, len(left))
	remaining := prefixInfo.Size()
	for remaining > 0 {
		size := int64(len(left))
		if remaining < size {
			size = remaining
		}
		if _, err = io.ReadFull(prefix, left[:size]); err != nil {
			return false, err
		}
		if _, err = io.ReadFull(full, right[:size]); err != nil {
			return false, err
		}
		if !bytes.Equal(left[:size], right[:size]) {
			return false, nil
		}
		remaining -= size
	}
	return true, nil
}

func safeDestinationParent(root, parent string) error {
	if !pathWithin(root, parent) && filepath.Clean(root) != filepath.Clean(parent) {
		return errors.New("session destination escaped its profile")
	}
	current := filepath.Clean(root)
	relative, err := filepath.Rel(current, parent)
	if err != nil {
		return err
	}
	parts := []string{"."}
	if relative != "." {
		parts = append(parts, strings.Split(relative, string(os.PathSeparator))...)
	}
	for _, part := range parts {
		if part != "." {
			current = filepath.Join(current, part)
		}
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("session destination parent is not a safe directory: %s", current)
		}
	}
	return nil
}

func (a *App) syncExistingSession(target string, session codexSession, destination string, destinationInfo os.FileInfo) (bool, error) {
	existing, err := readSession(destination)
	if err != nil || existing.ID != session.ID {
		return false, errors.New("existing destination session is invalid")
	}
	destinationPrefix, err := fileIsPrefix(destination, session.Path)
	if err != nil {
		return false, errors.New("cannot compare existing destination session")
	}
	if !destinationPrefix {
		sourcePrefix, prefixErr := fileIsPrefix(session.Path, destination)
		if prefixErr != nil {
			return false, errors.New("cannot compare existing destination session")
		}
		if sourcePrefix {
			return false, nil
		}
		return false, fmt.Errorf("session %s has diverged between accounts", session.ID)
	}
	sourceInfo, err := os.Stat(session.Path)
	if err != nil {
		return false, err
	}
	if destinationInfo.Size() == sourceInfo.Size() {
		return false, nil
	}
	previous, err := os.ReadFile(destination)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(session.Path)
	if err != nil {
		return false, err
	}
	if err = atomicWrite(destination, data); err != nil {
		return false, err
	}
	if err = a.indexTransferredSession(target, session.ID); err != nil {
		if restoreErr := atomicWrite(destination, previous); restoreErr != nil {
			return false, fmt.Errorf("index updated session: %v; restore previous session: %w", err, restoreErr)
		}
		return false, fmt.Errorf("index updated session: %w", err)
	}
	return true, nil
}

func (a *App) copySession(source, target string, session codexSession) (string, bool, error) {
	sourceHome, err := a.require(source)
	if err != nil {
		return "", false, err
	}
	targetHome, err := a.require(target)
	if err != nil {
		return "", false, err
	}
	sourceRoot := filepath.Join(sourceHome, "sessions")
	if !pathWithin(sourceRoot, session.Path) {
		return "", false, errors.New("session path is outside the source profile")
	}
	verified, err := readSession(session.Path)
	if err != nil || verified.ID != session.ID {
		return "", false, errors.New("session changed or became invalid before transfer")
	}
	relative, err := filepath.Rel(sourceRoot, session.Path)
	if err != nil || filepath.IsAbs(relative) || strings.HasPrefix(relative, "..") {
		return "", false, errors.New("session destination is invalid")
	}
	destination := filepath.Join(targetHome, "sessions", relative)
	if err = safeDestinationParent(targetHome, filepath.Dir(destination)); err != nil {
		return "", false, err
	}
	if info, statErr := os.Lstat(destination); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", false, errors.New("existing destination session is not a regular file")
		}
		updated, syncErr := a.syncExistingSession(target, session, destination, info)
		if syncErr != nil {
			return "", false, syncErr
		}
		return destination, updated, nil
	} else if !os.IsNotExist(statErr) {
		return "", false, statErr
	}
	data, err := os.ReadFile(session.Path)
	if err != nil {
		return "", false, err
	}
	if err = atomicWrite(destination, data); err != nil {
		return "", false, err
	}
	if err = a.indexTransferredSession(target, session.ID); err != nil {
		_ = os.Remove(destination)
		return "", false, fmt.Errorf("index transferred session: %w", err)
	}
	return destination, true, nil
}

func (a *App) indexTransferredSession(account, id string) error {
	if a.SessionIndexer != nil {
		return a.SessionIndexer(account, id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cli, args, env, err := a.command(account, []string{"migrate-rollouts", "--apply", "--thread", id, "--json"}, false)
	if err != nil {
		return err
	}
	cmd := processCommand(cli, args...)
	setProcessEnvironment(cmd, env)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		return err
	case <-ctx.Done():
		_ = terminateProcess(cmd, true)
		<-done
		return ctx.Err()
	}
}
