package pluginsdk

import (
	"context"
	"testing"

	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"
)

type fakeNotifier struct{}

func (f *fakeNotifier) Send(ctx context.Context, msg *gen.NotificationMessage) error { return nil }

func TestCurrentNotifierRejectsTypedNil(t *testing.T) {
	notifierMu.Lock()
	notifierImpl = nil
	notifierMu.Unlock()

	if _, ok := currentNotifier(); ok {
		t.Fatal("expected no notifier before ProvideNotifier")
	}

	var fn *fakeNotifier
	notifierMu.Lock()
	notifierImpl = fn
	notifierMu.Unlock()
	if _, ok := currentNotifier(); ok {
		t.Fatal("typed nil must not count as a provided notifier")
	}

	n := &fakeNotifier{}
	notifierMu.Lock()
	notifierImpl = n
	notifierMu.Unlock()
	if got, ok := currentNotifier(); !ok || got != n {
		t.Fatalf("expected the provided notifier, got %v (ok=%v)", got, ok)
	}
}
