package telejob_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"testing"

	"github.com/juliaogris/telejob/pkg/job"
	"github.com/juliaogris/telejob/pkg/pb"
	"github.com/juliaogris/telejob/pkg/telejob"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestServiceDirectly(t *testing.T) {
	t.Parallel()
	controller := newTestController(t)
	defer func() { require.NoError(t, controller.StopAll()) }()
	service := &telejob.Service{Controller: controller}
	ctx := context.WithValue(context.Background(), telejob.OwnerKey{}, "test-owner")
	startResp, err := service.Start(ctx, &pb.StartRequest{Command: "true"})
	require.NoError(t, err)
	id := startResp.GetId()
	statusResp, err := service.Status(ctx, &pb.StatusRequest{Id: id})
	require.NoError(t, err)
	require.Equal(t, id, statusResp.GetJobStatus().GetId())
	require.Equal(t, "true", statusResp.GetJobStatus().GetCommand())
}

func TestServiceWithCustomServer(t *testing.T) {
	t.Parallel()
	controller := newTestController(t)
	defer func() { require.NoError(t, controller.StopAll()) }()
	service := &telejob.Service{Controller: controller}
	opts := []grpc.ServerOption{
		grpc.Creds(insecure.NewCredentials()),
		grpc.UnaryInterceptor(unaryTestInterceptor),
	}
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterTelejobServer(grpcServer, service)
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			t.Errorf("serve error: %v", err)
		}
	}()
	defer grpcServer.Stop()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close()) }()
	client := pb.NewTelejobClient(conn)
	ctx := context.WithValue(context.Background(), telejob.OwnerKey{}, "test-owner")
	startResp, err := client.Start(ctx, &pb.StartRequest{Command: "true"})
	require.NoError(t, err)
	id := startResp.GetId()
	statusResp, err := service.Status(ctx, &pb.StatusRequest{Id: id})
	require.NoError(t, err)
	require.Equal(t, id, statusResp.GetJobStatus().GetId())
	require.Equal(t, "true", statusResp.GetJobStatus().GetCommand())
}

func newTestController(t *testing.T) *job.Controller {
	t.Helper()
	opts := []job.Option{
		//nolint:gosec // G404: Use of weak random number generator
		job.WithCgroup(fmt.Sprintf("/sys/fs/cgroup/telejob-%d", rand.Uint64())),
	}
	controller, err := job.NewController(opts...)
	require.NoError(t, err)
	return controller
}

func unaryTestInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx = context.WithValue(ctx, telejob.OwnerKey{}, "test-owner")
	return handler(ctx, req)
}
