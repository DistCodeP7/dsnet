package disttest

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	results         []TestResult
	mu              sync.Mutex
	failureMessages = make(map[string][]string)
)

// Wrap should be used around each test body:
func Wrap(t *testing.T, fn func(t *testing.T)) {
	name := t.Name()
	start := time.Now()

	defer func() {
		elapsed := time.Since(start).Milliseconds()

		mu.Lock()
		defer mu.Unlock()

		defer delete(failureMessages, name)

		if r := recover(); r != nil {
			t.Fail()
			results = append(results, TestResult{
				Type:       TypePanic,
				Name:       name,
				DurationMs: elapsed,
				Panic:      formatPanic(r),
			})
			return
		}

		if t.Failed() {
			msg := "test failed"
			if msgs, ok := failureMessages[name]; ok && len(msgs) > 0 {
				msg = strings.Join(msgs, "; ")
			}

			results = append(results, TestResult{
				Type:       TypeFailure,
				Name:       name,
				DurationMs: elapsed,
				Message:    msg,
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

// Fail records a failure reason for the current test and fails the test.
func Fail(t *testing.T, format string, args ...interface{}) {
	t.Helper()

	msg := fmt.Sprintf(format, args...)

	mu.Lock()
	failureMessages[t.Name()] = append(failureMessages[t.Name()], msg)
	mu.Unlock()

	t.Errorf("%s", msg)
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
