package provider

import (
	"context"
	"runtime"
)

func errContextDeadline() error { return context.DeadlineExceeded }

func runtimeNumGoroutine() int { return runtime.NumGoroutine() }
