package dsnet

import pb "github.com/distcode/dsnet/proto"

// Send sends a direct message to a single node.
func (d *DSNet) Send(to string, payload any, operation string) error {
	structPayload, err := encodeStruct(payload)
	if err != nil {
		return err
	}
	env := &pb.Envelope{
		From:         d.nodeId,
		To:           to,
		Payload:      structPayload,
		DeliveryType: pb.DeliveryType_DIRECT,
		Operation:    operation,
		Seq:          d.seq,
	}
	d.seq++
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Send{Send: env},
	})
}

// Broadcast sends a message to all nodes.
func (d *DSNet) Broadcast(payload any, operation string) error {
	structPayload, err := encodeStruct(payload)
	if err != nil {
		return err
	}
	env := &pb.Envelope{
		From:         d.nodeId,
		Payload:      structPayload,
		DeliveryType: pb.DeliveryType_BROADCAST,
		Operation:    operation,
		Seq:          d.seq,
	}
	d.seq++
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Send{Send: env},
	})
}

// Publish sends a message to a group.
func (d *DSNet) Publish(group string, payload any, operation string) error {
	structPayload, err := encodeStruct(payload)
	if err != nil {
		return err
	}
	env := &pb.Envelope{
		From:         d.nodeId,
		Group:        group,
		Payload:      structPayload,
		DeliveryType: pb.DeliveryType_GROUP,
		Operation:    operation,
		Seq:          d.seq,
	}
	d.seq++
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Send{
			Send: env,
		},
	})
}

// Subscribe subscribes the node to a group.
func (d *DSNet) Subscribe(group string) error {
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Subscribe{
			Subscribe: &pb.SubscribeReq{
				NodeId: d.nodeId,
				Group:  group,
			},
		},
	})
}

// Unsubscribe unsubscribes the node from a group.
func (d *DSNet) Unsubscribe(group string) error {
	return d.stream.Send(&pb.ClientToController{
		Payload: &pb.ClientToController_Unsubscribe{
			Unsubscribe: &pb.UnsubscribeReq{
				NodeId: d.nodeId,
				Group:  group,
			},
		},
	})
}
