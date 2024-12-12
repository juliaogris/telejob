// Package job provides a controller and job types for the telejob service.
//
// It provides methods to manage jobs:
//   - Start: Creates and starts a new job.
//   - Stop: Stops a running job.
//   - Status: Returns the current status of a job.
//
// ## Job Access:
// Started jobs may only be accessed by their owner.
//
// ## Concurrency:
// The Status method returns a concurrency-safe copy of the job.Status.package job
//
// ## Resource Limits:
// The controller enforces memory, I/O, and CPU resource limits for each job
// using cgroups v2. These limits are configured using functional options when
// creating the controller.
package job

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
)

// The Controller manages jobs for the telejob service.
//
// It provides methods to:
//   - Start jobs.
//   - Stop jobs.
//   - Retrieve job status.
type Controller struct {
	mutex         sync.Mutex
	wg            sync.WaitGroup
	jobs          map[string]*job
	maxID         atomic.Uint64
	shutDown      bool
	telejobCgroup string
	limits        Limits
}

// NewController creates a new Controller with the given options.
func NewController(opts ...Option) (*Controller, error) {
	controller := &Controller{
		jobs:          make(map[string]*job),
		telejobCgroup: "/sys/fs/cgroup/telejob",
	}
	for _, opt := range opts {
		opt(controller)
	}
	if err := newTelejobCgroup(controller.telejobCgroup); err != nil {
		return nil, err
	}
	return controller, nil
}

// Option is a functional option for the Controller.
type Option func(*Controller)

// WithCgroup sets the parent cgroup for the Controller.
// All job cgroups will be created as children of this cgroup.
func WithCgroup(cgroup string) Option {
	return func(c *Controller) {
		c.telejobCgroup = cgroup
	}
}

// WithLimits sets the resource limits for the Controller.
// These limits will be applied to each job managed by the controller.
func WithLimits(limits Limits) Option {
	return func(c *Controller) {
		c.limits = limits
	}
}

// Start starts a new job with the given command and arguments for the given
// owner. It returns the ID of the newly started job, or an error if the job
// could not be started.
//
// The job is executed within its own cgroup, with resource limits applied as
// configured on the controller.
func (c *Controller) Start(owner string, command string, args ...string) (string, error) {
	if len(command) == 0 {
		return "", fmt.Errorf("%w: empty command", ErrCommand)
	}

	if c.isShutDown() {
		return "", fmt.Errorf("cannot start command: %w", ErrShutdown)
	}
	id := strconv.FormatUint(c.maxID.Add(1), 10)

	cgroup := filepath.Join(c.telejobCgroup, id)
	job, err := newJob(owner, id, command, args, c.limits, cgroup)
	if err != nil {
		return "", err
	}

	c.add(id, job) // synchronized with c.mutex

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		job.wait()
	}()
	return id, nil
}

// Stop stops the job with the given id.
//
// It terminates the job's process and all its child processes by first sending
// a SIGKILL signal directly to the job's process and then to all its child
// processes via the job's cgroup.kill file.
func (c *Controller) Stop(owner, id string) error {
	job, err := c.get(owner, id)
	if err != nil {
		return err
	}
	return job.stop()
}

// Status retrieves the status of the job with the given ID.
//
// It returns a concurrency-safe copy of the job's status. If the job does not
// exist or the owner does not have access to it, an error is returned.
func (c *Controller) Status(owner, id string) (Status, error) {
	job, err := c.get(owner, id)
	if err != nil {
		return Status{}, err
	}
	return job.getStatus(), nil
}

// StopAll stops all running jobs and cleans up the controller's resources.
//
// This method should be called only during shutdown. It iterates through all
// jobs, stops them, and waits for their termination. It also removes the
// parent cgroup.
//
// Since StopAll is intended for shutdown, it prioritizes completeness over
// latency and holds the controller's lock for the duration of the process.
func (c *Controller) StopAll() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.shutDown {
		slog.Info("already shut down")
		return nil
	}
	c.shutDown = true

	errs := []error{}
	for _, job := range c.jobs {
		if job.isRunning() {
			if err := job.stop(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	c.wg.Wait() // wait for all jobs to terminate.
	if err := deleteCgroup(c.telejobCgroup); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// add adds a job to the controller's job map. It is synchronized to ensure safe
// concurrent access to the job map.
func (c *Controller) add(id string, job *job) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.jobs[id] = job
}

// get retrieves a job from the controller by ID. It is synchronized to ensure
// safe concurrent access to the job map. It also verifies that the given owner
// has access to the requested job.
func (c *Controller) get(owner, id string) (*job, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	job, ok := c.jobs[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrJobNotFound, id)
	}
	if job.owner != owner {
		return nil, fmt.Errorf("%w: owner %q does not have access to job %q", ErrUnauthorized, owner, id)
	}
	return job, nil
}

// isShutDown reports whether the controller has been shut down. It is
// synchronized to ensure safe concurrent access to the shutdown status.
func (c *Controller) isShutDown() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.shutDown
}

// newTelejobCgroup creates a new parent cgroup for telejob with the CPU, I/O,
// and memory resource controllers enabled. It creates the cgroup directory and
// writes "+cpu +io +memory" to the cgroup.subtree_control file to enable the
// necessary controllers.
func newTelejobCgroup(telejobCgroup string) error {
	err := os.Mkdir(telejobCgroup, 0o750)
	if err != nil {
		return fmt.Errorf("cannot create new telejob cgroup %q: %w", telejobCgroup, err)
	}
	controlFile := filepath.Join(telejobCgroup, "cgroup.subtree_control")
	if err := os.WriteFile(controlFile, []byte("+cpu +io +memory"), 0o600); err != nil {
		return fmt.Errorf("cannot configure cgroup subtree control %q: %w", controlFile, err)
	}
	return nil
}

// newJobCgroup creates a new cgroup for a job with the specified resource
// limits. The new cgroup is created as a subcgroup under the given parent
// cgroup. It configures CPU, memory, and I/O limits based on the provided
// Limits.
func newJobCgroup(cgroup string, limits Limits) (err error) { //nolint:nonamedreturns // deliberate cleanup of error
	if err := os.Mkdir(cgroup, 0o750); err != nil {
		return fmt.Errorf("cannot create new job cgroup %q: %w", cgroup, err)
	}
	defer func() { deleteCgroupOnErr(cgroup, err) }()
	if limits.CPUs > 0 {
		content := fmt.Sprintf("%d\n", int(limits.CPUs*100000))
		if err := writeCgroupFile(cgroup, "cpu.max", content); err != nil {
			return err
		}
	}
	if limits.MemoryKiB > 0 {
		content := fmt.Sprintf("%d\n", limits.MemoryKiB*1024)
		if err := writeCgroupFile(cgroup, "memory.max", content); err != nil {
			return err
		}
	}
	for _, ioLimit := range limits.IO {
		if err := writeCgroupFile(cgroup, "io.max", ioLimit); err != nil {
			return err
		}
	}
	return nil
}

// writeCgroupFile writes a cgroup file with the given content. It takes the job's
// cgroup directory, the filename of the cgroup filename, and the content to
// write to the file.
func writeCgroupFile(jobCgroup, filename, content string) error {
	absFilename := filepath.Join(jobCgroup, filename)
	if err := os.WriteFile(absFilename, []byte(content), 0o600); err != nil {
		return fmt.Errorf("%w: cannot create %q", ErrCgroup, absFilename)
	}
	return nil
}

// deleteCgroup deletes the cgroup. It first checks if the cgroup exists. If it
// doesn't exist, it returns nil(no error). If the cgroup exists, it attempts
// to remove it.
func deleteCgroup(cgroup string) error {
	if _, err := os.Stat(cgroup); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("cannot stat existing cgroup %q: %w", cgroup, err)
	}
	if err := os.Remove(cgroup); err != nil {
		return fmt.Errorf("cannot delete cgroup %q: %w", cgroup, err)
	}
	return nil
}

// deleteCgroupOnErr deletes the given cgroup if an error occurs.
//
// This function is intended to be used as a cleanup mechanism after cgroup
// creation. If the provided error is non-nil, it attempts to delete the cgroup
// to avoid leaving orphaned cgroups. If an error occurs during deletion, it
// logs an error message but does not return the error as it is intended for
// use with `defer`.
func deleteCgroupOnErr(cgroup string, err error) {
	if err == nil {
		return
	}
	if err := deleteCgroup(cgroup); err != nil {
		slog.Error("cgroup cleanup error", "cgroup", cgroup, "error", err)
	}
}
