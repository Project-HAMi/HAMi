/*
Copyright 2026 The HAMi Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tracing

import (
	"context"
	"math/rand/v2"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"gotest.tools/v3/assert"
)

func TestSamplerFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		sampler string
		arg     string
		want    sdktrace.Sampler
	}{
		{name: "unset", want: sdktrace.ParentBased(sdktrace.AlwaysSample())},
		{name: "always_on", sampler: "always_on", want: sdktrace.AlwaysSample()},
		{name: "always_off", sampler: "always_off", want: sdktrace.NeverSample()},
		{name: "traceidratio", sampler: "traceidratio", arg: "0.1", want: sdktrace.TraceIDRatioBased(0.1)},
		{name: "traceidratio without arg", sampler: "traceidratio", want: sdktrace.TraceIDRatioBased(1)},
		{name: "parentbased_always_on", sampler: "parentbased_always_on", want: sdktrace.ParentBased(sdktrace.AlwaysSample())},
		{name: "parentbased_always_off", sampler: "parentbased_always_off", want: sdktrace.ParentBased(sdktrace.NeverSample())},
		{name: "parentbased_traceidratio", sampler: "parentbased_traceidratio", arg: "0.1", want: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))},
		{name: "case and spaces", sampler: " ParentBased_TraceIDRatio ", arg: " 0.1 ", want: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))},
		{name: "unknown sampler", sampler: "jaeger_remote", want: sdktrace.ParentBased(sdktrace.AlwaysSample())},
		{name: "arg not a number", sampler: "traceidratio", arg: "abc", want: sdktrace.TraceIDRatioBased(1)},
		{name: "arg negative", sampler: "traceidratio", arg: "-0.1", want: sdktrace.TraceIDRatioBased(1)},
		{name: "arg greater than one", sampler: "traceidratio", arg: "1.5", want: sdktrace.TraceIDRatioBased(1)},
		{name: "arg NaN", sampler: "traceidratio", arg: "NaN", want: sdktrace.TraceIDRatioBased(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tracesSamplerEnvName, tt.sampler)
			t.Setenv(tracesSamplerArgEnvName, tt.arg)
			assert.Equal(t, samplerFromEnv().Description(), tt.want.Description())
		})
	}
}

func TestNewSamplerChildFollowsParent(t *testing.T) {
	tests := []struct {
		name    string
		sampler string
		arg     string
		parent  trace.TraceFlags
		traceID trace.TraceID
		want    sdktrace.SamplingDecision
	}{
		// TraceIDRatioBased(0.5) drops this trace ID: bytes 8 to 15 are all 0xff.
		{name: "traceidratio keeps child of sampled parent", sampler: "traceidratio", arg: "0.5", parent: trace.FlagsSampled,
			traceID: trace.TraceID{8: 0xff, 9: 0xff, 10: 0xff, 11: 0xff, 12: 0xff, 13: 0xff, 14: 0xff, 15: 0xff}, want: sdktrace.RecordAndSample},
		// TraceIDRatioBased(0.5) keeps this trace ID: bytes 8 to 15 are all 0.
		{name: "traceidratio drops child of unsampled parent", sampler: "traceidratio", arg: "0.5", parent: 0, want: sdktrace.Drop},
		{name: "always_on drops child of unsampled parent", sampler: "always_on", parent: 0, want: sdktrace.Drop},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tracesSamplerEnvName, tt.sampler)
			t.Setenv(tracesSamplerArgEnvName, tt.arg)
			parent := testSpanContext(t, tt.parent)
			got := newSampler().ShouldSample(sdktrace.SamplingParameters{
				ParentContext: trace.ContextWithSpanContext(context.Background(), parent),
				TraceID:       tt.traceID,
			})
			assert.Equal(t, got.Decision, tt.want)
		})
	}
}

func TestNewSamplerAlwaysOffDropsEverySpan(t *testing.T) {
	sampled := testSpanContext(t, trace.FlagsSampled)
	tests := []struct {
		name   string
		parent trace.SpanContext
		links  []trace.Link
	}{
		// Allocate in a device plugin set to always_off, under a sampled Bind.
		{name: "drops child of sampled parent", parent: sampled},
		// Filter in a scheduler set to always_off, linked to a sampled admission span.
		{name: "drops span with sampled link", links: []trace.Link{{SpanContext: sampled}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tracesSamplerEnvName, "always_off")
			got := newSampler().ShouldSample(sdktrace.SamplingParameters{
				ParentContext: trace.ContextWithRemoteSpanContext(context.Background(), tt.parent),
				TraceID:       sampled.TraceID(),
				Links:         tt.links,
			})
			assert.Equal(t, got.Decision, sdktrace.Drop)
		})
	}
}

func TestLinksBasedSampler(t *testing.T) {
	sampled := testSpanContext(t, trace.FlagsSampled)
	notSampled := testSpanContext(t, 0)
	uid := []attribute.KeyValue{semconv.K8SPodUIDKey.String("6f1c2a4e-1b7d-4c3a-9e2f-0a1b2c3d4e5f")}

	tests := []struct {
		name   string
		root   sdktrace.Sampler
		parent trace.SpanContext
		links  []trace.Link
		attrs  []attribute.KeyValue
		want   sdktrace.SamplingDecision
	}{
		{name: "follows sampled link even if root drops", root: sdktrace.NeverSample(), links: []trace.Link{{SpanContext: sampled}}, want: sdktrace.RecordAndSample},
		{name: "follows unsampled link even if root samples", root: sdktrace.AlwaysSample(), links: []trace.Link{{SpanContext: notSampled}}, want: sdktrace.Drop},
		{name: "samples if any link is sampled", root: sdktrace.NeverSample(), links: []trace.Link{{SpanContext: notSampled}, {SpanContext: sampled}}, want: sdktrace.RecordAndSample},
		{name: "ignores invalid link and asks root", root: sdktrace.AlwaysSample(), links: []trace.Link{{}}, want: sdktrace.RecordAndSample},
		{name: "follows sampled parent", root: sdktrace.ParentBased(sdktrace.NeverSample()), parent: sampled, want: sdktrace.RecordAndSample},
		{name: "follows unsampled parent", root: sdktrace.ParentBased(sdktrace.AlwaysSample()), parent: notSampled, want: sdktrace.Drop},
		{name: "pod UID keeps always_off off", root: sdktrace.NeverSample(), attrs: uid, want: sdktrace.Drop},
		{name: "pod UID keeps always_on on", root: sdktrace.AlwaysSample(), attrs: uid, want: sdktrace.RecordAndSample},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := linksBasedSampler{root: tt.root}
			got := s.ShouldSample(sdktrace.SamplingParameters{
				ParentContext: trace.ContextWithRemoteSpanContext(context.Background(), tt.parent),
				TraceID:       sampled.TraceID(),
				Links:         tt.links,
				Attributes:    tt.attrs,
			})
			assert.Equal(t, got.Decision, tt.want)
		})
	}
}

func TestLinksBasedSamplerSameDecisionPerPod(t *testing.T) {
	s := linksBasedSampler{root: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.5))}
	attrs := []attribute.KeyValue{semconv.K8SPodUIDKey.String("6f1c2a4e-1b7d-4c3a-9e2f-0a1b2c3d4e5f")}

	want := s.ShouldSample(sdktrace.SamplingParameters{ParentContext: context.Background(), Attributes: attrs}).Decision
	for i := range 256 {
		// Each Filter attempt starts a new trace with a new random trace ID.
		// TraceIDRatioBased only reads bytes 8 to 15.
		traceID := trace.TraceID{8: byte(i)}
		got := s.ShouldSample(sdktrace.SamplingParameters{ParentContext: context.Background(), TraceID: traceID, Attributes: attrs})
		assert.Equal(t, got.Decision, want)
	}
}

func TestLinksBasedSamplerIgnoresEmptyPodUID(t *testing.T) {
	s := linksBasedSampler{root: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.5))}
	got := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: context.Background(),
		// TraceIDRatioBased(0.5) keeps this trace ID: bytes 8 to 15 are all 0.
		// It drops the hash of an empty UID: its bytes 8 to 15 are 9afbf4c8996fb924.
		TraceID:    trace.TraceID{},
		Attributes: []attribute.KeyValue{semconv.K8SPodUIDKey.String("")},
	})
	assert.Equal(t, got.Decision, sdktrace.RecordAndSample)
}

func TestLinksBasedSamplerPodUIDRatio(t *testing.T) {
	s := linksBasedSampler{root: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1))}
	r := rand.NewChaCha8([32]byte{})

	const pods = 10000
	traced := 0
	for range pods {
		uid, err := uuid.NewRandomFromReader(r)
		assert.NilError(t, err)
		attrs := []attribute.KeyValue{semconv.K8SPodUIDKey.String(uid.String())}
		if s.ShouldSample(sdktrace.SamplingParameters{ParentContext: context.Background(), Attributes: attrs}).Decision == sdktrace.RecordAndSample {
			traced++
		}
	}
	// 10% of 10,000 is 1,000; the seed is fixed, so the count does not change between runs.
	assert.Assert(t, traced > 900 && traced < 1100, "traced %d of %d pods", traced, pods)
}
