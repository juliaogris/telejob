// Package telejob provides a gRPC service for managing jobs.
//
// It allows clients to start, stop, and monitor the status of jobs running on a
// server. The communication between the client and server is secured using
// mutual TLS (mTLS) authentication.
//
// ## Client
//
// The [Client] created with [NewClient] provides a convenient way to
// interact with the Telejob server. It establishes and closes secure
// connections using mTLS.
//
// ## Server
//
// The [Server] created with [NewServer] manages the gRPC server and the
// underlying job controller. It provides methods for starting and stopping the
// server, as well as managing jobs.
//
// ## Security
//
// The package enforces mTLS authentication for all communication between the
// client and server. It uses TLS version 1.3 and requires valid certificates
// for both the client and server.
//
// ## Service
//
// The Service implements the generated gRPC interface pb.TelejobServer. It
// requires that the [job.Controller] is initialized and that job owners are
// passed via the context using the [OwnerKey]. It is a lower integration point
// than the [Server] type for custom security setup or testing.
//
// # Example Usage
//
// Client:
//
//	client, err := NewClient("localhost:8443", "client.crt", "client.key", "server-ca.crt")
//	if err != nil {
//		// handle error
//	}
//	defer client.Close()
//
//	// start a new job
//	resp, err := client.Start(context.Background(), &pb.StartRequest{
//		Command:   "echo",
//		Arguments: []string{"hello"},
//	})
//	if err != nil {
//		// handle error
//	}
//	jobID := resp.GetId()
//
// Server:
//
//	server, err := NewServer("localhost:8443", "server.crt", "server.key", "client-ca.crt")
//	if err != nil {
//		// handle error
//	}
//	server.StopOnSignals(os.Interrupt)
//
//	// start the server
//	if err := server.Serve(); err != nil {
//		// handle error
//	}
package telejob
