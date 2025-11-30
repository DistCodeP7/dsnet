package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"time"

	"github.com/distcodep7/dsnet/dsnet"
	"github.com/distcodep7/dsnet/exercises/echo"
	"github.com/distcodep7/dsnet/exercises/leader_election"
	"github.com/distcodep7/dsnet/solutions/echo_broadcast"
	"github.com/distcodep7/dsnet/solutions/echo_two_nodes"
	"github.com/distcodep7/dsnet/solutions/paxos_leader_election"
	"github.com/distcodep7/dsnet/testing/controller"
)

func main() {
	test := flag.String("e", "echo", "Exercise to test: Echo, Mutex, LeaderElection, Consensus")
	numNodes := flag.Int("n", 2, "Number of nodes in the system")
	flag.Parse()

	go controller.Serve(controller.TestConfig{})

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
			runMutexTest(tester)
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

func runMutexTest(tester *dsnet.Node) {

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