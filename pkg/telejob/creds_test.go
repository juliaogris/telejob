package telejob_test

import (
	"context"
	"testing"

	"github.com/juliaogris/telejob/pkg/pb"
	"github.com/juliaogris/telejob/pkg/telejob"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestCredsBadClientCA(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, serverCrt, serverKey, badClientCA)
	defer ts.Stop()

	client, err := telejob.NewClient(ts.address, crt1, key1, serverCA)
	require.NoError(t, err)

	_, err = client.Start(context.Background(), &pb.StartRequest{Command: "true"})
	s, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unavailable, s.Code())
}

func TestCredsServerCertNoIP(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, noIPServerCrt, noIPServerKey, clientCA)
	defer ts.Stop()

	client, err := telejob.NewClient(ts.address, crt1, key1, serverCA)
	require.NoError(t, err)

	_, err = client.Start(context.Background(), &pb.StartRequest{Command: "true"})
	require.Error(t, err)
	s, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unavailable, s.Code())
	// fragile condition, let's keep it to a minimum
	require.Contains(t, s.Message(), "doesn't contain any IP SANs")
}

func TestCredsBadClient(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, serverCrt, serverKey, clientCA)
	defer ts.Stop()

	client, err := telejob.NewClient(ts.address, crt1, key1, badServerCA)
	require.NoError(t, err)
	_, err = client.Start(context.Background(), &pb.StartRequest{Command: "true"})
	s, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.Unavailable, s.Code())

	_, err = telejob.NewClient(ts.address, badCrt1, key1, serverCA)
	require.Error(t, err)
	require.ErrorIs(t, err, telejob.ErrCredentials)

	_, err = telejob.NewClient(ts.address, badCrt1, key1, serverCA)
	require.Error(t, err)
	require.ErrorIs(t, err, telejob.ErrCredentials)

	// happy case: no IP required for client, ensuring no server config error
	client, err = telejob.NewClient(ts.address, noIPCrt, noIPKey, serverCA)
	require.NoError(t, err)
	_, err = client.Start(context.Background(), &pb.StartRequest{Command: "true"})
	require.NoError(t, err)
}

func TestCredsPlainText(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, serverCrt, serverKey, clientCA)
	defer ts.Stop()

	conn, err := grpc.NewClient(ts.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	rawClient := pb.NewTelejobClient(conn)
	_, err = rawClient.Start(context.Background(), &pb.StartRequest{Command: "true"})
	require.Error(t, err)
	require.Equal(t, codes.Unavailable, status.Convert(err).Code())

	// happy case, ensuring no server config error
	client, err := telejob.NewClient(ts.address, crt1, key1, serverCA)
	require.NoError(t, err)
	_, err = client.Start(context.Background(), &pb.StartRequest{Command: "true"})
	require.NoError(t, err)
}
