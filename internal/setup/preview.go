package setup

// Renderer renders an InstallPlan as a formatted string.
// Implementations must be stateless and deterministic.
// They must never inspect Environment, ValidationResult, or SetupOptions —
// only the InstallPlan is in scope.
type Renderer interface {
	Render(plan InstallPlan) (string, error)
}

// PreviewEngine delegates rendering to a Renderer.
// It has no state of its own and makes no decisions.
type PreviewEngine struct {
	renderer Renderer
}

// NewPreviewEngine constructs a PreviewEngine with the given renderer.
func NewPreviewEngine(r Renderer) *PreviewEngine {
	return &PreviewEngine{renderer: r}
}

// Render forwards the plan to the underlying renderer.
func (pe *PreviewEngine) Render(plan InstallPlan) (string, error) {
	return pe.renderer.Render(plan)
}

// newRenderer selects a Renderer from a format string.
// "json" → JSONRenderer; anything else (including "") → TerminalRenderer.
func newRenderer(format string) Renderer {
	if format == "json" {
		return &JSONRenderer{}
	}
	return &TerminalRenderer{}
}
