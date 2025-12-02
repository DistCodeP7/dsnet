package disttest

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

var (
	results []TestResult
	mu      sync.Mutex
)

func Wrap(t *testing.T, fn func(t *testing.T)) {
	name := t.Name()
	start := time.Now()

	defer func() {
		elapsed := time.Since(start).Milliseconds()

		mu.Lock()
		defer mu.Unlock()

		if r := recover(); r != nil {
			t.Fail() // ensure “failed” in go test output
			results = append(results, TestResult{
				Type:       TypePanic,
				Name:       name,
				DurationMs: elapsed,
				Panic:      formatPanic(r),
			})
			return
		}

		if t.Failed() {
			results = append(results, TestResult{
				Type:       TypeFailure,
				Name:       name,
				DurationMs: elapsed,
				Message:    "test failed",
			})
			return
		}

		results = append(results, TestResult{
			Type:       TypeSuccess,
			Name:       name,
			DurationMs: elapsed,
		})
	}()

	fn(t)
}

func Write(file string) error {
	mu.Lock()
	defer mu.Unlock()

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0o644)
}

func formatPanic(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case error:
		return x.Error()
	default:
		return "unknown panic"
	}
}
