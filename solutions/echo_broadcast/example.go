package echo_broadcast

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/distcodep7/dsnet/dsnet"
	"github.com/distcodep7/dsnet/exercises/echo"
)

var totalNodes int // From ENV

type EchoNode struct {
	Net *dsnet.Node
	pendingReplies map[string]map[string]bool // echoID -> map[nodeID]bool
}

func NewEchoNode(id string) *EchoNode {
	n, _ := dsnet.NewNode(id, "localhost:50051")
	return &EchoNode{Net: n}
}

func newBaseMessage(from, to, msgType string) dsnet.BaseMessage {
	return dsnet.BaseMessage{
		From: from,
		To:   to,
		Type: msgType,
	}
}

func (en *EchoNode) Run(ctx context.Context) {
	for {
		select {
		case event := <-en.Net.Inbound:
			en.handleEvent(ctx, event)
		case <-ctx.Done():
			return
		}
	}
}

func (en *EchoNode) handleEvent(ctx context.Context, event dsnet.Event) {
	switch event.Type {

		case "SendTrigger":
			var msg echo.SendTrigger
			json.Unmarshal(event.Payload, &msg)
			fmt.Printf("[%s] ⚡ Trigger received. EchoID: %s, Content: %s\n", en.Net.ID, msg.EchoID, msg.Content)

			if en.pendingReplies == nil {
				en.pendingReplies = make(map[string]map[string]bool)
			}
			en.pendingReplies[msg.EchoID] = make(map[string]bool)

			en.SendToAll(ctx, msg.EchoID, msg.Content)
		case "EchoMessage":
			var msg echo.EchoMessage
			json.Unmarshal(event.Payload, &msg)
			fmt.Printf("[%s] 📩 EchoMessage received. EchoID: %s, Content: %s\n", en.Net.ID, msg.EchoID, msg.Content)

			en.Net.Send(ctx, msg.From, echo.EchoResponse{
				BaseMessage: newBaseMessage(en.Net.ID, msg.From, "EchoResponse"),
        		EchoID:  msg.EchoID,
        		Content: msg.Content,
			})
		case "EchoResponse":
			// Handle EchoResponse
			var resp echo.EchoResponse
			json.Unmarshal(event.Payload, &resp)
			fmt.Printf("[%s] 📩 EchoResponse received. EchoID: %s, From %s, Content: %s\n", en.Net.ID, resp.EchoID, resp.From, resp.Content)

			if en.pendingReplies == nil {
				en.pendingReplies = make(map[string]map[string]bool)
			}
			if en.pendingReplies[resp.EchoID] == nil {
				en.pendingReplies[resp.EchoID] = make(map[string]bool)
			}

			fromNode := resp.From
			if fromNode == en.Net.ID {
				return // ignore self
			}

			// only mark first response from a node
			if !en.pendingReplies[resp.EchoID][fromNode] {
				en.pendingReplies[resp.EchoID][fromNode] = true
			}

			if len(en.pendingReplies[resp.EchoID]) == totalNodes - 1 {
				// All replies received
				en.Net.Send(ctx, "TESTER", echo.ReplyReceived{
					BaseMessage: newBaseMessage(en.Net.ID, "TESTER", "ReplyReceived"),
					EchoID:      resp.EchoID,
					Success:     true,
				})
				fmt.Printf("[%s] ✅ All EchoResponses received for EchoID: %s. Sent ReplyReceived to TESTER.\n", en.Net.ID, resp.EchoID)
				delete(en.pendingReplies, resp.EchoID)
			}
	}
}

func (en *EchoNode) SendToAll(ctx context.Context, echoID string, content string) {
	for i := 1; i <= totalNodes; i++ {
		nodeID := fmt.Sprintf("N%d", i)
		if nodeID == en.Net.ID {
			continue // skip self
		}
		
		en.Net.Send(ctx, nodeID, echo.EchoMessage{
			BaseMessage: newBaseMessage(en.Net.ID, nodeID, "EchoMessage"),
			EchoID:      echoID,
			Content:     content,
		})
	}
}

func Solution(ctx context.Context, nodeNum int) {
	totalNodes = nodeNum
	for i := 1; i <= totalNodes; i++ {
		nodeID := fmt.Sprintf("N%d", i)
		go NewEchoNode(nodeID).Run(ctx)
	}

	<-ctx.Done()
}
