package telejob

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/juliaogris/telejob/pkg/job"
	"github.com/juliaogris/telejob/pkg/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Sentinel Errors returned by the telejob package.
var (
	ErrCredentials = errors.New("credentials setup error")
	ErrCertLoad    = errors.New("certificate load error")
	ErrCASetup     = errors.New("CA setup error")
	ErrCommonName  = errors.New("failed to extract Common Name")
	ErrClientConn  = errors.New("client connection error")
)

// Client is a wrapper around the generated gRPC client for the Telejob service.
// It provides a convenient way to interact with the Telejob server
// establishing and closing secure connections.
type Client struct {
	pb.TelejobClient
	conn *grpc.ClientConn
}

// Server is a wrapper around the gRPC server for the Telejob service.
// It provides methods for starting and stopping the server,
// as well as managing the underlying job controller.
type Server struct {
	*grpc.Server
	controller *job.Controller
}

// NewClient creates a new Telejob client and establishes a connection to the
// server at the specified address. It uses the provided client certificate and
// key for mTLS authentication. It optionally uses the provided server CA
// certificate, if it's not available as part of the root certificates.
//
// If there is an error establishing the connection or setting up the TLS
// configuration, an error is returned.
func NewClient(address, clientCert, clientKey, serverCA string) (*Client, error) {
	tlsConfig, err := clientTLSConfig(clientCert, clientKey, serverCA)
	if err != nil {
		return nil, fmt.Errorf("ConnectClient: %w: %w", ErrCredentials, err)
	}
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	}
	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return nil, fmt.Errorf("ConnectClient: address %q: %w", address, err)
	}
	return &Client{
		TelejobClient: pb.NewTelejobClient(conn),
		conn:          conn,
	}, nil
}

// Close closes the client's connection to the server.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("%w: cannot close: %w", ErrClientConn, err)
	}
	return nil
}

// NewServer creates a new Telejob server.
//
// It listens on the specified address, configures mTLS using the provided
// server certificate, server key, and client CA certificate for mTLS
// authentication, and initializes a job controller with the given options.
//
// If there is an error listening on the address, setting up the TLS
// configuration, or creating the job controller, an error is returned.
func NewServer(serverCert, serverKey, clientCA string, jobOpts ...job.Option) (*Server, error) {
	tlsConfig, err := serverTLSConfig(serverCert, serverKey, clientCA)
	if err != nil {
		return nil, fmt.Errorf("NewServer: %w: %w", ErrCredentials, err)
	}
	controller, err := job.NewController(jobOpts...)
	if err != nil {
		return nil, fmt.Errorf("NewServer: %w", err)
	}
	gropOpts := []grpc.ServerOption{
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.UnaryInterceptor(unaryInterceptorCN),
		grpc.StreamInterceptor(streamInterceptorCN),
	}
	grpcServer := grpc.NewServer(gropOpts...)
	service := &Service{Controller: controller}
	pb.RegisterTelejobServer(grpcServer, service)
	return &Server{
		Server:     grpcServer,
		controller: controller,
	}, nil
}

// Stop stops the server ungracefully and shuts down the job controller.
// Useful for tests, especially within a defer statement.
func (s *Server) Stop() {
	if err := s.controller.StopAll(); err != nil {
		slog.Error("failed to close job controller:", "err", err)
	}
	s.Server.Stop()
}

// StopOnSignals registers signal handlers to gracefully stop the server
// and shut down the job controller when specified signals are received.
// If no signals are provided, this function does nothing.
func (s *Server) StopOnSignals(sig ...os.Signal) {
	if len(sig) == 0 {
		return
	}
	go handleSignals(s.Server, s.controller, sig...)
}

// handleSignals receives signals and gracefully stops the server and job
// controller. It is intended to be run in a separate goroutine.
func handleSignals(grpcServer *grpc.Server, controller *job.Controller, sig ...os.Signal) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig...)
	<-ch
	slog.Info("stopping server")
	if err := controller.StopAll(); err != nil {
		slog.Error("failed to close job controller:", "err", err)
	}
	go grpcServer.GracefulStop()
	time.Sleep(2 * time.Second) // grace period
	grpcServer.Stop()
}
