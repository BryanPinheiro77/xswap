package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "app-server":
			serveAppServer()
			return
		case "login":
			home := os.Getenv("CODEX_HOME")
			if os.MkdirAll(home, 0700) != nil || os.WriteFile(filepath.Join(home, "auth.json"), []byte("{}"), 0600) != nil {
				os.Exit(1)
			}
		}
	}
	payload := struct {
		Args      []string `json:"args"`
		CodexHome string   `json:"codexHome"`
	}{Args: os.Args[1:], CodexHome: os.Getenv("CODEX_HOME")}
	data, err := json.Marshal(payload)
	if err != nil {
		os.Exit(1)
	}
	capture := os.Getenv("XSWAP_TEST_CAPTURE")
	if capture != "" {
		err = os.WriteFile(capture, data, 0600)
	}
	if err != nil {
		os.Exit(1)
	}
}

func serveAppServer() {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.Method == "initialized" {
			continue
		}
		result := any(map[string]any{})
		var responseError any
		switch request.Method {
		case "account/read":
			result = map[string]any{"account": map[string]any{"type": "chatgpt", "email": "windows@example.com", "planType": "plus"}}
		case "account/rateLimits/read":
			used := 12.0
			result = map[string]any{"rateLimits": map[string]any{"limitId": "codex", "primary": map[string]any{"usedPercent": used, "windowDurationMins": 300, "resetsAt": time.Now().Add(time.Hour).Unix()}}}
		case "thread/resume":
			var params struct {
				ThreadID     string `json:"threadId"`
				ExcludeTurns bool   `json:"excludeTurns"`
			}
			if json.Unmarshal(request.Params, &params) != nil || params.ThreadID == "" || !params.ExcludeTurns {
				responseError = map[string]any{"message": "invalid thread resume request"}
			} else {
				result = map[string]any{"thread": map[string]any{"id": params.ThreadID}}
			}
		}
		response := map[string]any{"id": request.ID, "result": result}
		if responseError != nil {
			response["error"] = responseError
		}
		_ = encoder.Encode(response)
	}
}
