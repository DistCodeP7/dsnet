package consensus

import "github.com/distcodep7/dsnet/dsnet"

// ==========================================================
// 1. EXTERNAL TRIGGERS (The "Input")
// ==========================================================

type ConsensusTrigger struct {
	dsnet.BaseMessage        // Type: "ConsensusTrigger"
	ConsensusID      string `json:"consensus_id"`
}

// ==========================================================
// 2. INTERNAL PROTOCOL (The "Logic")
// ==========================================================

type ProposalMessage struct {
	dsnet.BaseMessage        // Type: "ProposalMessage"
	ConsensusID     string `json:"consensus_id"`
	Value			string `json:"value"`
	Round			int    `json:"round"`
}

type VoteMessage struct {
	dsnet.BaseMessage        // Type: "VoteMessage"
	ConsensusID      string `json:"consensus_id"`
	Proposal         string `json:"proposal"`
	Vote             bool   `json:"vote"`
}	

// ==========================================================
// 3. EXTERNAL RESULTS (The "Output")
// ==========================================================

type ConsensusResult struct {
	dsnet.BaseMessage
	ConsensusID 	string `json:"consensus_id"`
	Success     	bool   `json:"success"`
	DecidedValue 	string `json:"decided_value"`
}