package planner

import "fmt"

func validateProfile(p ApplicationProfile) error {
	if !p.Deployable() {
		return fmt.Errorf("application profile is %s; resolve it before planning", p.Status)
	}
	if p.Port.Value <= 0 || p.Port.Value > 65535 {
		return fmt.Errorf("application profile port must be between 1 and 65535")
	}
	return nil
}

func validateServer(s ServerProfile) error {
	if s.ID == "" {
		return fmt.Errorf("server id is required")
	}
	if s.Status != "READY" {
		return fmt.Errorf("server is not READY")
	}
	return nil
}
