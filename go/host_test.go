package pluginsdk

import "testing"

func TestConfigCache(t *testing.T) {
	setConfigCache("")
	if _, err := CurrentConfig[struct{}](); err != ErrNoConfig {
		t.Fatalf("expected ErrNoConfig for empty cache, got %v", err)
	}

	setConfigCache(`{"greeting":"Hi"}`)
	type cfg struct {
		Greeting string `json:"greeting"`
	}
	c, err := CurrentConfig[cfg]()
	if err != nil {
		t.Fatalf("CurrentConfig: %v", err)
	}
	if c.Greeting != "Hi" {
		t.Fatalf("unexpected config: %+v", c)
	}

	setConfigCache(`{"greeting":1}`)
	if _, err := CurrentConfig[cfg](); err == nil {
		t.Fatal("expected unmarshal error for mismatched config type")
	}
}
