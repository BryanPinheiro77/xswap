package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Account struct {
	Type  string `json:"type"`
	Email string `json:"email"`
	Plan  string `json:"planType"`
}
type Window struct {
	Used    *float64 `json:"usedPercent"`
	Minutes *int     `json:"windowDurationMins"`
	Resets  *int64   `json:"resetsAt"`
}
type Bucket struct {
	ID        string  `json:"limitId"`
	Name      string  `json:"limitName"`
	Primary   *Window `json:"primary"`
	Secondary *Window `json:"secondary"`
}
type Limits struct {
	Main    *Bucket           `json:"rateLimits"`
	Buckets map[string]Bucket `json:"rateLimitsByLimitId"`
}
type Record struct {
	Account Account `json:"account"`
	Limits  Limits  `json:"limits"`
	Updated int64   `json:"updated"`
	Error   string  `json:"error,omitempty"`
}

func (a *App) original() (string, error) {
	var metadata struct {
		CLI string `json:"cli"`
	}
	data, err := os.ReadFile(filepath.Join(a.Root, "installation.json"))
	if err != nil {
		return "", errors.New("installation metadata missing; run: codex-swap install")
	}
	if err = json.Unmarshal(data, &metadata); err != nil {
		return "", err
	}
	info, err := os.Stat(metadata.CLI)
	if err != nil || !isRunnable(info) {
		return "", errors.New("original Codex CLI unavailable; reinstall Codex and run codex-swap install")
	}
	return metadata.CLI, nil
}
func envWith(env []string, key, value string) []string {
	next := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, key+"=") {
			next = append(next, entry)
		}
	}
	return append(next, key+"="+value)
}
func (a *App) command(name string, args []string, honorEnv bool) (string, []string, []string, error) {
	cli, err := a.original()
	if err != nil {
		return "", nil, nil, err
	}
	env := os.Environ()
	if honorEnv && os.Getenv("CODEX_HOME") != "" {
		return cli, args, env, nil
	}
	home, err := a.require(name)
	if err != nil {
		return "", nil, nil, err
	}
	env = envWith(env, "CODEX_HOME", home)
	if name != "default" {
		args = append([]string{"-c", `cli_auth_credentials_store="file"`}, args...)
	}
	return cli, args, env, nil
}
func (a *App) launch(name string, args []string, honorEnv bool) error {
	cli, args, env, err := a.command(name, args, honorEnv)
	if err != nil {
		return err
	}
	return replaceProcess(cli, args, env)
}
func (a *App) login(name string, device bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	unlock, err := a.lock(ctx, filepath.Join("locks", name+".lock"))
	if err != nil {
		return err
	}
	defer unlock()
	args := []string{"login"}
	if device {
		args = append(args, "--device-auth")
	}
	cli, args, env, err := a.command(name, args, false)
	if err != nil {
		return err
	}
	cmd := exec.Command(cli, args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return err
	}
	home, _ := a.profile(name)
	if name != "default" {
		os.Chmod(filepath.Join(home, "auth.json"), 0600)
	}
	fmt.Printf("Login complete. Select this account with: xswap switch %s\n", name)
	return nil
}

type rpcReply struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func (a *App) readLimits(parent context.Context, name string) (Record, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	unlock, err := a.lock(ctx, filepath.Join("locks", name+".lock"))
	if err != nil {
		return Record{}, err
	}
	defer unlock()
	cli, args, env, err := a.command(name, []string{"app-server", "--stdio"}, false)
	if err != nil {
		return Record{}, err
	}
	cmd := exec.Command(cli, args...)
	cmd.Env = env
	configureProcess(cmd)
	input, err := cmd.StdinPipe()
	if err != nil {
		return Record{}, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return Record{}, err
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return Record{}, err
	}
	replies := make(chan rpcReply, 16)
	ended := make(chan struct{})
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 65536), 2*1024*1024)
		for scanner.Scan() {
			var reply rpcReply
			if json.Unmarshal(scanner.Bytes(), &reply) == nil {
				select {
				case replies <- reply:
				case <-ctx.Done():
					return
				}
			}
		}
		close(ended)
	}()
	defer func() {
		cancel()
		input.Close()
		terminateProcess(cmd, false)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			terminateProcess(cmd, true)
			<-done
		}
		// The npm launcher can exit before its native child; stop any survivors.
		terminateProcess(cmd, true)
		output.Close()
		<-scanDone
	}()
	send := func(value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = input.Write(append(data, '\n'))
		return err
	}
	request := func(id int, method string, params any) (json.RawMessage, error) {
		message := map[string]any{"id": id, "method": method}
		if params != nil {
			message["params"] = params
		}
		if err := send(message); err != nil {
			return nil, errors.New("Codex ended the query; check your login with xswap status")
		}
		for {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("quota query cancelled or timed out: %w", ctx.Err())
			case <-ended:
				return nil, errors.New("Codex ended the query; check your login with xswap status")
			case reply := <-replies:
				if reply.ID == id && reply.Method == "" {
					if len(reply.Error) > 0 && string(reply.Error) != "null" {
						return nil, errors.New("Codex could not query this account; check login with xswap status")
					}
					return reply.Result, nil
				}
			}
		}
	}
	if _, err = request(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "codex_swap", "title": "XSwap", "version": "1.0.0"}}); err != nil {
		return Record{}, err
	}
	if err = send(map[string]string{"method": "initialized"}); err != nil {
		return Record{}, err
	}
	data, err := request(2, "account/read", map[string]bool{"refreshToken": false})
	if err != nil {
		return Record{}, err
	}
	var accountResponse struct {
		Account *Account `json:"account"`
	}
	if err = json.Unmarshal(data, &accountResponse); err != nil {
		return Record{}, errors.New("invalid account response")
	}
	if accountResponse.Account == nil {
		return Record{}, fmt.Errorf("login pending; run: xswap login %s", name)
	}
	if accountResponse.Account.Type != "chatgpt" {
		return Record{}, errors.New("subscription quotas require a ChatGPT account; API keys are not supported")
	}
	data, err = request(3, "account/rateLimits/read", nil)
	if err != nil {
		return Record{}, err
	}
	var limits Limits
	if err = json.Unmarshal(data, &limits); err != nil {
		return Record{}, errors.New("invalid quota response")
	}
	return Record{Account: *accountResponse.Account, Limits: limits, Updated: time.Now().Unix()}, nil
}
func allBuckets(limits Limits) map[string]Bucket {
	buckets := map[string]Bucket{}
	for key, value := range limits.Buckets {
		buckets[key] = value
	}
	if limits.Main != nil {
		key := limits.Main.ID
		if key == "" {
			key = "codex"
		}
		if _, ok := buckets[key]; !ok {
			buckets[key] = *limits.Main
		}
	}
	return buckets
}
func (a *App) showLimits(ctx context.Context, names []string) error {
	failed := false
	for _, name := range names {
		record, err := a.readLimits(ctx, name)
		if err != nil {
			fmt.Printf("%s: %v\n", name, err)
			failed = true
			continue
		}
		fmt.Printf("%s · %s · %s\n", name, clean(record.Account.Email), clean(record.Account.Plan))
		windows := quotaWindows(record.Limits)
		if len(windows) == 0 {
			fmt.Println("  No quota windows returned by the server.")
		}
		for _, item := range windows {
			used := "unavailable"
			if item.Window.Used != nil {
				used = fmt.Sprintf("%g%% used · %g%% remaining", *item.Window.Used, max(0, 100-*item.Window.Used))
			}
			fmt.Printf("  %s: %s · %s\n", clean(item.Label), used, resetLabel(item.Window.Resets, time.Now()))
		}
	}
	if failed {
		return errors.New("some accounts could not be queried")
	}
	return nil
}
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || (r >= 128 && r < 160) {
			return -1
		}
		return r
	}, s)
}
func resetLabel(timestamp *int64, now time.Time) string {
	if timestamp == nil {
		return "reset unavailable"
	}
	seconds := *timestamp - now.Unix()
	if seconds <= 0 {
		return "resets now"
	}
	days := seconds / 86400
	hours := seconds % 86400 / 3600
	minutes := seconds % 3600 / 60
	duration := fmt.Sprintf("%dm", max(1, minutes))
	if days > 0 {
		duration = fmt.Sprintf("%dd %dh", days, hours)
	} else if hours > 0 {
		duration = fmt.Sprintf("%dh %dm", hours, minutes)
	}
	layout := "15:04"
	if days > 0 {
		layout = "Jan 02 15:04"
	}
	return "resets " + duration + " · " + time.Unix(*timestamp, 0).Format(layout)
}

type QuotaItem struct {
	Label  string
	Window Window
}

func quotaWindows(limits Limits) []QuotaItem {
	buckets := allBuckets(limits)
	keys := []string{}
	if _, ok := buckets["codex"]; ok {
		keys = append(keys, "codex")
	}
	for key := range buckets {
		if key != "codex" {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i] == "codex" {
			return keys[j] != "codex"
		}
		if keys[j] == "codex" {
			return false
		}
		return keys[i] < keys[j]
	})
	items := []QuotaItem{}
	for _, key := range keys {
		bucket := buckets[key]
		for _, window := range []*Window{bucket.Primary, bucket.Secondary} {
			if window == nil {
				continue
			}
			label := "usage"
			if window.Minutes != nil {
				minutes := *window.Minutes
				if minutes == 10080 {
					label = "7d"
				} else if minutes%60 == 0 {
					label = strconv.Itoa(minutes/60) + "h"
				} else {
					label = strconv.Itoa(minutes) + "m"
				}
			}
			if key != "codex" {
				label = bucket.Name
				if label == "" {
					label = key
				}
			}
			items = append(items, QuotaItem{label, *window})
		}
	}
	return items
}
