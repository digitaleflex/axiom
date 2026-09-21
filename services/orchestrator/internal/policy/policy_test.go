package policy

import "testing"

func TestDenyByDefault(t *testing.T) {
	p := StaticPolicy{Rules: map[string]Decision{}}
	if got := p.Authorize(Request{Principal:"agent", Capability:"deploy", Permission:"production"}); got != Deny { t.Fatalf("expected DENY, got %s", got) }
}

func TestExplicitApproval(t *testing.T) {
	p := StaticPolicy{Rules: map[string]Decision{"deploy:production":ApprovalRequired}}
	if got := p.Authorize(Request{Capability:"deploy", Permission:"production"}); got != ApprovalRequired { t.Fatalf("expected approval, got %s", got) }
}