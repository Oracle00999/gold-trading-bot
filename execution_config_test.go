package main

import "testing"

func TestExecutionPinFailsClosed(t *testing.T) {
	t.Setenv("DEMO_LOGIN", "12345678")
	t.Setenv("DEMO_SERVER", "Example-Demo")
	pin, err := loadExecutionPin()
	if err != nil {
		t.Fatal(err)
	}
	s := ExecutionSnapshot{Demo: true, Account: pin.Account, Server: pin.Server, BrokerTime: 1}
	if err := s.validate(pin); err != nil {
		t.Fatal(err)
	}
	s.Account = "87654321"
	if s.validate(pin) == nil {
		t.Fatal("accepted a different account")
	}
	s.Account = pin.Account
	s.Server = "Different-Demo"
	if s.validate(pin) == nil {
		t.Fatal("accepted a different server")
	}
	t.Setenv("DEMO_SERVER", "")
	if _, err := loadExecutionPin(); err == nil {
		t.Fatal("partial environment fell back to local account")
	}
	if s.validate(executionPin{}) == nil {
		t.Fatal("accepted unconfigured account")
	}
}
