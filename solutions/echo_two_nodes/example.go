package echo_two_nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/distcodep7/dsnet/dsnet"
	"github.com/distcodep7/dsnet/exercises/echo"
)

type EchoNode struct{ Net *dsnet.Node }

func NewEchoNode(id, controllerAddr string) *EchoNode {
	n, err := dsnet.NewNode(id, controllerAddr)
	if err != nil {
		log.Fatalf("Failed to create node %s: %v", id, err)
	}
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
	defer en.Net.Close()
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
			en.Net.Send(ctx, "N2", echo.EchoMessage{
				BaseMessage: newBaseMessage(en.Net.ID, "N2", "EchoMessage"),
				EchoID:  msg.EchoID,
				Content: msg.Content,
			})
		case "EchoMessage":
			var msg echo.EchoMessage
			json.Unmarshal(event.Payload, &msg)

    		en.Net.Send(ctx, msg.From, echo.EchoResponse{
				BaseMessage: newBaseMessage(en.Net.ID, msg.From, "EchoResponse"),
				EchoID:  msg.EchoID,
				Content: msg.Content,
			})
		case "EchoResponse":
			var resp echo.EchoResponse
			json.Unmarshal(event.Payload, &resp)

			en.Net.Send(ctx, "TESTER", echo.ReplyReceived{
				BaseMessage: newBaseMessage(en.Net.ID, "TESTER", "ReplyReceived"),
				EchoID:  resp.EchoID,
				Success: true,
			})
	}
}

func Solution(ctx context.Context) {
	for i := 1; i <= 2; i++ {
		nodeID := fmt.Sprintf("N%d", i)
		go NewEchoNode(nodeID, "localhost:50051").Run(ctx)
	}

	<-ctx.Done()
}
