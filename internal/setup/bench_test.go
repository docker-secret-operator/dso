package setup

import (
	"context"
	"testing"
)

// Benchmarks measure the CPU and allocation cost of each pipeline stage.
// All OS interactions use the same injectable fakes as the unit tests.

func BenchmarkDetect(b *testing.B) {
	d := newDetector()
	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		_, _ = d.Detect(ctx)
	}
}

func BenchmarkValidate_Local(b *testing.B) {
	env := &Environment{
		Docker: DockerInfo{BinaryFound: true, DaemonReachable: true},
		Capabilities: Capabilities{
			SupportsDocker:    true,
			SupportsLocalMode: true,
		},
	}
	v := newValidator()
	ctx := context.Background()
	opts := SetupOptions{Mode: ModeLocal, Provider: "local"}
	b.ResetTimer()
	for range b.N {
		_, _ = v.Validate(ctx, env, opts)
	}
}

func BenchmarkPlan_Local(b *testing.B) {
	p := newPlanner()
	env := &Environment{User: UserInfo{HomeDir: "/home/bench"}}
	vr := &ValidationResult{}
	ctx := context.Background()
	opts := SetupOptions{Mode: ModeLocal, Provider: "local"}
	b.ResetTimer()
	for range b.N {
		_, _ = p.Plan(ctx, env, vr, opts)
	}
}

func BenchmarkPlan_Agent(b *testing.B) {
	p := newPlanner()
	env := &Environment{User: UserInfo{IsRoot: true}}
	vr := &ValidationResult{}
	ctx := context.Background()
	opts := SetupOptions{Mode: ModeAgent, Provider: "aws"}
	b.ResetTimer()
	for range b.N {
		_, _ = p.Plan(ctx, env, vr, opts)
	}
}

func BenchmarkPreview_Terminal(b *testing.B) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(),
		&Environment{User: UserInfo{HomeDir: "/home/bench"}},
		&ValidationResult{},
		SetupOptions{Mode: ModeLocal, Provider: "local"},
	)
	eng := &Engine{Events: &Emitter{}}
	b.ResetTimer()
	for range b.N {
		_, _ = eng.preview(plan, "terminal")
	}
}

func BenchmarkPreview_JSON(b *testing.B) {
	p := newPlanner()
	plan, _ := p.Plan(context.Background(),
		&Environment{User: UserInfo{IsRoot: true}},
		&ValidationResult{},
		SetupOptions{Mode: ModeAgent, Provider: "aws"},
	)
	eng := &Engine{Events: &Emitter{}}
	b.ResetTimer()
	for range b.N {
		_, _ = eng.preview(plan, "json")
	}
}

func BenchmarkEmitter_Emit_NoListeners(b *testing.B) {
	e := &Emitter{}
	evt := Event{Type: EventSetupStarted}
	b.ResetTimer()
	for range b.N {
		e.Emit(evt)
	}
}

func BenchmarkEmitter_Emit_TenListeners(b *testing.B) {
	e := &Emitter{}
	for range 10 {
		e.Subscribe(func(_ Event) {})
	}
	evt := Event{Type: EventSetupStarted}
	b.ResetTimer()
	for range b.N {
		e.Emit(evt)
	}
}

func BenchmarkGeneratePlanID(b *testing.B) {
	for range b.N {
		_ = generatePlanID()
	}
}

func BenchmarkGenerateTransactionID(b *testing.B) {
	for range b.N {
		_ = generateTransactionID()
	}
}

func BenchmarkEngine_Setup_DryRun(b *testing.B) {
	b.ResetTimer()
	for range b.N {
		eng := newTestEngine()
		_, _ = eng.Setup(context.Background(), SetupOptions{
			Mode:   ModeLocal,
			DryRun: true,
		})
	}
}

func BenchmarkDoctor_Plan_AllPass(b *testing.B) {
	r := NewRepair(RepairOptions{})
	result := allPassDoctorResult()
	ctx := context.Background()
	b.ResetTimer()
	for range b.N {
		_ = r.Plan(ctx, result)
	}
}

func BenchmarkContainsPath(b *testing.B) {
	msg := "DSO configuration found at /etc/dso/dso.yaml — setup will upgrade"
	path := "/etc/dso/dso.yaml"
	b.ResetTimer()
	for range b.N {
		_ = containsPath(msg, path)
	}
}
