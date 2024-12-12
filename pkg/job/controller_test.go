package job_test

import (
	"flag"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/juliaogris/telejob/pkg/job"
	"github.com/stretchr/testify/require"
)

var jobCount = flag.Int("jobs", 100, "number of jobs to start") //nolint:gochecknoglobals

func TestControllerSimple(t *testing.T) {
	t.Parallel()
	cgroup := randCgroup()
	controller, err := job.NewController(job.WithCgroup(cgroup))
	require.NoError(t, err)
	defer cleanupCgroup(cgroup)

	id, err := controller.Start("owner", "sleep", "10")
	require.NoError(t, err)

	got, err := controller.Status("owner", id)
	require.NoError(t, err)
	want := job.Status{
		ID:       id,
		Command:  "sleep",
		Args:     []string{"10"},
		Started:  got.Started,
		Running:  true,
		ExitCode: job.NotTerminated,
		Stopped:  time.Time{},
	}
	require.Equal(t, want, got)
	require.False(t, got.Started.After(time.Now()))

	err = controller.Stop("owner", id)
	require.NoError(t, err)
	requireEventuallyStopped(t, controller, "owner", id)

	got, err = controller.Status("owner", id)
	require.NoError(t, err)
	want = job.Status{
		ID:       id,
		Command:  "sleep",
		Args:     []string{"10"},
		Started:  got.Started,
		Running:  false,
		ExitCode: job.TerminatedBySignal,
		Stopped:  got.Stopped,
	}
	require.Equal(t, want, got)
	require.False(t, got.Started.After(time.Now()))

	err = controller.StopAll()
	require.NoError(t, err)

	_, err = os.Stat(cgroup)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestControllerCommandOutput(t *testing.T) {
	t.Parallel()
	cgroup := randCgroup()
	controller, err := job.NewController(job.WithCgroup(cgroup))
	require.NoError(t, err)
	defer cleanupCgroup(cgroup)

	count := 10
	tmpDir := t.TempDir()
	tmpFiles := make([]string, count)
	ids := make([]string, count)
	for i := range count {
		tmpFiles[i] = filepath.Join(tmpDir, fmt.Sprintf("hello-%d.txt", i))
		awkCmd := fmt.Sprintf(`END { print "hello world %d" > "%s" }`, i, tmpFiles[i])
		id, err := controller.Start("owner1", "awk", awkCmd)
		require.NoError(t, err)
		ids[i] = id
	}
	// wait for all awk commands to terminate naturally
	for _, id := range ids {
		requireEventuallyStopped(t, controller, "owner1", id)
	}

	for i, f := range tmpFiles {
		b, err := os.ReadFile(f) //nolint:gosec // G304: Potential file inclusion via variable
		require.NoError(t, err)
		want := fmt.Sprintf("hello world %d\n", i)
		require.Equal(t, want, string(b))
	}

	err = controller.StopAll()
	require.NoError(t, err)

	_, err = os.Stat(cgroup)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestControllerWithManyJobs(t *testing.T) {
	t.Parallel()

	limits := job.Limits{CPUs: 0.5, MemoryKiB: 1000}
	cgroup := randCgroup()
	controller, err := job.NewController(job.WithCgroup(cgroup), job.WithLimits(limits))
	require.NoError(t, err)
	defer cleanupCgroup(cgroup)

	// count greater than ~9000, resulting 9000+ cmd.Starts in parallel with other tests cause runtime panic:
	// runtime: program exceeds 10000-thread limit
	// fatal error: thread exhaustion
	// Limit can be pushed with debug.SetMaxThreads(20000).
	count := *jobCount // defaults to 100
	for range count {
		_, err := controller.Start("owner1", "sleep", "100")
		require.NoError(t, err)
	}

	var subdirs []string
	err = filepath.Walk(cgroup, func(path string, info os.FileInfo, _ error) error {
		if info.IsDir() && info.Name() != filepath.Base(cgroup) {
			subdirs = append(subdirs, path)
		}
		return nil
	})
	require.NoError(t, err)
	require.Len(t, subdirs, count)
	for _, d := range subdirs {
		b, err := os.ReadFile(filepath.Join(d, "cpu.max")) //nolint:gosec // G304: Potential file inclusion via variable
		require.NoError(t, err)
		require.Equal(t, "50000 100000\n", string(b))
		b, err = os.ReadFile(filepath.Join(d, "memory.max")) //nolint:gosec // G304: Potential file inclusion via variable
		require.NoError(t, err)
		require.Equal(t, "1024000\n", string(b))
	}

	err = controller.StopAll()
	require.NoError(t, err)

	_, err = os.Stat(cgroup)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestControllerOwnerAccess(t *testing.T) {
	t.Parallel()
	cgroup := randCgroup()
	defer cleanupCgroup(cgroup)

	controller, err := job.NewController(job.WithCgroup(cgroup))
	require.NoError(t, err)

	id1, err := controller.Start("owner1", "sleep", "100")
	require.NoError(t, err)

	_, err = controller.Status("WRONG-OWNER", id1)
	require.ErrorIs(t, err, job.ErrUnauthorized)

	status, err := controller.Status("owner1", id1)
	require.NoError(t, err)
	require.True(t, status.Running)

	err = controller.StopAll()
	require.NoError(t, err)

	_, err = os.Stat(cgroup)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestControllerExitCode(t *testing.T) {
	t.Parallel()
	cgroup := randCgroup()
	controller, err := job.NewController(job.WithCgroup(cgroup))
	require.NoError(t, err)
	defer cleanupCgroup(cgroup)

	id1, err := controller.Start("owner1", "sleep", "100")
	require.NoError(t, err)
	status, err := controller.Status("owner1", id1)
	require.NoError(t, err)
	require.Equal(t, job.NotTerminated, status.ExitCode)
	err = controller.Stop("owner1", id1)
	require.NoError(t, err)

	requireEventuallyStopped(t, controller, "owner1", id1)
	status, err = controller.Status("owner1", id1)
	require.NoError(t, err)
	require.Equal(t, job.TerminatedBySignal, status.ExitCode) // SIGKILL

	id1, err = controller.Start("owner1", "false")
	require.NoError(t, err)
	requireEventuallyStopped(t, controller, "owner1", id1)
	status, err = controller.Status("owner1", id1)
	require.NoError(t, err)
	require.Equal(t, 1, status.ExitCode) // "false" command has exit status 1

	id1, err = controller.Start("owner1", "true")
	require.NoError(t, err)
	requireEventuallyStopped(t, controller, "owner1", id1)
	status, err = controller.Status("owner1", id1)
	require.NoError(t, err)
	require.Equal(t, 0, status.ExitCode) // "true" command has exit status 0

	_, err = controller.Start("owner1", "NON-EXISTENT-COMMAND")
	require.ErrorIs(t, err, job.ErrCommand)

	err = controller.StopAll()
	require.NoError(t, err)

	_, err = os.Stat(cgroup)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func randCgroup() string {
	//nolint:gosec // G404: Use of weak random number generator
	return fmt.Sprintf("/sys/fs/cgroup/telejob-%d", rand.Uint64())
}

func requireEventuallyStopped(t *testing.T, controller *job.Controller, owner, id string) {
	t.Helper()
	fn := func() bool {
		status, err := controller.Status(owner, id)
		require.NoError(t, err)
		return !status.Running
	}
	require.Eventually(t, fn, time.Second*2, time.Millisecond*50, 0)
}

func cleanupCgroup(cgroup string) {
	// best effort cleanup
	var subdirs []string
	_ = filepath.Walk(cgroup, func(path string, info os.FileInfo, _ error) error {
		if info != nil && info.IsDir() && info.Name() != filepath.Base(cgroup) {
			subdirs = append(subdirs, path)
		}
		return nil
	})
	for _, d := range subdirs {
		_ = os.Remove(d)
	}
	_ = os.Remove(cgroup)
}
