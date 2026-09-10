package usage_test

import (
	"sync"
	"testing"

	"github.com/ashimrai123/infera/internal/usage"
)

func TestTrackerCounters(t *testing.T) {
	tr := usage.New()

	tr.RecordRequest("openai")
	tr.RecordRequest("openai")
	tr.RecordRequest("anthropic")
	tr.RecordTokens("openai", 150)
	tr.RecordTokens("openai", 50)
	tr.RecordTokens("anthropic", 200)
	tr.RecordError("openai")

	snap := tr.Snapshot()

	if snap["openai"].Requests != 2 {
		t.Errorf("openai requests: want 2, got %d", snap["openai"].Requests)
	}
	if snap["openai"].Tokens != 200 {
		t.Errorf("openai tokens: want 200, got %d", snap["openai"].Tokens)
	}
	if snap["openai"].Errors != 1 {
		t.Errorf("openai errors: want 1, got %d", snap["openai"].Errors)
	}
	if snap["anthropic"].Requests != 1 {
		t.Errorf("anthropic requests: want 1, got %d", snap["anthropic"].Requests)
	}
	if snap["anthropic"].Tokens != 200 {
		t.Errorf("anthropic tokens: want 200, got %d", snap["anthropic"].Tokens)
	}
	if snap["anthropic"].Errors != 0 {
		t.Errorf("anthropic errors: want 0, got %d", snap["anthropic"].Errors)
	}
}

// TestTrackerConcurrent verifies there are no races under concurrent access.
func TestTrackerConcurrent(t *testing.T) {
	tr := usage.New()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.RecordRequest("openai")
			tr.RecordTokens("openai", 10)
		}()
	}
	wg.Wait()

	snap := tr.Snapshot()
	if snap["openai"].Requests != 100 {
		t.Errorf("concurrent requests: want 100, got %d", snap["openai"].Requests)
	}
	if snap["openai"].Tokens != 1000 {
		t.Errorf("concurrent tokens: want 1000, got %d", snap["openai"].Tokens)
	}
}

func TestTrackerNegativeTokensIgnored(t *testing.T) {
	tr := usage.New()
	tr.RecordTokens("openai", -5) // should be a no-op
	snap := tr.Snapshot()
	if snap["openai"].Tokens != 0 {
		t.Errorf("negative tokens should be ignored, got %d", snap["openai"].Tokens)
	}
}
