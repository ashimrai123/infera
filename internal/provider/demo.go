package provider

import (
	"context"
	"fmt"

	"github.com/ashimrai123/infera/internal/models"
)

// FailingProvider is a stub that always returns an error.
// It is registered as the primary for the "demo-" model prefix so that
// every request deliberately fails over to the next provider in the list.
// This makes the failover path visible and testable in the live demo.
type FailingProvider struct{}

func NewFailingProvider() *FailingProvider { return &FailingProvider{} }

func (f *FailingProvider) Name() string { return "demo-failing" }

func (f *FailingProvider) Complete(_ context.Context, _ *models.ChatRequest) (*models.ChatResponse, error) {
	return nil, fmt.Errorf("demo-failing: intentional failure to demonstrate failover")
}

func (f *FailingProvider) Stream(_ context.Context, _ *models.ChatRequest) (<-chan models.ChatChunk, error) {
	return nil, fmt.Errorf("demo-failing: intentional failure to demonstrate failover")
}

func (f *FailingProvider) HealthCheck(_ context.Context) error {
	return fmt.Errorf("demo-failing: always unhealthy by design")
}
