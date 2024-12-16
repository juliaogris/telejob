package job

import (
	"errors"
	"time"
)

// Sentinel Errors returned by the job package.
var (
	ErrCgroup       = errors.New("cgroup error")
	ErrCommand      = errors.New("command error")
	ErrJobNotFound  = errors.New("job not found")
	ErrJobStop      = errors.New("job stop error")
	ErrShutdown     = errors.New("already shut down")
	ErrUnauthorized = errors.New("unauthorized")
)

// NotTerminated is the exit code used to indicate that a job is still running.
//
// The os package uses an exit code of -1 if the process hasn't exited or was
// terminated by a signal. To avoid ambiguity, this package uses -2 to
// specifically represent a job that has not yet terminated.
const (
	NotTerminated      = -2
	TerminatedBySignal = -1
)

// Status represents the current state of the job.
type Status struct {
	ID       string
	Command  string
	Args     []string
	Started  time.Time
	Running  bool
	ExitCode int
	Stopped  time.Time
}

// Limits represents the resource limits for a job.
type Limits struct {
	CPUs      float64
	MemoryKiB uint64
	IO        []string
}
