// a CLI command to serve as a gRPC provider of awm-relayer/proto/decider

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"

	pb "github.com/ava-labs/awm-relayer/proto/pb/decider"
	"google.golang.org/grpc"
)

var port = flag.Int("port", 50051, "The server port")

type deciderServer struct {
	pb.UnimplementedDeciderServiceServer
}

func (s *deciderServer) ShouldSendMessage(
	ctx context.Context,
	msg *pb.ShouldSendMessageRequest,
) (*pb.ShouldSendMessageResponse, error) {
	return &pb.ShouldSendMessageResponse{
		ShouldSendMessage: true,
	}, nil
}

func main() {
	flag.Parse()

	server := grpc.NewServer()

	pb.RegisterDeciderServiceServer(server, &deciderServer{})

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("decider failed to listen: %v", err)
	}

	log.Printf("decider listening at %v", listener.Addr())

	err = server.Serve(listener)
	if err != nil {
		log.Fatalf("decider failed to serve: %v", err)
	}
}
