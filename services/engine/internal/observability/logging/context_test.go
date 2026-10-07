package logging

import (
	"context"
	"reflect"
	"testing"
)

func TestAttrsEmptyContext(t *testing.T) {
	if got := Attrs(nil); got != nil {
		t.Fatalf("Attrs(nil) = %v, want nil", got)
	}
	if got := Attrs(context.Background()); len(got) != 0 {
		t.Fatalf("Attrs(Background) = %v, want empty", got)
	}
}

func TestAttrsExtractsPresentFieldsInOrder(t *testing.T) {
	ctx := context.Background()
	ctx = WithDeploymentID(ctx, "dep_abc")
	ctx = WithRequestID(ctx, "req_123")
	ctx = WithCorrelationID(ctx, "req_123")

	want := []any{
		FieldRequestID, "req_123",
		FieldCorrelationID, "req_123",
		FieldDeploymentID, "dep_abc",
	}
	if got := Attrs(ctx); !reflect.DeepEqual(got, want) {
		t.Fatalf("Attrs = %v, want %v", got, want)
	}
}

func TestAttrsOmitsEmptyIdentifiers(t *testing.T) {
	ctx := WithRequestID(context.Background(), "")
	ctx = WithCorrelationID(ctx, "req_corr")
	if got := Attrs(ctx); !reflect.DeepEqual(got, []any{FieldCorrelationID, "req_corr"}) {
		t.Fatalf("Attrs = %v, want only correlationId", got)
	}
}

func TestExtractHelpers(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req_a")
	ctx = WithCorrelationID(ctx, "req_b")
	ctx = WithDeploymentID(ctx, "dep_c")

	if got := RequestID(ctx); got != "req_a" {
		t.Errorf("RequestID = %q, want req_a", got)
	}
	if got := CorrelationID(ctx); got != "req_b" {
		t.Errorf("CorrelationID = %q, want req_b", got)
	}
	if got := DeploymentID(ctx); got != "dep_c" {
		t.Errorf("DeploymentID = %q, want dep_c", got)
	}
}

func TestExtractNilAndMissingContext(t *testing.T) {
	if RequestID(nil) != "" || CorrelationID(nil) != "" || DeploymentID(nil) != "" {
		t.Fatal("extract helpers must return empty for nil context")
	}
	ctx := context.Background()
	if RequestID(ctx) != "" || CorrelationID(ctx) != "" || DeploymentID(ctx) != "" {
		t.Fatal("extract helpers must return empty when keys are absent")
	}
}
