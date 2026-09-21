package policy

type Decision string

const (
	Allow Decision = "ALLOW"
	Deny Decision = "DENY"
	ApprovalRequired Decision = "APPROVAL_REQUIRED"
)

type Request struct {
	Principal string
	ProjectID string
	Capability string
	Permission string
}

type Policy interface { Authorize(Request) Decision }

type StaticPolicy struct { Rules map[string]Decision }

func (p StaticPolicy) Authorize(r Request) Decision {
	if d, ok := p.Rules[r.Capability+":"+r.Permission]; ok { return d }
	return Deny
}