package dsnet

import (
	"context"
	"fmt"
	"log"

	pb "github.com/distcode/dsnet/proto"
)

type HandlerFunc[T any] func(ctx context.Context, from string, msg T) error

// On registers a type-safe handler for a specific operation on a DSNet instance.
func On[T any](d *DSNet, operation string, handler HandlerFunc[T]) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.handlers[operation] = func(ctx context.Context, from string, env *pb.Envelope) error {
		var msg T
		if err := decodeStruct(env.Payload, &msg); err != nil {
			return fmt.Errorf("decode %s: %w", operation, err)
		}
		return handler(ctx, from, msg)
	}
}

// Executes a registered handler
func (d *DSNet) dispatch(ctx context.Context, env *pb.Envelope) {
	d.mu.RLock()
	handler := d.handlers[env.Operation]
	d.mu.RUnlock()

	if handler != nil {
		go func() {
			if err := handler(ctx, env.From, env); err != nil {
				log.Printf("[%s] handler for %s failed: %v", d.nodeId, env.Operation, err)
			}
		}()
	} else {
		select {
		case d.Inbox <- env:
		default:
			log.Printf("[%s] inbox full; dropping message for %s", d.nodeId, env.Operation)
		}
	}
}
