package telejob

import (
	"context"
	"errors"
	"time"

	"github.com/juliaogris/telejob/pkg/job"
	"github.com/juliaogris/telejob/pkg/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service implements the generated gRPC interface pb.TelejobServer.
//
// It requires that the [job.Controller] is initialized and that job owners
// are passed via the context using the [OwnerKey]. It is a lower integration
// point than the Server type for custom security setup or testing.
//
// It implements the gRPC layer to access [job.Controller] methods to:
//   - Start jobs.
//   - Stop jobs.
//   - Retrieve job status.
type Service struct {
	Controller *job.Controller
}

// OwnerKey is the key used to store the job owner in the context.
type OwnerKey struct{}

// Start creates a new job with the given command and arguments. It extracts the
// owner from the context and uses the [job.Controller] to start the job. If
// the command is empty or an error occurs, it returns an appropriate gRPC
// error.
func (s *Service) Start(ctx context.Context, req *pb.StartRequest) (*pb.StartResponse, error) {
	owner := extractOwner(ctx)
	command := req.GetCommand()
	arguments := req.GetArguments()
	if len(command) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "empty command")
	}
	id, err := s.Controller.Start(owner, command, arguments...)
	if err != nil {
		if errors.Is(err, job.ErrCommand) {
			return nil, status.Errorf(codes.InvalidArgument, "%v", err)
		}
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &pb.StartResponse{Id: id}, nil
}

// Stop stops the job with the given ID. It extracts the owner from the context
// and uses the [job.Controller] to stop the job. If an error occurs, it
// returns an appropriate gRPC error.
func (s *Service) Stop(ctx context.Context, req *pb.StopRequest) (*pb.StopResponse, error) {
	owner := extractOwner(ctx)
	if err := s.Controller.Stop(owner, req.GetId()); err != nil {
		return nil, statusError(err, req.GetId())
	}
	return &pb.StopResponse{}, nil
}

// Status retrieves the status of the job with the given ID. It extracts the
// owner from the context and uses the [job.Controller] to get the job status.
// If an error occurs, it returns an appropriate gRPC error.
func (s *Service) Status(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	owner := extractOwner(ctx)
	js, err := s.Controller.Status(owner, req.GetId())
	if err != nil {
		return nil, statusError(err, req.GetId())
	}
	return &pb.StatusResponse{JobStatus: pbJobStatus(js)}, nil
}

// Logs is not yet implemented.
func (s *Service) Logs(_ *pb.LogsRequest, _ pb.Telejob_LogsServer) error {
	return status.Errorf(codes.Unimplemented, "not yet implemented")
}

// pbJobStatus converts a job.Status to a pb.JobStatus.
func pbJobStatus(s job.Status) *pb.JobStatus {
	return &pb.JobStatus{
		Id:        s.ID,
		Command:   s.Command,
		Arguments: s.Args,
		Started:   pbTimestamp(s.Started),
		State:     pbState(s.Running),
		Stopped:   pbTimestamp(s.Stopped),
		ExitCode:  int64(s.ExitCode),
	}
}

// pbTimestamp converts a time.Time to a timestamppb.Timestamp.
// It handles the zero value of time.Time by returning nil.
func pbTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// pbState converts a boolean running state to a pb.State.
func pbState(running bool) pb.State {
	if running {
		return pb.State_STATE_RUNNING
	}
	return pb.State_STATE_STOPPED
}

// statusError converts a job error to a gRPC status error.
func statusError(err error, id string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, job.ErrJobNotFound) {
		return status.Errorf(codes.NotFound, "job %q not found", id)
	}
	if errors.Is(err, job.ErrUnauthorized) {
		return status.Errorf(codes.PermissionDenied, "no ownership of job %q", id)
	}
	return status.Errorf(codes.Internal, "job %q: %v", id, err)
}

func extractOwner(ctx context.Context) string {
	return ctx.Value(OwnerKey{}).(string) //nolint:forcetypeassert // enforced by UnaryInterceptor
}
