package pluginsdk

import (
	"context"
	"testing"

	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"
)

func TestUIBuilders(t *testing.T) {
	uiMu.Lock()
	formFunc = nil
	pageFunc = nil
	uiMu.Unlock()

	server := newUIServer()

	// No builders registered: empty schemas.
	formResp, err := server.GetForm(context.Background(), &gen.GetFormRequest{})
	if err != nil {
		t.Fatalf("GetForm without builder: %v", err)
	}
	if formResp.SchemaJson != "" {
		t.Fatalf("expected empty form schema, got %q", formResp.SchemaJson)
	}

	// The plugin writes the tree with plain Vuetify names (see the Vuetify
	// documentation); the SDK imposes no vocabulary.
	if err := OnForm(func(ctx context.Context) Component {
		return Component{
			Component: "VForm",
			Content: []Component{
				{
					Component: "VRow",
					Content: []Component{
						{
							Component: "VCol",
							Props:     map[string]any{"cols": 8},
							Content: []Component{
								{
									Component: "VTextField",
									Props:     map[string]any{"model": "greeting", "label": "问候语"},
								},
								{
									Component: "VSelect",
									Props: map[string]any{
										"model": "level",
										"label": "级别",
										"items": []map[string]string{{"title": "Info", "value": "info"}},
									},
								},
							},
						},
					},
				},
			},
		}
	}); err != nil {
		t.Fatalf("OnForm: %v", err)
	}
	if err := OnPage(func(ctx context.Context) Component {
		return Component{
			Component: "VCard",
			Content:   []Component{{Component: "VCardTitle", Props: map[string]any{"text": "状态"}}},
		}
	}); err != nil {
		t.Fatalf("OnPage: %v", err)
	}

	formResp, err = server.GetForm(context.Background(), &gen.GetFormRequest{})
	if err != nil {
		t.Fatalf("GetForm: %v", err)
	}
	if formResp.SchemaJson == "" {
		t.Fatal("expected form schema")
	}
	pageResp, err := server.GetPage(context.Background(), &gen.GetPageRequest{})
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if pageResp.SchemaJson == "" {
		t.Fatal("expected page schema")
	}
}

func TestFieldNodeBindsModel(t *testing.T) {
	n := FieldNode("VSwitch", "verbose", "详细日志", nil)
	if n.Component != "VSwitch" {
		t.Fatalf("unexpected component %q", n.Component)
	}
	if n.Props["model"] != "verbose" || n.Props["label"] != "详细日志" {
		t.Fatalf("unexpected props: %v", n.Props)
	}
}

func TestFieldNodeDoesNotShareCallerMap(t *testing.T) {
	shared := map[string]any{"hint": "a"}
	a := FieldNode("VTextField", "a", "A", shared)
	b := FieldNode("VTextField", "b", "B", shared)

	// The caller's map must be untouched.
	if len(shared) != 1 || shared["hint"] != "a" {
		t.Fatalf("FieldNode mutated the caller's map: %v", shared)
	}
	// Two fields built from the same map must not overwrite each other.
	if a.Props["model"] != "a" || b.Props["model"] != "b" {
		t.Fatalf("model leaked between fields: a=%v b=%v", a.Props, b.Props)
	}
	// Later caller edits must not leak into the returned component.
	shared["hint"] = "changed"
	if a.Props["hint"] != "a" {
		t.Fatalf("returned props share storage with the caller: %v", a.Props)
	}
}
