package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

type runningServer struct {
	server   *http.Server
	listener net.Listener
	cancel   context.CancelFunc
	done     chan struct{}
	err      error
}

func startServer(t *testing.T, handler http.Handler, policy shutdownPolicy, cleanup func()) *runningServer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan net.Listener, 1)
	r := &runningServer{cancel: cancel, done: make(chan struct{})}
	r.server = &http.Server{
		Addr: "127.0.0.1:0", Handler: handler,
		BaseContext: func(l net.Listener) context.Context {
			listening <- l
			return context.Background()
		},
	}
	go func() {
		r.err = runServer(ctx, policy, func(context.Context) (*http.Server, func(), error) {
			return r.server, cleanup, nil
		})
		close(r.done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(3 * time.Second):
			t.Error("server did not stop")
		}
	})
	select {
	case r.listener = <-listening:
	case <-r.done:
		t.Fatalf("server exited before listening: %v", r.err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not listen")
	}
	return r
}

func (r *runningServer) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-r.done:
		return r.err
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown exceeded test deadline")
		return nil
	}
}

func TestServerDrainsInflightRequest(t *testing.T) {
	entered := make(chan context.Context, 1)
	finish := make(chan struct{})
	handlerDone := make(chan struct{})
	cleaned := make(chan bool, 1)
	r := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		entered <- req.Context()
		<-finish
		_, _ = io.WriteString(w, "complete")
		close(handlerDone)
	}), shutdownPolicy{time.Second, time.Second}, func() {
		select {
		case <-handlerDone:
			cleaned <- true
		default:
			cleaned <- false
		}
	})
	response := make(chan string, 1)
	go func() {
		res, err := http.Get("http://" + r.listener.Addr().String())
		if err != nil {
			response <- err.Error()
			return
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		response <- fmt.Sprintf("%d:%s", res.StatusCode, body)
	}()
	requestCtx := <-entered
	r.cancel()
	select {
	case <-requestCtx.Done():
		t.Fatal("stop signal cancelled request before drain")
	case <-r.done:
		t.Fatal("shutdown returned before handler completed")
	case <-time.After(25 * time.Millisecond):
	}
	close(finish)
	if err := r.wait(t); err != nil {
		t.Fatal(err)
	}
	if got := <-response; got != "200:complete" {
		t.Fatalf("response = %q", got)
	}
	if !<-cleaned {
		t.Fatal("resources closed before request drained")
	}
	if conn, err := net.DialTimeout("tcp", r.listener.Addr().String(), time.Second); err == nil {
		conn.Close()
		t.Fatal("listener still accepts connections")
	}
}

func TestServerDeadlineCancelsBeforeCleanup(t *testing.T) {
	entered := make(chan context.Context, 1)
	requestDone := make(chan struct{})
	var requestCtx context.Context
	cleaned := make(chan bool, 1)
	r := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		entered <- req.Context()
		<-req.Context().Done()
		close(requestDone)
	}), shutdownPolicy{30 * time.Millisecond, time.Second}, func() {
		cleaned <- errors.Is(requestCtx.Err(), context.Canceled)
	})
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		res, err := http.Get("http://" + r.listener.Addr().String())
		if err == nil {
			res.Body.Close()
		}
	}()
	requestCtx = <-entered
	r.cancel()
	if err := r.wait(t); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected drain deadline, got %v", err)
	}
	if !<-cleaned {
		t.Fatal("cleanup preceded request cancellation")
	}
	<-requestDone
	<-clientDone
}

func TestServerStartupErrorsReleaseResources(t *testing.T) {
	for _, tc := range []struct {
		name string
		srv  *http.Server
		err  error
	}{
		{"prepare", nil, errors.New("startup failed")},
		{"missing server", nil, nil},
		{"listen", &http.Server{Addr: "invalid-address"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cleaned := false
			err := runServer(context.Background(), shutdownPolicy{time.Second, time.Second}, func(context.Context) (*http.Server, func(), error) {
				return tc.srv, func() { cleaned = true }, tc.err
			})
			if err == nil || !cleaned {
				t.Fatalf("error=%v, cleanup=%v", err, cleaned)
			}
		})
	}
}

func TestServerServeErrorAndNormalClose(t *testing.T) {
	for _, normal := range []bool{false, true} {
		t.Run(fmt.Sprint(normal), func(t *testing.T) {
			cleaned := false
			r := startServer(t, http.NotFoundHandler(), shutdownPolicy{time.Second, time.Second}, func() { cleaned = true })
			if normal {
				_ = r.server.Close()
			} else {
				_ = r.listener.Close()
			}
			err := r.wait(t)
			if (err == nil) != normal || !cleaned {
				t.Fatalf("normal=%v, error=%v, cleanup=%v", normal, err, cleaned)
			}
		})
	}
}

func TestServerCleanupDeadline(t *testing.T) {
	release := make(chan struct{})
	cleanupDone := make(chan struct{})
	defer func() { close(release); <-cleanupDone }()
	r := startServer(t, http.NotFoundHandler(), shutdownPolicy{time.Second, 20 * time.Millisecond}, func() {
		<-release
		close(cleanupDone)
	})
	r.cancel()
	if err := r.wait(t); !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "resource cleanup") {
		t.Fatalf("expected cleanup deadline, got %v", err)
	}
}

func TestServerAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runServer(ctx, shutdownPolicy{time.Second, time.Second}, func(context.Context) (*http.Server, func(), error) {
		t.Fatal("prepared resources after cancellation")
		return nil, nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestServerCancelledPreparationReleasesResources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleaned := false
	err := runServer(ctx, shutdownPolicy{time.Second, time.Second}, func(context.Context) (*http.Server, func(), error) {
		cancel()
		return nil, func() { cleaned = true }, context.Canceled
	})
	if err != nil || !cleaned {
		t.Fatalf("error=%v, cleanup=%v", err, cleaned)
	}
}

func TestServerSignalProcess(t *testing.T) {
	if os.Getenv("RECIPE_SHUTDOWN_TEST_PROCESS") == "1" {
		ctx, stop := terminationContext()
		defer stop()
		err := runServer(ctx, shutdownPolicy{time.Second, time.Second}, func(context.Context) (*http.Server, func(), error) {
			return &http.Server{
				Addr: "127.0.0.1:0", Handler: http.NotFoundHandler(),
				BaseContext: func(net.Listener) context.Context {
					fmt.Println("SERVER_READY")
					return context.Background()
				},
			}, func() { fmt.Println("CLEANUP_DONE") }, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, sig := range []os.Signal{syscall.SIGTERM, os.Interrupt} {
		t.Run(sig.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestServerSignalProcess$", "-test.v")
			command.Env = append(os.Environ(), "RECIPE_SHUTDOWN_TEST_PROCESS=1")
			var stderr bytes.Buffer
			command.Stderr = &stderr
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			scanner := bufio.NewScanner(stdout)
			ready := false
			for scanner.Scan() {
				if scanner.Text() == "SERVER_READY" {
					ready = true
					break
				}
			}
			if ready {
				if err := command.Process.Signal(sig); err != nil {
					t.Error(err)
				}
			}
			cleaned := false
			for scanner.Scan() {
				cleaned = cleaned || scanner.Text() == "CLEANUP_DONE"
			}
			err = command.Wait()
			if !ready || !cleaned || err != nil || scanner.Err() != nil {
				t.Fatalf("ready=%v, cleaned=%v, exit=%v, scan=%v, stderr=%s", ready, cleaned, err, scanner.Err(), stderr.String())
			}
		})
	}
}
