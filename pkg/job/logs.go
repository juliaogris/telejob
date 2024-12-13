package job

import (
	"context"
	"fmt"
	"io"
	"slices"
)

// channelWriter implements io.Writer by sending byte slices to a channel.
type channelWriter chan []byte

// Write implements io.Writer by sending the provided byte slice to the channel.
//
// A copy of the byte slice is sent to prevent race conditions.
func (w channelWriter) Write(b []byte) (int, error) {
	w <- slices.Clone(b) // Send a copy to avoid data races on the underlying array.
	return len(b), nil
}

// logResponseCh is a channel for receiving log data.
type logResponseCh chan []byte

// logRequest represents a request for log data, specifying the starting index
// and a channel for receiving the response.
type logRequest struct {
	startIdx uint64
	respCh   logResponseCh
}

// logDispatcher distributes log data received on an input channel to multiple
// readers.
type logDispatcher struct {
	inputCh chan []byte
	reqCh   chan logRequest
	doneCh  chan logResponseCh
	fullLog []byte

	// followers is a set of log response channels waiting to receive the next
	// piece of future log data. Followers are removed from this set after the
	// next piece of log data is sent.
	followers map[logResponseCh]bool
}

// newStartedLogDispatcher creates and starts a new logDispatcher. The
// dispatcher runs in its own goroutine.
func newStartedLogDispatcher(inputCh chan []byte) *logDispatcher {
	l := &logDispatcher{
		inputCh:   inputCh,
		reqCh:     make(chan logRequest),
		doneCh:    make(chan logResponseCh),
		followers: make(map[logResponseCh]bool),
	}
	go l.start()
	return l
}

// start is the main loop of the logDispatcher, handling incoming log data,
// requests for logs, and cleaning up log followers that are done.
func (l *logDispatcher) start() {
	for {
		select {
		case b, ok := <-l.inputCh:
			if !ok {
				l.handleInputClosed()
			} else {
				l.handleInput(b)
			}
		case req := <-l.reqCh:
			l.handleRequest(req)
		case respCh := <-l.doneCh:
			if l.followers[respCh] {
				delete(l.followers, respCh)
				close(respCh)
			}
		}
	}
}

// handleInput processes incoming log data.
//
// If the input channel is closed, it notifies all followers and cleans up.
// Otherwise, it appends the new data to the full log and sends it to all
// current followers.
func (l *logDispatcher) handleInput(b []byte) {
	l.fullLog = append(l.fullLog, b...)
	for follower := range l.followers {
		// A follower is always waiting for a response on a buffered channel,
		// this never blocks.
		follower <- b
	}
	clear(l.followers)
}

func (l *logDispatcher) handleInputClosed() {
	l.inputCh = nil
	for follower := range l.followers {
		close(follower)
	}
	clear(l.followers)
}

// handleRequest processes a log request.
//
// If the requested data is already available, it is sent to the requester.
// Otherwise, the requester is added as a follower to receive future log data.
// If the input channel is closed, the response channel is closed immediately.
func (l *logDispatcher) handleRequest(req logRequest) {
	respCh := req.respCh
	switch {
	case req.startIdx < uint64(len(l.fullLog)):
		respCh <- l.fullLog[req.startIdx:]
	case l.inputCh != nil:
		l.followers[respCh] = true
	default:
		close(respCh)
	}
}

// newReader creates a new io.Reader for reading logs from the dispatcher.
//
// Each call to newReader creates a new, independent reader with its own
// dedicated response channel. The provided context controls the lifetime of
// the reader. When the context is cancelled, pending and subsequent calls to
// Read will return an error.
func (l *logDispatcher) newReader(ctx context.Context) io.Reader {
	return &logReader{
		startIdx:   0,
		respCh:     make(logResponseCh, 1),
		ctx:        ctx,
		dispatcher: l,
	}
}

// closeInput closes the log dispatcher's input channel, signaling that no more
// log data will be received. This notifies any active log readers of the end
// of the log stream. After calling closeInput, the dispatcher continues to
// serve log requests from the buffered data in dispatcher.fullLog. The request
// and done channels remain open to facilitate this.
func (l *logDispatcher) closeInput() {
	close(l.inputCh)
}

// logReader reads log data from a logDispatcher.
//
// A logReader requests log data in discrete chunks from the dispatcher using a
// dedicated response channel. It maintains a start index to track the position
// of the next read.
type logReader struct {
	startIdx   uint64
	respCh     logResponseCh
	ctx        context.Context //nolint:containedctx // The context is used to cancel Read.
	dispatcher *logDispatcher
}

// Read reads log data from the dispatcher into p.
//
// It sends a request to the dispatcher for the next chunk of log data
// starting from the reader's current start index. The reader then waits
// for a response on its dedicated response channel.
//
// If the context is cancelled, Read returns an error wrapping the context
// error. If the response channel is closed, Read returns io.EOF, indicating
// the end of the log stream. Otherwise, Read copies the received data into p
// and updates the start index.
func (lr *logReader) Read(p []byte) (int, error) {
	if lr.ctx.Err() != nil {
		return 0, fmt.Errorf("log reader context already done: %w", lr.ctx.Err())
	}
	if lr.respCh == nil {
		return 0, io.EOF
	}
	req := logRequest{startIdx: lr.startIdx, respCh: lr.respCh}
	lr.dispatcher.reqCh <- req
	select {
	case <-lr.ctx.Done():
		lr.dispatcher.doneCh <- req.respCh
		return 0, fmt.Errorf("log reader context received done: %w", lr.ctx.Err())
	case b, ok := <-lr.respCh:
		if !ok {
			lr.respCh = nil
			return 0, io.EOF
		}
		n := copy(p, b)
		lr.startIdx += uint64(n) //nolint:gosec // n cannot be negative.
		return n, nil
	}
}
