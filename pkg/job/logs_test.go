package job

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLogsSimple(t *testing.T) {
	t.Parallel()
	inputCh := make(chan []byte)
	go func() {
		inputCh <- []byte("hello")
		close(inputCh)
	}()
	dispatcher := newStartedLogDispatcher(inputCh)
	r := dispatcher.newReader(context.Background())
	b := make([]byte, 10)
	n, err := r.Read(b)
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, "hello", string(b[:n]))
	_, err = r.Read(b)
	require.ErrorIs(t, err, io.EOF)
}

func TestLogsNoInput(t *testing.T) {
	t.Parallel()
	inputCh := make(chan []byte)
	go func() {
		close(inputCh)
	}()
	dispatcher := newStartedLogDispatcher(inputCh)
	r := dispatcher.newReader(context.Background())
	b := make([]byte, 10)
	n, err := r.Read(b)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 0, n)
	require.Equal(t, "", string(b[:n]))
}

func TestLogsWithManyReaders(t *testing.T) {
	t.Parallel()
	const readerCount = 100

	inputCh := make(chan []byte)
	dispatcher := newStartedLogDispatcher(inputCh)
	go func() {
		inputCh <- []byte("hello")
		close(inputCh)
	}()
	readers := make([]io.Reader, readerCount)
	wg := &sync.WaitGroup{}
	wg.Add(len(readers))
	for i := range readers {
		readers[i] = dispatcher.newReader(context.Background())
		go func() {
			requireRead(t, readers[i], 2, "hello")
			wg.Done()
		}()
	}
	waitWithTimeout(t, wg, time.Second*100)
}

func TestLogsWithManyDelayedReaders(t *testing.T) {
	t.Parallel()
	const readerCount = 100
	const delay = 100 * time.Millisecond
	const text = "Hello slow, slow world!"

	inputCh := make(chan []byte)

	dispatcher := newStartedLogDispatcher(inputCh)
	go inputSlowly(inputCh, text, delay)
	wg := &sync.WaitGroup{}
	wg.Add(readerCount)
	for range readerCount {
		r := dispatcher.newReader(context.Background())
		go func() {
			sr := &slowReader{r: r, delay: time.Millisecond * 10}
			requireRead(t, sr, 20, text)
			wg.Done()
		}()
	}
	waitWithTimeout(t, wg, time.Second*100)
}

type slowReader struct {
	r     io.Reader
	delay time.Duration
}

func (sr *slowReader) Read(b []byte) (int, error) {
	if sr.delay > 0 {
		randDelay := time.Duration(rand.Int63n(int64(sr.delay)))
		time.Sleep(randDelay)
	}
	return sr.r.Read(b) //nolint:wrapcheck
}

func TestLogsWithCancel(t *testing.T) {
	t.Parallel()
	inputCh := make(chan []byte)
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := newStartedLogDispatcher(inputCh)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	r := dispatcher.newReader(ctx)
	go func() {
		_, err := r.Read(make([]byte, 1))
		if !errors.Is(err, context.Canceled) {
			t.Errorf("TestLogsWithCancel: not a contextCancel error: %v", err)
		}
		wg.Done()
	}()

	cancel()
	waitWithTimeout(t, wg, time.Second)

	// ensure we can read historical logs
	inputCh <- []byte("hi")
	r = dispatcher.newReader(context.Background())
	close(inputCh)
	requireRead(t, r, 1, "hi")
}

type delayedTestCase struct {
	name        string
	input       string
	inputDelay  time.Duration
	outputDelay time.Duration
}

func TestLogsWithDelay(t *testing.T) {
	t.Parallel()
	input := "hello"
	delay := time.Millisecond * 20
	longerInput := strings.Repeat(input, 100)
	shortDelay := time.Millisecond
	testCases := []delayedTestCase{
		{"no delay", input, 0, 0},
		{"no input", "", 0, 0},
		{"input delay", input, delay, 0},
		{"output delay", input, 0, delay},
		{"input and output delay", input, delay, delay},
		{"long input", longerInput, shortDelay, shortDelay},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inputCh := make(chan []byte)

			go inputSlowly(inputCh, tc.input, tc.inputDelay)
			dispatcher := newStartedLogDispatcher(inputCh)
			r := dispatcher.newReader(context.Background())
			rs := &slowReader{r: r, delay: tc.outputDelay}
			requireRead(t, rs, 10, tc.input)
		})
	}
}

func inputSlowly(inputCh chan []byte, s string, delay time.Duration) {
	b := []byte(s)
	for i := range b {
		inputCh <- b[i : i+1]
		if delay > 0 {
			randDelay := time.Duration(rand.Int63n(int64(delay)))
			time.Sleep(randDelay)
		}
	}
	close(inputCh)
}

func waitWithTimeout(t *testing.T, wg *sync.WaitGroup, timeout time.Duration) {
	t.Helper()
	c := make(chan struct{})
	go func() {
		wg.Wait()
		close(c)
	}()
	select {
	case <-c:
		return
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for wait group")
	}
}

func requireRead(t *testing.T, r io.Reader, size int, want string) {
	t.Helper()
	b := make([]byte, size)
	got := &strings.Builder{}
	for {
		n, err := r.Read(b)
		if errors.Is(err, io.EOF) {
			if want != got.String() {
				t.Errorf("requireRead: want != got: \nwant: %v\ngot:  %v", want, got) // go-routine safe
			}
			return
		} else if err != nil {
			t.Errorf("requireRead: error: %v", err) // go-routine safe
		}
		got.Write(b[:n])
	}
}
