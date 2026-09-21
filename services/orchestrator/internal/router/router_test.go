package router

import "testing"

func TestMatch(t *testing.T) {
	agent, err := Match(Task{Role:"architect", Capabilities:[]string{"architecture"}}, []Agent{{ID:"a1", Role:"architect", Capabilities:map[string]bool{"architecture":true}, Permissions:map[string]bool{}}})
	if err != nil || agent.ID != "a1" { t.Fatalf("unexpected routing result: %+v, %v", agent, err) }
}

func TestRejectsMissingCapability(t *testing.T) {
	_, err := Match(Task{Role:"architect", Capabilities:[]string{"architecture"}}, []Agent{{ID:"a1", Role:"architect", Capabilities:map[string]bool{}, Permissions:map[string]bool{}}})
	if err == nil { t.Fatal("expected routing rejection") }
}