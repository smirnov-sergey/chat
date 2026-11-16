package app

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestServeWaitsForActiveRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	finishRequest := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(finishRequest)

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
			w.Write([]byte("done"))
		}),
	}
	shutdownStarted := make(chan struct{})
	server.RegisterOnShutdown(func() { close(shutdownStarted) })
	listener := newTestListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() { server.Close() })

	result := make(chan error, 1)
	go func() { result <- serve(ctx, server, listener, 5*time.Second) }()

	type response struct {
		body   string
		status int
		err    error
	}
	responseResult := make(chan response, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			responseResult <- response{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		responseResult <- response{body: string(body), status: resp.StatusCode, err: err}
	}()

	waitForSignal(t, started)
	cancel()
	waitForSignal(t, shutdownStarted)

	select {
	case err := <-result:
		t.Fatalf("server returned before active request finished: %v", err)
	default:
	}

	finishRequest()
	select {
	case resp := <-responseResult:
		if resp.err != nil {
			t.Fatalf("request: %v", resp.err)
		}
		if resp.status != http.StatusOK || resp.body != "done" {
			t.Errorf("response = (%d, %q), want (200, done)", resp.status, resp.body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("request did not finish")
	}
	if err := waitForError(t, result); err != nil {
		t.Fatalf("serve: %v", err)
	}

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err == nil {
		conn.Close()
		t.Error("server still accepts connections after shutdown")
	}
}

func TestServeForcesCloseAfterShutdownDeadline(t *testing.T) {
	started := make(chan struct{})
	handlerDone := make(chan struct{})
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
			close(handlerDone)
		}),
	}
	listener := newTestListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() { server.Close() })

	result := make(chan error, 1)
	go func() { result <- serve(ctx, server, listener, 50*time.Millisecond) }()

	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + listener.Addr().String())
		if resp != nil {
			resp.Body.Close()
		}
		requestDone <- err
	}()

	waitForSignal(t, started)
	cancel()
	if err := waitForError(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("serve error = %v, want context deadline exceeded", err)
	}
	waitForSignal(t, handlerDone)
	if err := waitForError(t, requestDone); err == nil {
		t.Error("request succeeded, want connection closed")
	}
}

func TestServeReturnsListenerError(t *testing.T) {
	listener := newTestListener(t)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	server := &http.Server{}
	t.Cleanup(func() { server.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := serve(ctx, server, listener, time.Second)
	if !errors.Is(err, net.ErrClosed) {
		t.Errorf("serve error = %v, want closed listener", err)
	}
}

func newTestListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener
}

func waitForSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for lifecycle event")
	}
}

func waitForError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for operation to finish")
		return nil
	}
}
