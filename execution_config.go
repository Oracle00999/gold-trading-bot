package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type executionPin struct {
	Account string `json:"account"`
	Server  string `json:"server"`
}

// Environment configuration must supply both values; never mix account sources.
func loadExecutionPin() (executionPin, error) {
	account, hasAccount := os.LookupEnv("DEMO_LOGIN")
	server, hasServer := os.LookupEnv("DEMO_SERVER")
	if hasAccount || hasServer {
		return parseExecutionPin(account, server)
	}
	b, err := os.ReadFile(".local/execution-account.json")
	if err != nil {
		return executionPin{}, fmt.Errorf("set DEMO_LOGIN and DEMO_SERVER or create .local/execution-account.json")
	}
	var pin executionPin
	if err := json.Unmarshal(b, &pin); err != nil {
		return executionPin{}, fmt.Errorf("invalid execution account configuration")
	}
	return parseExecutionPin(pin.Account, pin.Server)
}

func parseExecutionPin(account, server string) (executionPin, error) {
	account, server = strings.TrimSpace(account), strings.TrimSpace(server)
	n, err := strconv.ParseInt(account, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != account || server == "" || len(server) > 128 {
		return executionPin{}, fmt.Errorf("DEMO_LOGIN must be a positive account number and DEMO_SERVER must be set")
	}
	return executionPin{Account: account, Server: server}, nil
}
