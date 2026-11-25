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
	"crypto/sha256"
	"fmt"
	"os"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"
)

const (
	tracesSamplerEnvName    = "OTEL_TRACES_SAMPLER"
	tracesSamplerArgEnvName = "OTEL_TRACES_SAMPLER_ARG"
)

// samplerFromEnv returns the sampler set by OTEL_TRACES_SAMPLER and
// OTEL_TRACES_SAMPLER_ARG. An unset or unknown sampler falls back to
// parentbased_always_on, and an unset or invalid ratio falls back to 1.
func samplerFromEnv() sdktrace.Sampler {
	defaultSampler := sdktrace.ParentBased(sdktrace.AlwaysSample())
	raw := os.Getenv(tracesSamplerEnvName)
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "parentbased_always_on":
		return defaultSampler
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(traceIDRatioFromEnv())
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(traceIDRatioFromEnv()))
	default:
		klog.Errorf("Invalid %s value %q, using default %v", tracesSamplerEnvName, raw, defaultSampler.Description())
		return defaultSampler
	}
}

// newSampler makes every span with a parent follow the parent's decision, so a
// trace is kept or dropped as a whole. Spans without a parent go to
// linksBasedSampler. always_off drops every span, even under a sampled parent
// or link.
func newSampler() sdktrace.Sampler {
	root := samplerFromEnv()
	if root.Description() == sdktrace.NeverSample().Description() {
		return root
	}
	return sdktrace.ParentBased(linksBasedSampler{root: root})
}

// traceIDRatioFromEnv returns OTEL_TRACES_SAMPLER_ARG, or 1 when it is unset or invalid.
func traceIDRatioFromEnv() float64 {
	ratio := 1.0
	raw := strings.TrimSpace(os.Getenv(tracesSamplerArgEnvName))
	if raw == "" {
		return ratio
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || !(parsed >= 0 && parsed <= 1) {
		klog.Errorf("Invalid %s value %q, using default %v", tracesSamplerArgEnvName, raw, ratio)
		return ratio
	}
	return parsed
}

// linksBasedSampler follows the sampled flag of a span's links, so Filter, which
// starts a new trace linked to the admission span, keeps the webhook's decision.
// Spans without links go to root.
//
// A root span without links that carries k8s.pod.uid is sampled on the pod UID
// instead of its random trace ID, so every Filter attempt of a pod without the
// annotation gets the same decision.
type linksBasedSampler struct {
	root sdktrace.Sampler
}

func (s linksBasedSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	psc := trace.SpanContextFromContext(p.ParentContext)
	hasLink, sampled := false, false
	for _, link := range p.Links {
		if link.SpanContext.IsValid() {
			hasLink = true
			sampled = sampled || link.SpanContext.IsSampled()
		}
	}
	if hasLink {
		decision := sdktrace.Drop
		if sampled {
			decision = sdktrace.RecordAndSample
		}
		return sdktrace.SamplingResult{Decision: decision, Tracestate: psc.TraceState()}
	}
	if !psc.IsValid() {
		if uid, ok := podUID(p.Attributes); ok {
			p.TraceID = uidTraceID(uid)
		}
	}
	return s.root.ShouldSample(p)
}

func (s linksBasedSampler) Description() string {
	return fmt.Sprintf("LinksBased{root:%s}", s.root.Description())
}

func podUID(attrs []attribute.KeyValue) (string, bool) {
	for _, kv := range attrs {
		if kv.Key == semconv.K8SPodUIDKey {
			uid := kv.Value.AsString()
			return uid, uid != ""
		}
	}
	return "", false
}

// uidTraceID hashes the pod UID because the fixed bits of a UUID would skew TraceIDRatioBased.
func uidTraceID(uid string) trace.TraceID {
	sum := sha256.Sum256([]byte(uid))
	var id trace.TraceID
	copy(id[:], sum[:])
	return id
}
