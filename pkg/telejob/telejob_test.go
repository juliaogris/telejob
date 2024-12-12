package telejob_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/juliaogris/telejob/pkg/job"
	"github.com/juliaogris/telejob/pkg/pb"
	"github.com/juliaogris/telejob/pkg/telejob"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	crt1     = "testdata/client1.crt"
	key1     = "testdata/client1.key"
	crt2     = "testdata/client2.crt"
	key2     = "testdata/client2.key"
	clientCA = "testdata/client-ca.crt"

	serverCrt = "testdata/server.crt"
	serverKey = "testdata/server.key"
	serverCA  = "testdata/server-ca.crt"

	badCrt1     = "testdata/client2.crt"
	badServerCA = "testdata/client-ca.crt"
	badClientCA = "testdata/server-ca.crt"

	noIPCrt = "testdata/client-no-ip.crt"
	noIPKey = "testdata/client-no-ip.key"

	noIPServerCrt = "testdata/server-no-ip.crt"
	noIPServerKey = "testdata/server-no-ip.key"
)

func TestServiceSimple(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, serverCrt, serverKey, clientCA)
	defer ts.Stop()

	client, err := telejob.NewClient(ts.Address(), crt1, key1, serverCA)
	require.NoError(t, err)

	ctx := context.Background()
	startReq := &pb.StartRequest{
		Command:   "sleep",
		Arguments: []string{"100"},
	}
	startResp, err := client.Start(ctx, startReq)
	require.NoError(t, err)

	id := startResp.GetId()
	statusResp, err := client.Status(ctx, &pb.StatusRequest{Id: id})
	require.NoError(t, err)
	statusWant := job.Status{
		ID:       id,
		Command:  "sleep",
		Args:     []string{"100"},
		Running:  true,
		Started:  statusResp.GetJobStatus().GetStarted().AsTime(),
		ExitCode: job.NotTerminated,
	}
	require.Equal(t, statusWant, statusFromPB(statusResp.GetJobStatus()))

	_, err = client.Stop(ctx, &pb.StopRequest{Id: id})
	require.NoError(t, err)

	statusResp, err = client.Status(ctx, &pb.StatusRequest{Id: id})
	require.NoError(t, err)

	fn := func() bool {
		statusResp, err = client.Status(ctx, &pb.StatusRequest{Id: id})
		require.NoError(t, err)
		statusWant := job.Status{
			ID:       id,
			Command:  "sleep",
			Args:     []string{"100"},
			Running:  false,
			Started:  statusResp.GetJobStatus().GetStarted().AsTime(),
			Stopped:  statusResp.GetJobStatus().GetStopped().AsTime(),
			ExitCode: job.TerminatedBySignal,
		}
		return reflect.DeepEqual(statusWant, statusFromPB(statusResp.GetJobStatus()))
	}
	require.Eventually(t, fn, time.Second, 10*time.Millisecond, statusFromPB(statusResp.GetJobStatus()))
}

func TestServiceNotFound(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, serverCrt, serverKey, clientCA)
	defer ts.Stop()

	client1, err := telejob.NewClient(ts.Address(), crt1, key1, serverCA)
	require.NoError(t, err)

	ctx := context.Background()
	_, err = client1.Status(ctx, &pb.StatusRequest{Id: "NON-EXISTENT-ID"})
	require.Error(t, err)
	s, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.NotFound, s.Code())
}

func TestServiceOwnership(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, serverCrt, serverKey, clientCA)
	defer ts.Stop()

	client1, err := telejob.NewClient(ts.Address(), crt1, key1, serverCA)
	require.NoError(t, err)

	ctx := context.Background()
	startResp, err := client1.Start(ctx, &pb.StartRequest{Command: "true"})
	require.NoError(t, err)
	id1 := startResp.GetId()
	_, err = client1.Status(ctx, &pb.StatusRequest{Id: id1})
	require.NoError(t, err)

	client2, err := telejob.NewClient(ts.Address(), crt2, key2, serverCA)
	require.NoError(t, err)
	startResp, err = client2.Start(ctx, &pb.StartRequest{Command: "true"})
	require.NoError(t, err)
	id2 := startResp.GetId()
	_, err = client2.Status(ctx, &pb.StatusRequest{Id: id2})
	require.NoError(t, err)

	// client1 accessing client2's job
	_, err = client1.Status(ctx, &pb.StatusRequest{Id: id2})
	require.Error(t, err)
	s, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, s.Code())

	// client2 accessing client1's job
	_, err = client2.Status(ctx, &pb.StatusRequest{Id: id1})
	require.Error(t, err)
	s, ok = status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.PermissionDenied, s.Code())
}

func statusFromPB(js *pb.JobStatus) job.Status {
	return job.Status{
		ID:       js.GetId(),
		Command:  js.GetCommand(),
		Args:     js.GetArguments(),
		Running:  js.GetState() == pb.State_STATE_RUNNING,
		Started:  timeFromPB(js.GetStarted()),
		Stopped:  timeFromPB(js.GetStopped()),
		ExitCode: int(js.GetExitCode()),
	}
}

func timeFromPB(t *timestamppb.Timestamp) time.Time {
	if t.GetSeconds() == 0 && t.GetNanos() == 0 {
		return time.Time{} // this is not the zeroValue to timestamppb.Timestamp
	}
	return t.AsTime()
}

func newTestServer(t *testing.T, serverCrt, serverKey, clientCA string) *telejob.Server {
	t.Helper()
	opts := []job.Option{
		//nolint:gosec // G404: Use of weak random number generator
		job.WithCgroup(fmt.Sprintf("/sys/fs/cgroup/telejob-%d", rand.Uint64())),
	}
	server, err := telejob.NewServer("127.0.0.1:0", serverCrt, serverKey, clientCA, opts...)
	require.NoError(t, err)
	go func() {
		if err := server.Serve(); err != nil {
			t.Errorf("cannot start test server %v", err)
		}
	}()
	return server
}
