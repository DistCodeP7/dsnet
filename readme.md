# DSNet Client and Controller
This repository contains the implementation of a client and controller for DSNet, a distributed networking solution. DSNet allows multiple nodes to communicate through the controller via gRPC. The client side aims to provide a simple interface for users to interact with connect to other nodes in the DSNet network without traditional 'net' package. The controller will be ran as a part of a submission and will be the central point of communication for all clients and will serve as the entry point for the evaluation engine.

## Update proto file
1. Make sure you have installed the protobuf compiler `protoc`. You can download it from the [official Protocol Buffers GitHub repository](https://github.com/protocolbuffers/protobuf/releases).
2. Install the Go plugins for Protocol Buffers if you haven't already: 

```bash
go get google.golang.org/protobuf/cmd/protoc-gen-go
go get google.golang.org/grpc/cmd/protoc-gen-go-grpc
```

3. Then run the following command in the root directory of the project:

```bash
protoc   --go_out=proto --go_opt=paths=source_relative   --go-grpc_out=proto --go-grpc_opt=paths=source_relative   dsnet.proto
```

## Dockerfile
The Dockerfile in the repository is used to create a Docker image for workers, thus the client library will be availible for the worker to use. 