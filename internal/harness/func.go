package harness

import "context"

// Func adapts a Go function into a Harness. It is used for in-process fakes
// in tests and for embedding council as a library.
type Func struct {
	AgentName string
	KindName  string
	Fn        func(ctx context.Context, prompt string) (string, error)
}

func (f *Func) Name() string { return f.AgentName }
func (f *Func) Kind() string {
	if f.KindName == "" {
		return "func"
	}
	return f.KindName
}
func (f *Func) Run(ctx context.Context, prompt string) (string, error) { return f.Fn(ctx, prompt) }
