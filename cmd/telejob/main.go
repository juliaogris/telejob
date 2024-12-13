// Telejob is the client CLI to run jobs remotely in a restricted environment.
//
// It communicates with a Telejob server over gRPC. The CLI supports the
// following commands:
//
//   - start: starts a new job.
//   - stop: stops a running job.
//   - status: retrieves the status of a job.
//   - logs: stream logs of a job.
//
// Each command requires the address of the Telejob server and the client's
// certificate and key for mTLS authentication. The server's CA certificate
// can also be provided if it's not available as part of the system's trust
// store.
//
// The CLI optionally uses environment variables to configure the server address
// and certificate paths. The following environment variables are supported:
//
//   - TELEJOB_ADDRESS: the address of the Telejob server.
//   - TELEJOB_CLIENT_CERT: the path to the client's certificate file.
//   - TELEJOB_CLIENT_KEY: the path to the client's key file.
//   - TELEJOB_SERVER_CA_CERT: the path to the server's CA certificate file.
//
// Example usage after environment setup:
//
//		telejob start sleep 100
//		telejob stop <job_id>
//		telejob status <job_id>
//		telejob logs <job_id>
//	    telejob [COMMAND] --help
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/alecthomas/kong"
	"github.com/juliaogris/telejob/pkg/job"
	"github.com/juliaogris/telejob/pkg/pb"
	"github.com/juliaogris/telejob/pkg/telejob"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const description = "Telejob is a client CLI to run jobs remotely in a restricted environment."

type app struct {
	Start  startCmd  `cmd:"" help:"Start a new job."`
	Stop   stopCmd   `cmd:"" help:"Stop the job with given ID."`
	Status statusCmd `cmd:"" help:"Status the job with given ID."`
	Logs   logsCmd   `cmd:"" help:"Print logs of the job with given ID. Continuously stream additional output."`
}

func main() {
	var writer io.Writer = os.Stdout
	opts := []kong.Option{
		kong.Bind(&writer),
		kong.Description(description),
		kong.ConfigureHelp(kong.HelpOptions{Compact: true}),
	}
	kctx := kong.Parse(&app{}, opts...)
	kctx.FatalIfErrorf(kctx.Run())
}

type startCmd struct {
	cmd
	Command string   `arg:"" required:"" help:"Command."`
	Args    []string `arg:"" optional:"" help:"Command arguments."`
}

type stopCmd struct {
	cmd
	ID string `arg:"" required:"" help:"Job ID."`
}

type statusCmd struct {
	cmd
	ID         string `arg:"" required:"" help:"Job ID, use 'list' to find IDs."`
	TimeFormat string `short:"t" help:"Time format." default:"2006-01-02T15:04:05Z07:00" env:"TELEJOB_TIME_FORMAT"`
}

type logsCmd struct {
	cmd
	ID string `arg:"" required:"" help:"Job ID."`
}

type cmd struct {
	Address      string `required:"" short:"A" help:"Server address." env:"TELEJOB_ADDRESS"`
	ClientCert   string `required:"" help:"Client Certificate file." env:"TELEJOB_CLIENT_CERT"`
	ClientKey    string `required:"" help:"Client Private Key file." env:"TELEJOB_CLIENT_KEY"`
	ServerCACert string `help:"Server CA certificate file." env:"TELEJOB_SERVER_CA_CERT"`

	client *telejob.Client
	w      io.Writer // can be overridden for testing
}

// Run is called by [kong] when the CLI arguments contain the `start` command.
func (c *startCmd) Run() error {
	req := &pb.StartRequest{
		Command:   c.Command,
		Arguments: c.Args,
	}
	resp, err := c.client.Start(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to start job: %w", err)
	}
	_, err = fmt.Fprintln(c.w, resp.GetId())
	if err != nil {
		return fmt.Errorf("failed to write job ID %q: %w", resp.GetId(), err)
	}
	return nil
}

// Run is called by [kong] when the CLI arguments contain the `stop` command.
func (c *stopCmd) Run() error {
	req := &pb.StopRequest{Id: c.ID}
	_, err := c.client.Stop(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to stop job: %w", err)
	}
	return nil
}

// Run is called by [kong] when the CLI arguments contain the `status` command.
func (c *statusCmd) Run() error {
	req := &pb.StatusRequest{Id: c.ID}
	resp, err := c.client.Status(context.Background(), req)
	if err != nil {
		return fmt.Errorf("failed to get job status: %w", err)
	}
	return printJobStatus(c.w, resp.GetJobStatus(), c.TimeFormat)
}

// Run is called by [kong] when the CLI arguments contain the `logs` command.
func (c *logsCmd) Run() error {
	req := &pb.LogsRequest{Id: c.ID}
	stream, err := c.client.Logs(context.Background(), req)
	if err != nil {
		return fmt.Errorf("cannot open job logs stream: %w", err)
	}
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil // stream closed,
		}
		if err != nil {
			return fmt.Errorf("failed to get job logs from stream: %w", err)
		}
		if _, err := c.w.Write(resp.GetChunk()); err != nil {
			return fmt.Errorf("failed to print logs: %w ", err)
		}
	}
}

// AfterApply is called by [kong] immediately after flag validation and
// assignment and _before_ a command's Run method. It is useful for setting up
// common resources like gRPC connections.
//
// The pointer to the io.Writer is required to keep the io.Writer type when
// passing through an `any` parameter on the [kong.Bind] function.
func (c *cmd) AfterApply(w *io.Writer) error {
	c.w = cmp.Or(*w, io.Writer(os.Stdout))
	client, err := telejob.NewClient(c.Address, c.ClientCert, c.ClientKey, c.ServerCACert)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	c.client = client
	return nil
}

// AfterRun is called by [kong] immediately after a command's Run method
// completes. It is useful for cleaning up common resources like gRPC
// connections.
func (c *cmd) AfterRun() error {
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("after run: %w", err)
	}
	return nil
}

// printJobStatus writes the job status to the provided writer in a tabular
// format.
func printJobStatus(w io.Writer, j *pb.JobStatus, layout string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, err := fmt.Fprintln(tw, "ID\tCOMMAND\tSTATE\tSTARTED\tSTOPPED\tEXIT")
	if err != nil {
		return fmt.Errorf("cannot write job status header: %w", err)
	}
	state := stateString(j.GetState())
	started := pbTimeString(j.GetStarted(), layout)
	stopped := pbTimeString(j.GetStopped(), layout)
	cs := append([]string{j.GetCommand()}, j.GetArguments()...)
	command := strings.Join(cs, " ") // Consider proper shell quoting, not trivial.
	exitCode := exitCodeString(j.GetExitCode())
	_, err = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", j.GetId(), command, state, started, stopped, exitCode)
	if err != nil {
		return fmt.Errorf("cannot write job status content: %w", err)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("cannot flush job status tab writer: %w", err)
	}
	return nil
}

// stateString converts a pb.State to a human-readable string.
func stateString(s pb.State) string {
	switch s {
	case pb.State_STATE_RUNNING:
		return "running"
	case pb.State_STATE_STOPPED:
		return "stopped"
	case pb.State_STATE_UNSPECIFIED:
		return "State_STATE_UNSPECIFIED"
	default:
		return s.String()
	}
}

// pbTimeString converts a [timestamppb.Timestamp] to a string formatted
// according to the provided layout. If the timestamp is zero, it returns an
// empty string.
func pbTimeString(t *timestamppb.Timestamp, layout string) string {
	if t.GetSeconds() == 0 && t.GetNanos() == 0 {
		return ""
	}
	return t.AsTime().Local().Format(layout) //nolint:gosmopolitan // usage of time.Local in local client CLI makes timestamps more readable.
}

// exitCodeString converts an exit code to a string. It handles special cases
// for non-terminated jobs and jobs terminated by a signal.
func exitCodeString(e int64) string {
	if e == job.NotTerminated {
		return ""
	}
	if e == job.TerminatedBySignal {
		return "signal"
	}
	return strconv.FormatInt(e, 10)
}
