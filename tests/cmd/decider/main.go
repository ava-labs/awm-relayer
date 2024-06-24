// a CLI command to serve as a gRPC provider of awm-relayer/proto/decider

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"

	pb "github.com/ava-labs/awm-relayer/proto/pb/decider"
	pbMock "github.com/ava-labs/awm-relayer/proto/pb/mock_decider"
	emptypb "github.com/golang/protobuf/ptypes/empty"
	"google.golang.org/grpc"
)

var port = flag.Int("port", 50051, "The server port")

type mockDecider struct {
	pb.UnimplementedDeciderServer
	pbMock.UnimplementedMockDeciderServer

	shouldSendMessageResponse bool
}

func (s *mockDecider) ShouldSendMessage(
	ctx context.Context,
	msg *pb.UnsignedWarpMessage,
) (*pb.ShouldSendMessageResponse, error) {
	return &pb.ShouldSendMessageResponse{
		ShouldSendMessage: s.shouldSendMessageResponse,
	}, nil
}

func (s *mockDecider) SetShouldSendMessageResponse(
	ctx context.Context,
	msg *pbMock.SetShouldSendMessageResponseRequest,
) (*emptypb.Empty, error) {
	s.shouldSendMessageResponse = msg.ShouldSendMessageResponse
	return &emptypb.Empty{}, nil
}

func main() {
	flag.Parse()
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("decider failed to listen: %v", err)
	}

	server := grpc.NewServer()

	decider := mockDecider{shouldSendMessageResponse: true}

	pb.RegisterDeciderServer(server, &decider)

	pbMock.RegisterMockDeciderServer(server, &decider)

	err = server.Serve(listener)
	if err != nil {
		log.Fatalf("decider failed to serve: %v", err)
	}

	log.Printf("decider listening at %v", listener.Addr())
}
