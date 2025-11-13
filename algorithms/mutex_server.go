package algorithms

import (
	"sync"

	pb "github.com/distcode/dsnet/proto"
)

type MutexServer struct {
	mu        sync.Mutex
	NodeID    string
	hasToken  bool
	waitQueue []string
}

func NewMutexServer(nodeID string, hasToken bool) *MutexServer {
	return &MutexServer{
		NodeID:   nodeID,
		hasToken: hasToken,
		waitQueue: []string{},
	}
}

func (h *MutexServer) OnEvent(env *pb.Envelope) []*pb.Envelope {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch env.Type {
	case pb.MessageType_REQUEST_TOKEN:
		if h.hasToken {
			h.hasToken = false
			return []*pb.Envelope{
				{
					From:    h.NodeID,
					To:      env.From,
					Payload: "TOKEN",
					Type:    pb.MessageType_TOKEN,
				},
			}
		}
		h.waitQueue = append(h.waitQueue, env.From)

	case pb.MessageType_RELEASE_TOKEN:
		if len(h.waitQueue) > 0 {
			next := h.waitQueue[0]
			h.waitQueue = h.waitQueue[1:]
			return []*pb.Envelope{
				{
					From:    h.NodeID,
					To:      next,
					Payload: "TOKEN",
					Type:    pb.MessageType_TOKEN,
				},
			}
		}
		h.hasToken = true
	}

	return nil
}

func (h *MutexServer) NumWaiting() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.waitQueue)
}
