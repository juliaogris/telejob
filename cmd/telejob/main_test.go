package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/juliaogris/telejob/pkg/job"
	"github.com/juliaogris/telejob/pkg/pb"
	"github.com/juliaogris/telejob/pkg/telejob"
	"github.com/stretchr/testify/require"
)

//nolint:gochecknoglobals
var (
	readerCount = flag.Int("readers", 100, "number of concurrent log readers")
	address     = flag.String("address", "", "server address")
)

func TestMainSimple(t *testing.T) {
	ts := newTestServer(t)
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

func TestMainLogs(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	t.Setenv("TELEJOB_ADDRESS", ts.address)
	t.Setenv("TELEJOB_CLIENT_CERT", "testdata/client1.crt")
	t.Setenv("TELEJOB_CLIENT_KEY", "testdata/client1.key")
	t.Setenv("TELEJOB_SERVER_CA_CERT", "testdata/server-ca.crt")
	out, err := run(t, []string{"start", "echo", "hello"})
	require.NoError(t, err)
	id := strings.TrimSpace(out)
	out, err = run(t, []string{"logs", id})
	require.NoError(t, err)
	require.Equal(t, "hello\n", out)
}

func TestMainLogsStreamed(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	t.Setenv("TELEJOB_ADDRESS", ts.address)
	t.Setenv("TELEJOB_CLIENT_CERT", "testdata/client1.crt")
	t.Setenv("TELEJOB_CLIENT_KEY", "testdata/client1.key")
	t.Setenv("TELEJOB_SERVER_CA_CERT", "testdata/server-ca.crt")

	fname := filepath.Join(t.TempDir(), "logs")
	f, err := os.OpenFile(fname, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	out, err := run(t, []string{"start", "--", "tail", "-f", fname})
	require.NoError(t, err)
	id := strings.TrimSpace(out)
	str, err := start(t, []string{"logs", id})
	require.NoError(t, err)
	require.Equal(t, "", str.String())

	mustWrite(t, f, "1\n")
	wantLogs := "1\n"
	logsEqual := func() bool { return wantLogs == str.String() }
	require.Eventually(t, logsEqual, time.Second, 10*time.Millisecond, str.String())

	mustWrite(t, f, "2\n")
	wantLogs = "1\n2\n"
	require.Eventually(t, logsEqual, time.Second, 10*time.Millisecond, str.String())

	// Add new log reading client and cancel
	s, err := receiveOnce(ts.address, id)
	require.NoError(t, err)
	require.Equal(t, wantLogs, s)

	mustWrite(t, f, "3\n")
	wantLogs = "1\n2\n3\n"
	require.Eventually(t, logsEqual, time.Second, 10*time.Millisecond, str.String())

	_, err = run(t, []string{"stop", id})
	require.NoError(t, err)

	out, err = run(t, []string{"logs", id})
	require.NoError(t, err)
	require.Equal(t, wantLogs, out)
}

func TestMainManyLogsStreamed(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	t.Setenv("TELEJOB_ADDRESS", ts.address)
	t.Setenv("TELEJOB_CLIENT_CERT", "testdata/client1.crt")
	t.Setenv("TELEJOB_CLIENT_KEY", "testdata/client1.key")
	t.Setenv("TELEJOB_SERVER_CA_CERT", "testdata/server-ca.crt")

	fname := filepath.Join(t.TempDir(), "logs")
	f, err := os.OpenFile(fname, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	out, err := run(t, []string{"start", "--", "tail", "-f", fname})
	require.NoError(t, err)
	id := strings.TrimSpace(out)
	strs := make([]fmt.Stringer, *readerCount)
	wantLogs := ""

	for i := range strs {
		strs[i], err = start(t, []string{"logs", id})
		require.NoError(t, err)
		text := strconv.Itoa(i) + "\n"
		mustWrite(t, f, text)
		wantLogs += text
		s, err := receiveOnce(ts.address, id)
		require.NoError(t, err)
		wantOnceChunk := wantLogs[:min(len(wantLogs), telejob.LogChunkSize)]
		require.Equal(t, wantOnceChunk, s)
	}

	_, err = run(t, []string{"stop", id})
	require.NoError(t, err)
	stoppedFn := func() bool {
		out, err := run(t, []string{"status", id})
		require.NoError(t, err)
		return strings.Contains(out, "stopped")
	}
	require.Eventually(t, stoppedFn, 5*time.Second, 10*time.Millisecond)

	for _, str := range strs {
		fn := func() bool { return wantLogs == str.String() }
		require.Eventually(t, fn, 5*time.Second, 10*time.Millisecond, str.String())
	}
}

func mustWrite(t *testing.T, f *os.File, s string) {
	t.Helper()
	_, err := f.WriteString(s)
	require.NoError(t, err)
}

func receiveOnce(addr, id string) (string, error) {
	client, err := telejob.NewClient(addr, "testdata/client1.crt", "testdata/client1.key", "testdata/server-ca.crt")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.Logs(ctx, &pb.LogsRequest{Id: id})
	if err != nil {
		cancel()
		return "", err
	}
	resp, err := stream.Recv()
	cancel()
	if err != nil {
		return "", err
	}
	return string(resp.GetChunk()), nil
}

func run(t *testing.T, args []string) (string, error) {
	t.Helper()
	buf := &bytes.Buffer{}
	kctx, err := setupRun(t, args, buf)
	if err != nil {
		return "", err
	}
	err = kctx.Run()
	if err != nil {
		return "", fmt.Errorf("kong.Context.Run: %w", err)
	}
	return buf.String(), nil
}

func start(t *testing.T, args []string) (fmt.Stringer, error) {
	t.Helper()
	syncBuf := &syncBuf{}
	kctx, err := setupRun(t, args, syncBuf)
	if err != nil {
		return nil, err
	}
	go func() {
		err := kctx.Run()
		if err != nil {
			t.Errorf("kong.Context.Run: %v", err)
		}
	}()
	return syncBuf, nil
}

func setupRun(t *testing.T, args []string, w io.Writer) (*kong.Context, error) {
	t.Helper()

	opts := []kong.Option{
		kong.Exit(exitFatalFn(t)),
		kong.Bind(&w),
	}
	parser, err := kong.New(&app{}, opts...)
	if err != nil {
		return nil, fmt.Errorf("kong.New: %w", err)
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		return nil, fmt.Errorf("kong.Parser.Parse: %w", err)
	}
	return kctx, nil
}

type syncBuf struct {
	mutex sync.Mutex
	data  []byte
}

func (sb *syncBuf) Write(b []byte) (int, error) {
	sb.mutex.Lock()
	defer sb.mutex.Unlock()
	sb.data = append(sb.data, b...)
	return len(b), nil
}

func (sb *syncBuf) String() string {
	sb.mutex.Lock()
	defer sb.mutex.Unlock()
	return string(sb.data)
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

func (ts *testServer) Stop() {
	if ts.Server != nil {
		ts.Server.Stop()
	}
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	opts := []job.Option{
		job.WithCgroup(fmt.Sprintf("/sys/fs/cgroup/telejob-%d", rand.Uint64())),
	}
	if *address != "" {
		return &testServer{address: *address}
	}
	server, err := telejob.NewServer("testdata/server.crt", "testdata/server.key", "testdata/client-ca.crt", opts...)
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	go func() {
		if err := server.Serve(lis); err != nil {
			t.Errorf("cannot start test server %v", err)
		}
	}()
	return &testServer{Server: server, address: lis.Addr().String()}
}
