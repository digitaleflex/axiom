package server

import "testing"

func TestCheckEligibility(t *testing.T) {
	service := NewService(nil)
	record := Record{
		ID: "srv_1", Status: StatusReady,
		CPUCount: 2, MemoryMB: 8192, DiskFreeMB: 50000,
		Capabilities: []Capability{CapabilityDocker, CapabilityTraefik, CapabilityTLS},
	}
	result := service.CheckEligibility(record, EligibilityRequest{
		RequiredCapabilities: []Capability{CapabilityDocker, CapabilityTraefik},
	})
	if !result.Eligible {
		t.Fatalf("expected eligible server, reasons: %v", result.Reasons)
	}
}

func TestCheckEligibilityRejectsUnavailableServer(t *testing.T) {
	service := NewService(nil)
	result := service.CheckEligibility(Record{
		ID: "srv_1", Status: StatusDegraded,
		CPUCount: 2, MemoryMB: 8192, DiskFreeMB: 50000,
	}, EligibilityRequest{})
	if result.Eligible {
		t.Fatal("expected degraded server to be ineligible")
	}
}
