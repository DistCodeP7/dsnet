package mutex_central_coord

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/distcodep7/dsnet/dsnet"
	ex "github.com/distcodep7/dsnet/exercises/mutex"
)

type MutexNode struct{ Net *dsnet.Node; Total int }

// Centralized coordinator (N1) state
type state struct {
	// client-side
	waitingGrant bool
	mutexID      string
	workMillis   int

	// coordinator-side
	isCoordinator bool
	started       bool
	inCS          bool
	holder        string
	queue         []string
	completed     map[string]bool
	allNodes      []string
}

func NewMutexNode(id string, total int) *MutexNode {
	n, err := dsnet.NewNode(id, "localhost:50051")
	if err != nil {
		log.Fatalf("Failed to create node %s: %v", id, err)
	}
	return &MutexNode{Net: n, Total: total}
}

func (en *MutexNode) Run(ctx context.Context) {
	defer en.Net.Close()
	// Central coordinator design: N1 coordinates; others request/release to N1
	nodes := make([]string, 0, en.Total)
	for i := 1; i <= en.Total; i++ { nodes = append(nodes, fmt.Sprintf("N%d", i)) }
	st := state{ isCoordinator: en.Net.ID == "N1", started: false, inCS: false, holder: "", queue: []string{}, workMillis: 300, completed: map[string]bool{}, allNodes: nodes }

	for {
		select {
		case event := <-en.Net.Inbound:
			handleEvent(ctx, en, &st, event)
		case <-ctx.Done():
			return
		}
	}
}

func handleEvent(ctx context.Context, en *MutexNode, st *state, event dsnet.Event) {
	switch event.Type {
	case "MutexTrigger":
		var trig ex.MutexTrigger
		if err := json.Unmarshal(event.Payload, &trig); err != nil { return }
		st.mutexID = trig.MutexID
		if trig.WorkMillis > 0 { st.workMillis = trig.WorkMillis }

		if st.isCoordinator {
			if st.started { return }
			st.started = true
			// Ask all other nodes to request once
			for _, id := range st.allNodes {
				if id == en.Net.ID { continue }
				t := ex.MutexTrigger{ BaseMessage: dsnet.BaseMessage{ From: en.Net.ID, To: id, Type: "MutexTrigger" }, MutexID: st.mutexID, WorkMillis: st.workMillis }
				en.Net.Send(context.Background(), id, t)
			}
			// Coordinator takes its own turn first
			st.inCS = true
			doCoordinatorCS(en, st)
		} else {
			// Send request to coordinator N1 and wait for grant
			req := ex.RequestCS{ BaseMessage: dsnet.BaseMessage{ From: en.Net.ID, To: "N1", Type: "RequestCS" }, MutexID: st.mutexID }
			en.Net.Send(context.Background(), "N1", req)
			st.waitingGrant = true
		}

	case "RequestCS":
		if !st.isCoordinator { return }
		var req ex.RequestCS
		if err := json.Unmarshal(event.Payload, &req); err != nil { return }
		handleCoordinatorRequest(en, st, req.From, req.MutexID)

	case "ReplyCS":
		if st.isCoordinator { return }
		var rep ex.ReplyCS
		if err := json.Unmarshal(event.Payload, &rep); err != nil { return }
		if !st.waitingGrant { return }
		if rep.Granted {
			st.waitingGrant = false
			doClientCS(en, st)
		} else {
			// Backoff and retry
			time.Sleep(100 * time.Millisecond)
			req := ex.RequestCS{ BaseMessage: dsnet.BaseMessage{ From: en.Net.ID, To: "N1", Type: "RequestCS" }, MutexID: st.mutexID }
			en.Net.Send(context.Background(), "N1", req)
		}

	case "ReleaseCS":
		if !st.isCoordinator { return }
		// client released; mark completion and maybe finish, else grant next
		st.inCS = false
		st.holder = ""
		st.completed[event.From] = true
		if len(st.completed) >= len(st.allNodes) {
			res := ex.MutexResult{ BaseMessage: dsnet.BaseMessage{ From: en.Net.ID, To: "TESTER", Type: "MutexResult" }, MutexID: st.mutexID, NodeId: en.Net.ID, Success: true }
			en.Net.Send(context.Background(), "TESTER", res)
			return
		}
		grantNext(en, st)
	}
}

func handleCoordinatorRequest(en *MutexNode, st *state, from string, mutexID string) {
	// If free and no holder, grant immediately; else deny now and enqueue
	if !st.inCS && st.holder == "" {
		st.inCS = true
		st.holder = from
		rep := ex.ReplyCS{ BaseMessage: dsnet.BaseMessage{ From: en.Net.ID, To: from, Type: "ReplyCS" }, MutexID: mutexID, Granted: true }
		en.Net.Send(context.Background(), from, rep)
		return
	}
	// enqueue and send denial
	st.queue = append(st.queue, from)
	rep := ex.ReplyCS{ BaseMessage: dsnet.BaseMessage{ From: en.Net.ID, To: from, Type: "ReplyCS" }, MutexID: mutexID, Granted: false }
	en.Net.Send(context.Background(), from, rep)
}

func grantNext(en *MutexNode, st *state) {
	if st.inCS || len(st.queue) == 0 { return }
	next := st.queue[0]
	st.queue = st.queue[1:]
	st.inCS = true
	st.holder = next
	rep := ex.ReplyCS{ BaseMessage: dsnet.BaseMessage{ From: en.Net.ID, To: next, Type: "ReplyCS" }, MutexID: st.mutexID, Granted: true }
	en.Net.Send(context.Background(), next, rep)
}

func doClientCS(en *MutexNode, st *state) {
	// Simulate CS
	time.Sleep(time.Duration(st.workMillis) * time.Millisecond)
	// Release to coordinator
	rel := ex.ReleaseCS{ BaseMessage: dsnet.BaseMessage{ From: en.Net.ID, To: "N1", Type: "ReleaseCS" }, MutexID: st.mutexID }
	en.Net.Send(context.Background(), "N1", rel)
	// Report completion to tester
	res := ex.MutexResult{ BaseMessage: dsnet.BaseMessage{ From: en.Net.ID, To: "TESTER", Type: "MutexResult" }, MutexID: st.mutexID, NodeId: en.Net.ID, Success: true }
	en.Net.Send(context.Background(), "TESTER", res)
}

func doCoordinatorCS(en *MutexNode, st *state) {
	// Coordinator own CS
	time.Sleep(time.Duration(st.workMillis) * time.Millisecond)
	st.inCS = false
	st.holder = ""
	// Coordinator also reports its own completion
	res := ex.MutexResult{ BaseMessage: dsnet.BaseMessage{ From: en.Net.ID, To: "TESTER", Type: "MutexResult" }, MutexID: st.mutexID, NodeId: en.Net.ID, Success: true }
	en.Net.Send(context.Background(), "TESTER", res)
	grantNext(en, st)
}

func Solution(ctx context.Context, numNodes int) {
	if numNodes < 1 { numNodes = 1 }
	for i := 1; i <= numNodes; i++ {
		go NewMutexNode(fmt.Sprintf("N%d", i), numNodes).Run(ctx)
	}
	<-ctx.Done()
}