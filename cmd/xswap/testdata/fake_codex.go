package main

import (
	"encoding/json"
	"os"
)

func main() {
	payload := struct {
		Args      []string `json:"args"`
		CodexHome string   `json:"codexHome"`
	}{Args: os.Args[1:], CodexHome: os.Getenv("CODEX_HOME")}
	data, err := json.Marshal(payload)
	if err != nil {
		os.Exit(1)
	}
	if err = os.WriteFile(os.Getenv("XSWAP_TEST_CAPTURE"), data, 0600); err != nil {
		os.Exit(1)
	}
}
