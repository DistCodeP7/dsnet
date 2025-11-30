package mutex

import "github.com/distcodep7/dsnet/dsnet"

// ==========================================================
// 1. EXTERNAL TRIGGERS (The "Input")
// ==========================================================

type MutexTrigger struct {
	dsnet.BaseMessage        // Type: "MutexTrigger"
	MutexID        string `json:"mutex_id"`
}

// ==========================================================
// 2. INTERNAL PROTOCOL (The "Logic")
// ==========================================================

// RequestCS is sent between nodes to request access to the critical section.
type RequestCS struct {
	dsnet.BaseMessage 		// Type: "RequestCS"
	MutexID      	string `json:"mutex_id"`
	Timestamp    	int64  `json:"timestamp"`
}

// ReplyCS is the reply to a RequestCS.
type ReplyCS struct {
	dsnet.BaseMessage // Type: "ReplyCS"
	MutexID      	string `json:"mutex_id"`
	Granted	 		bool   `json:"granted"`
}

// ==========================================================
// 3. EXTERNAL RESULTS (The "Output")
// ==========================================================

type MutexResult struct {
	dsnet.BaseMessage
	MutexID 		string `json:"mutex_id"`
	NodeId  		string `json:"node_id"` // Node that finished its CS execution
	Success 		bool   `json:"success"` // True if mutual exclusion and progress were maintained
}