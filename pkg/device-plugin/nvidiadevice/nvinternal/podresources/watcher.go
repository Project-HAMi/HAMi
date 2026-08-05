/*
 * Copyright (c) 2026, HAMi.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 */

// Package podresources polls the kubelet pod-resources API and reports when
// devices that were previously allocated to a container disappear from the
// kubelet's view, which happens when the container ends.
//
// This is the signal HAMi's MIG reclaim path uses to destroy individual GPU
// instances on task completion instead of waiting for every task on a GPU to
// finish before the next nvidia-mig-parted apply can re-shape the card.
package podresources

import (
	"context"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/klog/v2"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
)

const (
	defaultSocketPath = "/var/lib/kubelet/pod-resources/kubelet.sock"
	defaultPollEvery  = 10 * time.Second
	// dialWait bounds how long we wait for the kubelet-side socket to reach
	// Ready before the first List after (re)connect. The connection itself
	// is created lazily by grpc.NewClient; this timeout only gates the
	// explicit wait performed before the first RPC on a fresh conn.
	dialWait = 5 * time.Second
	// listTimeout bounds a single List RPC. This is intentionally smaller
	// than the poll interval so a slow kubelet can't cause back-to-back
	// ticks to pile up. On very busy nodes with many pods, raising this
	// (and the poll interval) is the right lever.
	listTimeout = 5 * time.Second
	// maxMsgSize matches the kubelet default for this API; on busy nodes the
	// List response can exceed the grpc default 4 MiB.
	maxMsgSize = 16 * 1024 * 1024
)

// ReleaseHandler is invoked once per deviceID that was present in the previous
// snapshot and is missing in the current one. resourceName is the resource the
// device was allocated under (e.g. "nvidia.com/gpu") so a handler serving
// several resources can filter quickly.
type ReleaseHandler func(resourceName, deviceID string)

// Watcher polls kubelet's pod-resources API and fires a ReleaseHandler when a
// previously-allocated device is no longer in use by any container.
type Watcher struct {
	socketPath string
	interval   time.Duration

	// resourceNames restricts the release callback to devices under these
	// resource names. An empty slice means "all resources".
	resourceNames []string

	onRelease ReleaseHandler

	// Previous snapshot: set of active deviceIDs indexed by resource name.
	prev map[string]map[string]struct{}

	// Long-lived gRPC client; rebuilt on failure. kubelet's pod-resources
	// socket is stable across the plugin's lifetime, so reusing the
	// connection avoids eating our per-tick budget on DNS/dial/handshake.
	conn   *grpc.ClientConn
	client podresourcesv1.PodResourcesListerClient
}

// NewWatcher constructs a Watcher. Pass an empty socketPath to use the default
// kubelet location; pass 0 interval to use the default poll cadence.
func NewWatcher(socketPath string, interval time.Duration, resourceNames []string, onRelease ReleaseHandler) *Watcher {
	if socketPath == "" {
		socketPath = defaultSocketPath
	}
	if interval <= 0 {
		interval = defaultPollEvery
	}
	return &Watcher{
		socketPath:    socketPath,
		interval:      interval,
		resourceNames: resourceNames,
		onRelease:     onRelease,
		prev:          make(map[string]map[string]struct{}),
	}
}

// Run polls the kubelet pod-resources API in a loop until ctx is cancelled.
// It never returns an error; transient gRPC failures are logged and the
// previous snapshot is preserved so a missed tick doesn't produce spurious
// release events.
func (w *Watcher) Run(ctx context.Context) {
	klog.InfoS("starting podresources watcher", "socket", w.socketPath, "interval", w.interval, "resources", w.resourceNames)
	defer w.closeConn()

	// Prime the snapshot before starting to diff. If this first call fails
	// we start with an empty map and the first successful tick will just
	// record — no spurious release events for the state at plugin start.
	if err := w.tick(ctx, true); err != nil {
		klog.InfoS("podresources initial List failed; will retry on interval", "err", err)
	}

	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := w.tick(ctx, false); err != nil {
				klog.InfoS("podresources tick failed; keeping previous snapshot", "err", err)
			}
		}
	}
}

func (w *Watcher) tick(ctx context.Context, prime bool) error {
	if err := w.ensureConn(ctx); err != nil {
		return err
	}

	callCtx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()

	resp, err := w.client.List(callCtx, &podresourcesv1.ListPodResourcesRequest{})
	if err != nil {
		// Drop the connection so the next tick reconnects; the kubelet
		// socket can be recreated (e.g. kubelet restart) without the
		// plugin restarting, and sticking to a dead conn wastes ticks.
		w.closeConn()
		return err
	}

	current := w.collect(resp)
	if !prime {
		w.diff(current)
	}
	w.prev = current
	return nil
}

// ensureConn makes sure w.conn/w.client are usable, creating them lazily.
// The first call after (re)connect waits up to dialWait for the channel to
// reach Ready so the subsequent List has the full listTimeout budget.
func (w *Watcher) ensureConn(ctx context.Context) error {
	if w.conn != nil {
		switch w.conn.GetState() {
		case connectivity.Shutdown:
			w.closeConn()
		default:
			return nil
		}
	}

	conn, err := grpc.NewClient(
		"unix://"+w.socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMsgSize)),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", normalizeUnixDialAddr(addr))
		}),
	)
	if err != nil {
		return err
	}

	// Kick the channel and wait briefly for Ready. If the kubelet socket
	// isn't accepting yet, let the caller surface a clean error instead
	// of eating the full List budget on handshake.
	conn.Connect()
	waitCtx, cancel := context.WithTimeout(ctx, dialWait)
	defer cancel()
	for {
		s := conn.GetState()
		if s == connectivity.Ready {
			break
		}
		if !conn.WaitForStateChange(waitCtx, s) {
			_ = conn.Close()
			return waitCtx.Err()
		}
	}

	w.conn = conn
	w.client = podresourcesv1.NewPodResourcesListerClient(conn)
	return nil
}

func normalizeUnixDialAddr(addr string) string {
	trimmed := strings.TrimPrefix(addr, "unix://")
	if trimmed != addr {
		return trimmed
	}
	return strings.TrimPrefix(addr, "unix:")
}

func (w *Watcher) closeConn() {
	if w.conn != nil {
		_ = w.conn.Close()
		w.conn = nil
		w.client = nil
	}
}

// collect flattens the List response into "resourceName -> set(deviceID)".
func (w *Watcher) collect(resp *podresourcesv1.ListPodResourcesResponse) map[string]map[string]struct{} {
	out := make(map[string]map[string]struct{})
	for _, pod := range resp.GetPodResources() {
		for _, c := range pod.GetContainers() {
			for _, d := range c.GetDevices() {
				rn := d.GetResourceName()
				if !w.resourceMatch(rn) {
					continue
				}
				set, ok := out[rn]
				if !ok {
					set = make(map[string]struct{})
					out[rn] = set
				}
				for _, id := range d.GetDeviceIds() {
					set[id] = struct{}{}
				}
			}
		}
	}
	return out
}

// diff fires onRelease for every deviceID that was present in the previous
// snapshot under a given resource but is absent in the current snapshot.
func (w *Watcher) diff(current map[string]map[string]struct{}) {
	for rn, prevSet := range w.prev {
		currSet := current[rn]
		for id := range prevSet {
			if _, stillUsed := currSet[id]; stillUsed {
				continue
			}
			if w.onRelease != nil {
				w.onRelease(rn, id)
			}
		}
	}
}

func (w *Watcher) resourceMatch(rn string) bool {
	if len(w.resourceNames) == 0 {
		return true
	}
	for _, want := range w.resourceNames {
		if strings.EqualFold(rn, want) {
			return true
		}
	}
	return false
}
