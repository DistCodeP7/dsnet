package testing

// LogEntry defines the structure for the visualizer file
type LogEntry struct {
	Timestamp   int64             `json:"timestamp"` // Wall clock time
	From        string            `json:"from"`
	To          string            `json:"to"`
	Type        string            `json:"type"`
	VectorClock map[string]uint64 `json:"vector_clock"`
	Payload     string            `json:"payload"`
}
