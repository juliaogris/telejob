package main

import (
	"bytes"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/juliaogris/telejob/pkg/job"
	"github.com/juliaogris/telejob/pkg/telejob"
	"github.com/stretchr/testify/require"
)

func TestMainSimple(t *testing.T) {
	ts := newTestServer(t, "testdata/client-ca.crt")
	defer ts.Stop()
	t.Setenv("TELEJOB_ADDRESS", ts.address)
	t.Setenv("TELEJOB_CLIENT_CERT", "testdata/client1.crt")
	t.Setenv("TELEJOB_CLIENT_KEY", "testdata/client1.key")
	t.Setenv("TELEJOB_SERVER_CA_CERT", "testdata/server-ca.crt")

	out, err := run(t, []string{"start", "true"})
	require.NoError(t, err)
	id := strings.TrimSpace(out)

	out, err = run(t, []string{"status", id})
	require.NoError(t, err)
	lines := strings.Split(out, "\n")
	require.Len(t, lines, 3)
	require.Contains(t, lines[0], "ID") // header
	require.Contains(t, lines[1], id)   // content
	require.Equal(t, "", lines[2])

	out, err = run(t, []string{"start", "sleep", "100"})
	require.NoError(t, err)
	id = strings.TrimSpace(out)

	out, err = run(t, []string{"status", id, "--time-format", "15:04:05"})
	require.NoError(t, err)
	lines = strings.Split(out, "\n")
	require.Len(t, lines, 3)

	// sample output
	// ID  COMMAND    STATE    STARTED   STOPPED  EXIT
	// 2   sleep 100  running  19:11:28
	//
	// these test cases are fragile, let's keep them to a minimum
	require.Equal(t, "ID  COMMAND    STATE    STARTED   STOPPED  EXIT", lines[0])
	require.Regexp(t, `^2   sleep 100  running\s* \d\d:\d\d:\d\d\s*$`, lines[1])
	require.Equal(t, "", lines[2])

	out, err = run(t, []string{"stop", id})
	require.NoError(t, err)
	require.Equal(t, "", out)
}

func run(t *testing.T, args []string) (string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	var w io.Writer = buf
	opts := []kong.Option{
		kong.Exit(exitFatalFn(t)),
		kong.Bind(&w),
	}
	parser, err := kong.New(&app{}, opts...)
	if err != nil {
		return "", fmt.Errorf("kong.New: %w", err)
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		return "", fmt.Errorf("kong.Parser.Parse: %w", err)
	}
	err = kctx.Run()
	if err != nil {
		return "", fmt.Errorf("kong.Context.Run: %w", err)
	}
	return buf.String(), nil
}

func exitFatalFn(t *testing.T) func(c int) {
	t.Helper()
	return func(_ int) {
		t.Helper()
		t.Fatalf("unexpected exit by arg parser")
	}
}

type testServer struct {
	*telejob.Server
	address string
}

func newTestServer(t *testing.T, clientCA string) *testServer {
	t.Helper()
	opts := []job.Option{
		//nolint:gosec // G404: Use of weak random number generator
		job.WithCgroup(fmt.Sprintf("/sys/fs/cgroup/telejob-%d", rand.Uint64())),
	}
	server, err := telejob.NewServer("testdata/server.crt", "testdata/server.key", clientCA, opts...)
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("cannot start test server %v", err)
		}
	}()
	return &testServer{Server: server, address: lis.Addr().String()}
}
