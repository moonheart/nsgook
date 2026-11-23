package viewer

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

type stubPlugin struct {
	id, name  string
	priority  int
	canHandle func(string) bool
	extItems  []MenuAction
}

func (s *stubPlugin) ID() string                 { return s.id }
func (s *stubPlugin) Name() string               { return s.name }
func (s *stubPlugin) Priority() int              { return s.priority }
func (s *stubPlugin) CanHandle(path string) bool { return s.canHandle(path) }
func (s *stubPlugin) Prepare(string, *Context)   {}
func (s *stubPlugin) View(*core.Scope, string, *Context) core.Node {
	return nil
}
func (s *stubPlugin) ExtendedMenuItems(*core.Scope, string, *Context) []MenuAction {
	return s.extItems
}

func TestRegistryPriorityAndPanicRecovery(t *testing.T) {
	def := &stubPlugin{id: "default", name: "Default", canHandle: func(string) bool { return true }}
	low := &stubPlugin{id: "low", name: "Low", priority: -5, canHandle: func(string) bool { return true }, extItems: []MenuAction{{ID: "low-act", Label: "Low Act"}}}
	high := &stubPlugin{id: "high", name: "High", priority: 10, canHandle: func(p string) bool { return p == "match.txt" }}
	panicky := &stubPlugin{id: "panic", name: "Panic", priority: 100, canHandle: func(string) bool { panic("boom") }}

	reg := NewRegistry(def, low, high, panicky)

	if got := reg.FindMatch("match.txt"); got.ID() != "high" {
		t.Fatalf("expected high plugin despite panicking higher-priority plugin, got %s", got.ID())
	}
	if got := reg.FindMatch("other.bin"); got.ID() != "low" {
		t.Fatalf("expected low plugin for other.bin, got %s", got.ID())
	}
	if got := reg.FindMatch(""); got.ID() != "default" {
		t.Fatalf("expected default plugin for empty path, got %s", got.ID())
	}

	ext := reg.CollectExtendedMenuItems(nil, high, "match.txt", nil)
	if len(ext) == 0 || ext[0].ID != "low-act" {
		t.Fatalf("expected extended menu items from non-active plugins, got %+v", ext)
	}
}

func TestSetPreferredSizeFit(t *testing.T) {
	ctx := &Context{}
	desktop := core.Size{Width: 1000, Height: 800}

	// Small image should not upscale
	ratio := ctx.SetPreferredSizeFit(core.Size{Width: 300, Height: 200}, 0.8, desktop)
	if ratio != 1 || ctx.PreferredSize != (core.Size{Width: 300, Height: 200}) {
		t.Fatalf("unexpected small fit: ratio=%v size=%+v", ratio, ctx.PreferredSize)
	}

	// Oversized image should fit inside 80% of desktop (800x640)
	ratio = ctx.SetPreferredSizeFit(core.Size{Width: 1600, Height: 800}, 0.8, desktop)
	if ratio != 0.5 || ctx.PreferredSize != (core.Size{Width: 800, Height: 400}) {
		t.Fatalf("unexpected oversized fit: ratio=%v size=%+v", ratio, ctx.PreferredSize)
	}
}
