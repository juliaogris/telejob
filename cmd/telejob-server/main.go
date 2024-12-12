// Telejob-server is a gRPC server that runs and manages jobs in a restricted
// environment.
//
// The server can be configured with the following options:
//
//   - `--address`: The address to listen on.
//   - `--server-cert`: The path to the server's certificate file.
//   - `--server-key`: The path to the server's key file.
//   - `--client-ca-cert`: The path to the client CA certificate file.
//   - `--cpu-limit`: The number of CPUs per job.
//   - `--memory-limit`: The memory limit in KiB per job.
//   - `--io-limit`: The I/O limit per job. ex: 252:1 rbps=1000000
//
// The server can also be configured using environment variables:
//
//   - TELEJOB_ADDRESS: The address to listen on.
//   - TELEJOB_SERVER_CERT: The path to the server's certificate file.
//   - TELEJOB_SERVER_KEY: The path to the server's key file.
//   - TELEJOB_CLIENT_CA_CERT: The path to the client CA certificate file.
//
// Sample usage after environment setup:
//
//	telejob-server --cpu-limit 0.5 --memory-limit 2000
package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/alecthomas/kong"
	"github.com/juliaogris/telejob/pkg/job"
	"github.com/juliaogris/telejob/pkg/telejob"
)

const description = "Telejob-server is a gRPC server that runs and manages jobs in a restricted environment."

type app struct {
	Address      string `required:"" short:"A" help:"Address to listen on." env:"TELEJOB_ADDRESS"`
	ServerCert   string `required:"" help:"Server certificate file." env:"TELEJOB_SERVER_CERT"`
	ServerKey    string `required:"" help:"Server private key file." env:"TELEJOB_SERVER_KEY"`
	ClientCACert string `required:"" help:"Client CA certificate file." env:"TELEJOB_CLIENT_CA_CERT"`

	CPULimit    float64  `short:"c" help:"Number of CPUs per job."`
	MemoryLimit uint64   `short:"m" help:"Memory limit in KiB per job."`
	IOLimit     []string `short:"i" help:"I/O Limit per job, ex.: \"252:1 rbps=1000000\"."`
}

func main() {
	opts := []kong.Option{kong.Description(description)}
	kctx := kong.Parse(&app{}, opts...)
	kctx.FatalIfErrorf(kctx.Run())
}

// Run is called by [kong] after flags have been validated and parsed.
func (a *app) Run() error {
	opts := []job.Option{
		job.WithLimits(job.Limits{CPUs: a.CPULimit, MemoryKiB: a.MemoryLimit, IO: a.IOLimit}),
	}
	server, err := telejob.NewServer(a.ServerCert, a.ServerKey, a.ClientCACert, opts...)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}
	server.StopOnSignals(os.Interrupt)
	lis, err := net.Listen("tcp", a.Address)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	slog.Info("starting server", "address", lis.Addr().String())
	if err := server.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}
	return nil
}
