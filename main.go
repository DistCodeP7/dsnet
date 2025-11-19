package main

import (
	"log"
	"net"

	"github.com/distcodep7/dsnet/controller"
	pb "github.com/distcodep7/dsnet/proto"
	"google.golang.org/grpc"
)

func main() {
	ctrl := controller.NewController(controller.ControllerProps{
		Logger: log.Default(), // Use the standard library logger for production
	})

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal(err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterNetworkControllerServer(grpcServer, ctrl)

	log.Println("DSNet controller ready")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
