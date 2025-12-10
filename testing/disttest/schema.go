package disttest

type ResultType string

const (
	TypeSuccess ResultType = "success"
	TypeFailure ResultType = "failure"
	TypePanic   ResultType = "panic"
)

// TestResult represents the result of a single test execution.
type TestResult struct {
	Type       ResultType `json:"type"`
	Name       string     `json:"name"`
	DurationMs int64      `json:"duration_ms"`
	Message    string     `json:"message,omitempty"`
	Panic      string     `json:"panic,omitempty"`
}
