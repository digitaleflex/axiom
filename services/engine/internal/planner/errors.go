package planner

import "fmt"

func validateProfile(p ApplicationProfile) error {
	if p.Language == "" {
		return fmt.Errorf("application profile language is required")
	}
	if p.BuildCommand == "" {
		return fmt.Errorf("application profile build command is required")
	}
	if p.StartCommand == "" {
		return fmt.Errorf("application profile start command is required")
	}
	if p.Port <= 0 || p.Port > 65535 {
		return fmt.Errorf("application profile port must be between 1 and 65535")
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return fmt.Errorf("application profile confidence must be between 0 and 1")
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
