package pluginsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/hyuan280/Sonicore-PluginSDK/go/gen"
)

// Component is one node of the UI assembly tree a plugin returns for
// GetForm/GetPage — the process-isolated equivalent of an in-process
// plugin's component-tree assembly. The vocabulary is Vuetify's: authors
// use the component names from the Vuetify documentation (VForm, VRow,
// VCol, VSwitch, VTextField, VTextarea, VSelect, VCheckbox, VAlert, VCard,
// VCardTitle, VCardText, VTable, VDivider, ...). The host frontend maps
// each name to its own implementation; unknown names render as a warning
// box.
//
// Conventions:
//   - form fields bind to the plugin's config keys via props["model"]
//     (the Vuetify v-model habit); values are stored by the host and
//     delivered to the plugin via OnConfig
//   - VSelect uses the official items format: [{title, value}, ...];
//     props["multiple"] switches to multi-select
//   - VTable is a simplified table: props["columns"] ([]string) and
//     props["rows"] ([][]string)
//   - VCardTitle/VCardText take props["text"]
type Component struct {
	Component string         `json:"component"`
	Content   []Component    `json:"content,omitempty"`
	Props     map[string]any `json:"props,omitempty"`
}

// Node builds one UI assembly node.
func Node(component string, props map[string]any, content ...Component) Component {
	return Component{Component: component, Props: props, Content: content}
}

// FieldNode builds a config form field node: model binds the config key.
func FieldNode(component, model, label string, props map[string]any) Component {
	// Copy the caller's map: writing into it would mutate shared storage
	// (two fields built from one map would overwrite each other's model/
	// label, and later caller-side edits would leak into this component).
	out := make(map[string]any, len(props)+2)
	for k, v := range props {
		out[k] = v
	}
	out["model"] = model
	out["label"] = label
	return Component{Component: component, Props: out}
}

var (
	uiMu     sync.RWMutex
	formFunc func(context.Context) Component
	pageFunc func(context.Context) Component
)

// OnForm registers the config form builder (get_form equivalent). The host
// fetches the form via UIService.GetForm; the builder runs per request, so
// dynamic options stay fresh. Call it from Init. Return an empty Component
// to signal "no form" (the host then falls back to the manifest schema);
// handle internal errors yourself (log and return an empty/degraded tree) —
// panics are recovered by the SDK and reported to the host.
func OnForm(fn func(ctx context.Context) Component) error {
	if fn == nil {
		return errors.New("pluginsdk: OnForm function is nil")
	}
	uiMu.Lock()
	formFunc = fn
	uiMu.Unlock()
	return nil
}

// OnPage registers the data page builder (get_page equivalent). The host
// fetches it via UIService.GetPage; the builder runs per request, so the
// page reflects the current plugin state. Call it from Init. Return an
// empty Component when there is nothing to show: the host frontend then
// only displays the config form. Handle internal errors yourself (log and
// return an empty/degraded tree) — panics are recovered by the SDK and
// reported to the host.
func OnPage(fn func(ctx context.Context) Component) error {
	if fn == nil {
		return errors.New("pluginsdk: OnPage function is nil")
	}
	uiMu.Lock()
	pageFunc = fn
	uiMu.Unlock()
	return nil
}

// uiServer implements the plugin-side UIService: it runs the registered
// builders and serializes the tree.
type uiServer struct {
	gen.UnimplementedUIServiceServer
}

func newUIServer() *uiServer { return &uiServer{} }

func (s *uiServer) GetForm(ctx context.Context, req *gen.GetFormRequest) (*gen.GetFormResponse, error) {
	uiMu.RLock()
	fn := formFunc
	uiMu.RUnlock()
	if fn == nil {
		return &gen.GetFormResponse{}, nil
	}
	root, err := runUIBuilder(ctx, fn)
	if err != nil {
		return nil, err
	}
	if root.Component == "" {
		return &gen.GetFormResponse{}, nil
	}
	raw, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("pluginsdk: encode form: %w", err)
	}
	return &gen.GetFormResponse{SchemaJson: string(raw)}, nil
}

func (s *uiServer) GetPage(ctx context.Context, req *gen.GetPageRequest) (*gen.GetPageResponse, error) {
	uiMu.RLock()
	fn := pageFunc
	uiMu.RUnlock()
	if fn == nil {
		return &gen.GetPageResponse{}, nil
	}
	root, err := runUIBuilder(ctx, fn)
	if err != nil {
		return nil, err
	}
	if root.Component == "" {
		return &gen.GetPageResponse{}, nil
	}
	raw, err := json.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("pluginsdk: encode page: %w", err)
	}
	return &gen.GetPageResponse{SchemaJson: string(raw)}, nil
}

// runUIBuilder executes a registered UI builder with panic recovery (the
// panic is logged and reported to the host, like every other plugin hook).
func runUIBuilder(ctx context.Context, fn func(context.Context) Component) (out Component, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = reportPanic(r)
		}
	}()
	return fn(ctx), nil
}
