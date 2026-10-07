package evidence

import (
	"math"
	"sort"
)

// Unsupported frameworks for V0.1 (detected, never deployable by preset).
var unsupportedFrameworks = map[string]string{
	"Django": "Python frameworks are not supported in V0.1", "Flask": "Python frameworks are not supported in V0.1",
	"FastAPI": "Python frameworks are not supported in V0.1", "Rails": "Ruby frameworks are not supported in V0.1",
	"Laravel": "PHP frameworks are not supported in V0.1", "Spring Boot": "Java frameworks are not supported in V0.1",
}

// decide combines votes for a single-valued kind into one finding.
//
// Per candidate, confidence = 1 - Π(1 - w) over supporting evidence (noisy-OR,
// bounded in [0,1]). An explicit declaration (w = 1) wins over competing
// heuristics; otherwise candidates closer than 0.3 are ambiguous.
func decide(kind string, votes []vote, notApplicable string) Finding {
	if len(votes) == 0 {
		if notApplicable != "" {
			return Finding{Kind: kind, State: StateNotApplicable, Reason: notApplicable, Evidence: []Evidence{}}
		}
		return Finding{Kind: kind, State: StateNotDetected, Reason: "no evidence found", Evidence: []Evidence{}}
	}
	type cand struct {
		value    string
		conf     float64
		decisive bool
		ev       []Evidence
	}
	byValue := map[string]*cand{}
	for _, v := range votes {
		c := byValue[v.value]
		if c == nil {
			c = &cand{value: v.value, conf: 0}
			byValue[v.value] = c
		}
		c.conf = 1 - (1-c.conf)*(1-v.weight)
		c.decisive = c.decisive || v.weight >= 1
		c.ev = append(c.ev, v.evidence)
	}
	cands := make([]*cand, 0, len(byValue))
	for _, c := range byValue {
		cands = append(cands, c)
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].decisive != cands[j].decisive {
			return cands[i].decisive
		}
		if cands[i].conf != cands[j].conf {
			return cands[i].conf > cands[j].conf
		}
		return cands[i].value < cands[j].value
	})
	top := cands[0]
	f := Finding{Kind: kind, Value: top.value, Confidence: round(top.conf), Evidence: sortEvidence(top.ev), State: StateDetected}

	if len(cands) > 1 {
		second := cands[1]
		decisiveWin := top.decisive && !second.decisive
		if !decisiveWin && top.conf-second.conf < 0.3 {
			f.State = StateAmbiguous
			f.Value = ""
			f.Confidence = round(top.conf * (1 - second.conf/2))
			f.Evidence = nil
			for _, c := range cands {
				f.Candidates = append(f.Candidates, c.value)
				f.Evidence = append(f.Evidence, c.ev...)
			}
			f.Evidence = sortEvidence(f.Evidence)
			f.Reason = "conflicting evidence supports several values"
			return f
		}
		// Winner with recorded conflicts: confidence reduced by the strongest rival.
		f.Confidence = round(top.conf * (1 - second.conf*0.25))
		for _, c := range cands[1:] {
			f.Candidates = append(f.Candidates, c.value)
			for _, e := range c.ev {
				e.Effect = "conflicts"
				f.Evidence = append(f.Evidence, e)
			}
		}
		f.Evidence = sortEvidence(f.Evidence)
	}
	if kind == KindFramework {
		if reason, bad := unsupportedFrameworks[f.Value]; bad {
			f.State, f.Reason = StateUnsupported, reason
		}
	}
	return f
}

func multiFinding(kind string, m map[string][]Evidence) Finding {
	f := Finding{Kind: kind, State: StateDetected, Confidence: 1}
	for v, ev := range m {
		f.Values = append(f.Values, v)
		f.Evidence = append(f.Evidence, ev...)
	}
	sort.Strings(f.Values)
	f.Evidence = sortEvidence(f.Evidence)
	return f
}

func normalizeFinding(f Finding) Finding {
	if f.Evidence == nil {
		f.Evidence = []Evidence{}
	}
	f.Evidence = sortEvidence(f.Evidence)
	f.Confidence = round(f.Confidence)
	return f
}

func sortEvidence(ev []Evidence) []Evidence {
	sort.SliceStable(ev, func(i, j int) bool {
		if ev[i].Path != ev[j].Path {
			return ev[i].Path < ev[j].Path
		}
		if ev[i].Rule != ev[j].Rule {
			return ev[i].Rule < ev[j].Rule
		}
		return ev[i].Value < ev[j].Value
	})
	if ev == nil {
		return []Evidence{}
	}
	return ev
}

func round(f float64) float64 { return math.Round(math.Max(0, math.Min(1, f))*1000) / 1000 }
