package pluginsdk

import (
	"context"
	"testing"
)

func TestOnConfigDelivery(t *testing.T) {
	// Reset delivery state between tests.
	cfgMu.Lock()
	cfgRaw = ""
	cfgDelivered = false
	cfgReceivers = nil
	cfgMu.Unlock()

	type cfg struct {
		Greeting string `json:"greeting"`
	}

	// Registered before delivery: queued, invoked by runConfigReceivers.
	var preSeen []string
	if err := OnConfig(func(ctx context.Context, c cfg) error {
		preSeen = append(preSeen, c.Greeting)
		return nil
	}); err != nil {
		t.Fatalf("OnConfig before delivery: %v", err)
	}
	if len(preSeen) != 0 {
		t.Fatalf("receiver must be queued before delivery, called %d times", len(preSeen))
	}

	setConfigCache(`{"greeting":"Hi"}`)
	markConfigDelivered()
	if err := runConfigReceivers(context.Background()); err != nil {
		t.Fatalf("runConfigReceivers: %v", err)
	}
	if len(preSeen) != 1 || preSeen[0] != "Hi" {
		t.Fatalf("expected first delivery of Hi, got %v", preSeen)
	}

	// Registered after delivery: invoked immediately with the cached config.
	postCalls := 0
	if err := OnConfig(func(ctx context.Context, c cfg) error {
		postCalls++
		return nil
	}); err != nil {
		t.Fatalf("OnConfig after delivery: %v", err)
	}
	if postCalls != 1 {
		t.Fatalf("expected immediate delivery, got %d calls", postCalls)
	}

	// A new delivery reaches every receiver.
	setConfigCache(`{"greeting":"New"}`)
	if err := runConfigReceivers(context.Background()); err != nil {
		t.Fatalf("second runConfigReceivers: %v", err)
	}
	if len(preSeen) != 2 || preSeen[1] != "New" {
		t.Fatalf("expected second delivery of New, got %v", preSeen)
	}
	if postCalls != 2 {
		t.Fatalf("expected post receiver to get the new delivery too, got %d calls", postCalls)
	}
}
