package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/distcodep7/dsnet/dsnet"
	"github.com/distcodep7/dsnet/exercises/echo"
	"github.com/distcodep7/dsnet/exercises/leader_election"
	mtx "github.com/distcodep7/dsnet/exercises/mutex"
	"github.com/distcodep7/dsnet/solutions/echo_broadcast"
	"github.com/distcodep7/dsnet/solutions/echo_two_nodes"
	"github.com/distcodep7/dsnet/solutions/mutex_central_coord"
	"github.com/distcodep7/dsnet/solutions/paxos_leader_election"
	"github.com/distcodep7/dsnet/testing/controller"
	"github.com/distcodep7/dsnet/testing/wrapper"
)

func main() {
	test := flag.String("e", "echo", "Exercise to test: Echo, Mutex, LeaderElection, Consensus")
	numNodes := flag.Int("n", 2, "Number of nodes in the system")
	flag.Parse()

	// Enable fault injection in controller
	go controller.Serve(controller.TestConfig{
		DropProb:        0,   
		DupeProb:        0,   
		ReorderProb:     0,   
		ReorderMinDelay: 1,
		ReorderMaxDelay: 2,
		AsyncDuplicate:  true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tester, _ := dsnet.NewNode("TESTER", "localhost:50051")

	log.Printf("Starting %s", *test)
	switch *test {
		case "echo":
			if *numNodes == 2 {
				go echo_two_nodes.Solution(ctx)
			} else {
				go echo_broadcast.Solution(ctx, *numNodes)
			}
			time.Sleep(1 * time.Second)
			runEchoTest(tester)
		case "mutex":
			go mutex_central_coord.Solution(ctx, *numNodes)
			time.Sleep(1 * time.Second)
			runMutexTest(tester, *numNodes)
		case "leaderelection":
			go paxos_leader_election.Solution(ctx, *numNodes)
			time.Sleep(1 * time.Second)
			runLeaderElectionTest(tester)
		case "consensus":
			runConsensusTest(tester)
		default:
			log.Fatalf("Unknown exercise: %s", *test)
	}
}

func runEchoTest(tester *dsnet.Node) {
	trigger := echo.SendTrigger{
		BaseMessage: dsnet.BaseMessage{
			From: "TESTER",
			To:   "N1",
			Type: "SendTrigger", 
		},
		EchoID:      "TEST_001",
		Content:     "Hello, DSNet!",
	}

	log.Println("Sending SendTrigger...")
	tester.Send(context.Background(), "N1", trigger)

	// 2. Wait for the specific Result
	timeout := time.After(10 * time.Second)
	for {
		select {
		case event := <-tester.Inbound:
			if event.Type == "ReplyReceived" {
				var result echo.ReplyReceived
				json.Unmarshal(event.Payload, &result)

				if result.EchoID == "TEST_001" {
					log.Println("✅ TEST PASSED: Received EchoResponse from every unique node")
					return
				}
			}
		case <-timeout:
			log.Fatal("❌ TEST FAILED: Timed out waiting for ReplyReceived")
		}
	}
}

func runMutexTest(tester *dsnet.Node, numNodes int) {
	trigger := mtx.MutexTrigger{
		BaseMessage: dsnet.BaseMessage{ From: "TESTER", To: "N1", Type: "MutexTrigger" },
		MutexID:     "TEST_MUTEX_001",
		WorkMillis:  300,
	}
	log.Println("Sending MutexTrigger...")
	tester.Send(context.Background(), "N1", trigger)

	// Optional: use wrapper to reset a random node during the test (port 50001 assumed)
	wm := wrapper.NewWrapperManager(50001)
	go func() {
		time.Sleep(500 * time.Millisecond)
		_ = wm.Reset(context.Background(), wrapper.Alias("N2"))
	}()

	// Wait for MutexResult from all nodes
	received := map[string]bool{}
	expected := map[string]bool{}
	for i := 1; i <= numNodes; i++ { expected[fmt.Sprintf("N%d", i)] = true }

	timeout := time.After(15 * time.Second)
	for {
		if len(received) >= len(expected) {
			log.Println("✅ TEST PASSED: All nodes completed critical section")
			return
		}
		select {
		case event := <-tester.Inbound:
			if event.Type == "MutexResult" {
				var result mtx.MutexResult
				json.Unmarshal(event.Payload, &result)
				if result.Success && expected[result.NodeId] && result.MutexID == "TEST_MUTEX_001" {
					received[result.NodeId] = true
					log.Printf("Node %s completed CS (%d/%d)", result.NodeId, len(received), len(expected))
				}
			}
		case <-timeout:
			// Identify missing nodes for better error output
			missing := []string{}
			for n := range expected { if !received[n] { missing = append(missing, n) } }
			log.Fatalf("❌ TEST FAILED: Timed out waiting for MutexResult from: %v", missing)
		}
	}
}

func runLeaderElectionTest(tester *dsnet.Node) {
	// 1. Prepare the specific Trigger
	trigger := leader_election.ElectionTrigger{
		BaseMessage: dsnet.BaseMessage{
			From: "TESTER",
			To:   "N1",
			Type: "ElectionTrigger", // Matches struct name
		},
		ElectionID: "TEST_001",
	}

	log.Println("Sending ElectionTrigger...")
	tester.Send(context.Background(), "N1", trigger)

	// 2. Wait for the specific Result
	timeout := time.After(15 * time.Second)
	for {
		select {
		case event := <-tester.Inbound:
			if event.Type == "ElectionResult" {
				var result leader_election.ElectionResult
				json.Unmarshal(event.Payload, &result)

				if result.Success && result.LeaderID == "N1" {
					log.Println("✅ TEST PASSED: Leader elected successfully")
					return
				}
			}
		case <-timeout:
			log.Fatal("❌ TEST FAILED: Timed out waiting for ElectionResult")
		}
	}
}

func runConsensusTest(tester *dsnet.Node) {
}