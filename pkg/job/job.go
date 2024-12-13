package job

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// job represents a process with owner and resource limits in any execution
// state.
type job struct {
	mutex  sync.Mutex // protects concurrent access to status which contains mutable state
	status Status

	cmd    *exec.Cmd
	owner  string
	cgroup string
}

// newJob creates a new job with the given id, command, owner, limits and
// cgroup.
func newJob(owner, id string, command string, args []string, limits Limits, cgroup string) (*job, error) {
	cmd, err := newStartedCmd(id, command, args, limits, cgroup)
	if err != nil {
		return nil, err
	}
	return &job{
		status: Status{
			ID:       id,
			Command:  command,
			Args:     args,
			Started:  time.Now(),
			Running:  true,
			ExitCode: NotTerminated,
		},
		cmd:    cmd,
		owner:  owner,
		cgroup: cgroup,
	}, nil
}

// isRunning synchronously returns the running status of the job.
func (j *job) isRunning() bool {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	return j.status.Running
}

// getStatus synchronously creates a copy of the Status of the current to be
// used to returned to the client.
func (j *job) getStatus() Status {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	return j.status
}

// stop stops the job with a `SIGKILL` signal.
func (j *job) stop() error {
	j.mutex.Lock()
	defer j.mutex.Unlock()
	if !j.status.Running {
		slog.Info("job already stopped", "id", j.status.ID)
		return nil
	}
	if err := j.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		// There is an unavoidable race condition between killing the process
		// and waiting for it to exit. We ignore os.ErrProcessDone, as it
		// indicates the process has already exited, possibly due to a
		// concurrent call to job.stop() or natural termination.
		//
		// The cgroup.kill file is used in job.wait() for final cleanup,
		// ensuring any remaining child processes are also terminated.
		return fmt.Errorf("%w: cannot kill %q: %w", ErrJobStop, j.status.ID, err)
	}
	return nil
}

// wait waits for the job to finish, updates the job status and deletes its
// cgroups. It must only be called once per job.
func (j *job) wait() {
	waitErr := j.cmd.Wait()
	j.mutex.Lock()
	defer j.mutex.Unlock()
	j.status.Running = false
	j.status.Stopped = time.Now()
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
		j.status.ExitCode = 0
	case errors.As(waitErr, &exitErr):
		j.status.ExitCode = exitErr.ExitCode()
	default:
		slog.Error("cannot wait for job", "err", waitErr, "id", j.status.ID)
	}
	// Write "1" to <job-cgroup>/cgroup.kill to kill all children.
	if err := writeCgroupFile(j.cgroup, "cgroup.kill", "1"); err != nil {
		slog.Error("cannot write to cgroup.kill", "err", err, "id", j.status.ID)
	}
	deleteCgroupWithRetry(j.cgroup, j.status.ID, 3, time.Second)
}

// deleteCgroupWithRetry deletes the cgroup with the given id and retries the
// deletion if it fails with EBUSY (device or resource busy).
//
// It retries the deletion a specified number of times with a fixed duration
// between each attempt. If all retries fail, it logs an error.
func deleteCgroupWithRetry(cgroup, id string, retries int, dur time.Duration) {
	for i := range retries {
		err := deleteCgroup(cgroup)
		if err == nil {
			if i > 0 {
				slog.Info("successfully cleanup job cgroup", "id", id, "attempt", i+1)
			}
			return // successful deletion
		}
		if !errors.Is(err, syscall.EBUSY) {
			slog.Error("cannot delete cgroup", "err", err, "id", id)
			return
		}
		slog.Info("retrying cleanup job cgroup", "err", err, "id", id, "attempt", i+1)
		time.Sleep(dur) // consider better back-off strategy than constant wait
	}
	slog.Error("cannot delete cgroup after retries", "id", id, "attempt", retries)
}

// newStartedCmd creates a new started command with the given limits and cgroup.
func newStartedCmd(id string, command string, args []string, limits Limits, cgroup string) (*exec.Cmd, error) {
	if err := newJobCgroup(cgroup, limits); err != nil {
		return nil, err
	}
	file, err := os.Open(cgroup) //nolint:gosec // G304: Potential file inclusion via variable
	if err != nil {
		return nil, fmt.Errorf("cannot open new job cgroup %q: %w", cgroup, err)
	}
	defer func() {
		if err := file.Close(); err != nil { // cgroup file can only be closed after exec.Cmd has started!
			slog.Error("cannot close cgroup file", "Status.ID", id, "cgroup", cgroup, "err", err)
		}
	}()
	cmd := exec.Command(command, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(file.Fd())}
	if err := cmd.Start(); err != nil {
		if err := deleteCgroup(cgroup); err != nil {
			slog.Error("cannot delete failed job cgroup", "Status.ID", id, "cgroup", cgroup, "err", err)
		}
		return nil, fmt.Errorf("%w: cannot start command %v: %w", ErrCommand, command, err)
	}
	return cmd, nil
}
