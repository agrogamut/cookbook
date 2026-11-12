package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
)

type shutdownPolicy struct {
	drain   time.Duration
	cleanup time.Duration
}

func runServer(ctx context.Context, policy shutdownPolicy, prepare func(context.Context) (*http.Server, func(), error)) (result error) {
	if policy.drain <= 0 || policy.cleanup <= 0 {
		return errors.New("shutdown timeouts must be positive")
	}
	if ctx.Err() != nil {
		return nil
	}
	srv, cleanup, err := prepare(ctx)
	defer func() {
		if cleanup == nil {
			return
		}
		done := make(chan struct{})
		go func() {
			cleanup()
			close(done)
		}()
		timer := time.NewTimer(policy.cleanup)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			result = errors.Join(result, fmt.Errorf("resource cleanup exceeded %s: %w", policy.cleanup, context.DeadlineExceeded))
		}
	}()
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, context.Canceled) {
			return nil
		}
		return fmt.Errorf("prepare server: %w", err)
	}
	if srv == nil {
		return errors.New("prepare server returned no HTTP server")
	}
	if ctx.Err() != nil {
		return nil
	}
	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	base := context.Background()
	if srv.BaseContext != nil {
		base = srv.BaseContext(listener)
	}
	// A stop signal starts draining; request cancellation is reserved for forced shutdown.
	requests, cancelRequests := context.WithCancel(base)
	defer cancelRequests()
	srv.BaseContext = func(net.Listener) context.Context { return requests }
	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()
	log.Printf("server: listening on %s", listener.Addr())
	select {
	case err := <-served:
		cancelRequests()
		closeErr := srv.Close()
		if errors.Is(err, http.ErrServerClosed) {
			return closeErr
		}
		return errors.Join(fmt.Errorf("serve: %w", err), closeErr)
	case <-ctx.Done():
		log.Printf("server: shutdown requested, draining for up to %s", policy.drain)
	}
	// The signal context is already cancelled and cannot be used as the drain deadline.
	drain, cancelDrain := context.WithTimeout(context.Background(), policy.drain)
	defer cancelDrain()
	err = srv.Shutdown(drain)
	if err != nil {
		log.Printf("server: drain failed, cancelling active requests: %v", err)
		cancelRequests()
		err = errors.Join(fmt.Errorf("HTTP drain: %w", err), srv.Close())
	}
	if serveErr := <-served; !errors.Is(serveErr, http.ErrServerClosed) {
		err = errors.Join(err, serveErr)
	}
	if err == nil {
		log.Print("server: HTTP drain complete")
	}
	return err
}
