package testing

import (
	"encoding/json"
)

type EvtType string

const (
	EvtTypeSend EvtType = "SEND"
	EvtTypeRecv EvtType = "RECV"
	EvtTypeDrop EvtType = "DROP"
)

type TraceEvent struct {
	ID          string            `json:"id"`
	MessageID   string            `json:"message_id"`
	Timestamp   int64             `json:"ts"`
	EvtType     EvtType           `json:"evt_type"`
	MsgType     string            `json:"msg_type"`
	From        string            `json:"from"`
	To          string            `json:"to"`
	VectorClock map[string]uint64 `json:"vc"`
	Payload     json.RawMessage   `json:"payload,omitempty"`
}
