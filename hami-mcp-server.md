# HAMi MCP Server — Full Execution Plan

**Status:** Draft v1
**Owner:** Tim
**Goal:** Add a Model Context Protocol (MCP) server to HAMi that exposes read-only GPU/scheduler state to LLM clients (Claude Desktop, Claude Code, Cursor, etc.), with full E2E test coverage.

---

## 0. Scope and Decisions (commit before coding)

### 0.1 Audience
- **Primary:** Cluster operators debugging GPU scheduling issues with an AI assistant
- **Secondary:** Developers building tooling around HAMi
- **NOT in scope:** End users requesting GPU resources (they use kubectl)

### 0.2 Capability Boundary — read-only, period
| Allowed | Forbidden |
|---|---|
| List GPU nodes / pods / quotas | Bind pods, mutate webhook decisions |
| Read Prometheus metrics | Trigger device plugin allocation |
| Describe HAMi config | Modify HAMi config or K8s resources |

### 0.3 Architecture Decision
**Chosen:** standalone binary `cmd/mcp-server` that talks to the K8s API and Prometheus only — treats HAMi as a black box.
**Rejected:** embedding into scheduler binary (couples release cadence) or sidecar (extra deployment complexity for v1).

### 0.4 Transport
**v1:** stdio (local Claude Desktop usage)
**v2:** SSE/HTTP (in-cluster Service exposed via Ingress + auth) — separate plan

### 0.5 SDK
**Decision deferred to Phase 1.** Candidates:
- `github.com/mark3labs/mcp-go`
- `github.com/modelcontextprotocol/go-sdk` (official)
- `github.com/metoro-io/mcp-golang`

### 0.6 Upstream policy
HAMi requires AI-assistance disclosure in PRs (per repo CONTRIBUTING). This feature is a candidate for an upstream RFC issue **before** merging — see Phase 0.

---

## Phase 0 — RFC and SDK Selection

### 0.A Open upstream RFC
- File: `docs/proposals/hami-mcp-server.md`
- Sections: motivation, non-goals, alternatives (kubectl plugin, custom Grafana dashboard), security model, migration path
- Open GitHub issue linking the proposal; tag maintainers; **do not start implementation until at least one maintainer comments**

### 0.B SDK comparison spike
For each SDK, build a 50-line "hello world" MCP server that exposes a `ping` tool. Score on:
| Criterion | Weight |
|---|---|
| K8s controller-runtime / client-go compatibility (no version conflicts) | High |
| stdio + SSE transport in same SDK | High |
| Active maintenance (last commit < 60 days) | High |
| License (Apache-2.0 or MIT preferred — matches HAMi) | High |
| Tool/resource/prompt API ergonomics | Medium |
| Spec compliance test results | Medium |

**Output:** `docs/proposals/hami-mcp-server.md` updated with SDK choice + rationale.

### 0.C Verification
- [ ] RFC merged or has maintainer "go-ahead" comment
- [ ] SDK spike repos archived under `experimental/mcp-spike/` (git ignored or deleted before final PR)
- [ ] One SDK chosen and documented

---

## Phase 1 — Project Skeleton

### 1.A Directory layout
```
cmd/mcp-server/
  main.go                      # Entrypoint — stdio transport
  flags.go                     # CLI flags (kubeconfig, prometheus URL, log level)
pkg/mcp/
  server.go                    # Server constructor, tool/resource registration
  client/
    k8s.go                     # Wraps client-go: list nodes, pods, configmaps
    prometheus.go              # Prometheus HTTP client (read-only)
  tools/
    list_gpu_nodes.go
    list_gpu_pods.go
    get_quota_usage.go
    get_gpu_metrics.go
    describe_node.go
  resources/
    config.go                  # Exposes HAMi ConfigMap as MCP resource
  redact/
    redact.go                  # Strips secrets/env vars from responses
charts/hami/templates/mcp-server-rbac.yaml  # ServiceAccount + ClusterRole (optional)
docker/Dockerfile.mcp          # Minimal scratch+distroless image
docs/mcp-server.md             # User-facing docs
docs/mcp-server_cn.md          # Chinese version (HAMi convention)
```

### 1.B Build wiring
- Add `mcp-server` to a **separate** make target, not `CMDS`:
  ```makefile
  .PHONY: mcp-server
  mcp-server:
      $(GO) build -ldflags '$(GO_BUILD_LDFLAGS)' -o ${OUTPUT_DIR}/mcp-server ./cmd/mcp-server
  ```
- Reason: keep experimental binary off the default build until upstream accepts it.

### 1.C Skeleton verification
- [ ] `make mcp-server` produces `bin/mcp-server`
- [ ] `./bin/mcp-server --help` prints flags
- [ ] `echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' | ./bin/mcp-server` returns a valid MCP `InitializeResult`
- [ ] `go vet ./cmd/mcp-server/... ./pkg/mcp/...` passes
- [ ] `golangci-lint run ./cmd/mcp-server/... ./pkg/mcp/...` clean

---

## Phase 2 — Read-only Tools

### 2.A Tool catalogue (v1)

| Tool name | Input schema | Output |
|---|---|---|
| `list_gpu_nodes` | `{ "labelSelector"?: string }` | Array of `{ name, gpuVendor, gpuCount, allocatedMemoryGB, totalMemoryGB, allocatedCoresPct }` |
| `list_gpu_pods` | `{ "namespace"?: string, "phase"?: "Running"\|"Pending" }` | Array of `{ namespace, name, node, requestedGPU, allocatedDeviceUUIDs, status }` |
| `get_quota_usage` | `{ "namespace": string }` | `{ namespace, gpuMemoryUsed, gpuMemoryQuota, gpuCoreUsed, gpuCoreQuota }` |
| `get_gpu_metrics` | `{ "metric": string, "node"?: string }` | Time-series snapshot (last value) for one of the documented HAMi Prometheus metrics |
| `describe_node` | `{ "node": string }` | Full HAMi annotation dump + GPU device list + per-GPU usage |

### 2.B Implementation rules
- All tools call only `pkg/mcp/client/k8s.go` or `pkg/mcp/client/prometheus.go`
- No tool may import `pkg/scheduler/*` directly (avoids coupling to in-process scheduler state)
- Every tool runs response through `redact.Redact()` before returning
- Errors return MCP `content` with `isError: true` — never panic the server

### 2.C Per-tool verification
For each tool:
- [ ] `tools/list` JSON-RPC call lists it with correct schema
- [ ] `tools/call` with valid input returns content
- [ ] `tools/call` with invalid input returns `isError: true`, no stack trace
- [ ] Unit test in `pkg/mcp/tools/<tool>_test.go` mocks the client and asserts JSON shape

### 2.D Anti-patterns
- ❌ `os.Getenv("KUBECONFIG")` inside tool handlers → use injected client
- ❌ Returning raw pod env vars (may contain secrets) → redact
- ❌ Calling `pkg/scheduler/scheduler.go` Filter/Bind methods
- ❌ Adding any tool whose name contains `bind`, `allocate`, `mutate`, `apply`, `delete`, `create`

---

## Phase 3 — RBAC and Security

### 3.A RBAC manifest
`charts/hami/templates/mcp-server-rbac.yaml` (only rendered if `mcpServer.enabled=true`):

```yaml
# ServiceAccount: hami-mcp-server
# ClusterRole verbs (HARD LIMIT):
#   - get/list/watch on: nodes, pods, namespaces, configmaps (hami-scheduler-config only)
#   - NO write verbs anywhere
#   - NO get on secrets
```

Verification:
- [ ] `kubectl auth can-i create pods --as=system:serviceaccount:hami:hami-mcp-server` → **no**
- [ ] `kubectl auth can-i get secrets --as=system:serviceaccount:hami:hami-mcp-server` → **no**
- [ ] `kubectl auth can-i list nodes --as=system:serviceaccount:hami:hami-mcp-server` → **yes**

### 3.B Output redaction
`pkg/mcp/redact/redact.go` strips:
- All container `env` entries whose name matches `(?i)(token|secret|password|key|cred)`
- All `metadata.annotations` whose key matches the same pattern
- All `imagePullSecrets`
- Pod `spec.volumes[*].secret`

Unit test: feed pod with secrets, assert none leak in output.

### 3.C Security verification
- [ ] `gosec ./pkg/mcp/...` — no high-severity findings
- [ ] Grep guard in CI: `! grep -rE '(\.Update|\.Create|\.Delete|\.Patch)\(' pkg/mcp/ cmd/mcp-server/`
- [ ] No file writes by the server: `! grep -r 'os.WriteFile\|os.Create' pkg/mcp/ cmd/mcp-server/`

---

## Phase 4 — Container, Helm, Docs

### 4.A Dockerfile
`docker/Dockerfile.mcp` — multi-stage:
- Builder: `${GOLANG_IMAGE}` runs `make mcp-server`
- Runtime: `gcr.io/distroless/static:nonroot`, copies `/bin/mcp-server`, `USER nonroot`
- No nvidia base needed (server has no GPU calls)

Makefile target:
```makefile
docker-mcp:
    docker build \
    --platform ${TARGET_PLATFORMS} \
    --build-arg GOLANG_IMAGE=${GOLANG_IMAGE} \
    --build-arg VERSION=${VERSION} \
    . -f=docker/Dockerfile.mcp -t ${IMG_NAME}-mcp:${IMG_TAG}
```

### 4.B Helm
- Add `mcpServer:` block in `charts/hami/values.yaml` (default: `enabled: false`)
- New templates only when enabled: `Deployment`, `ServiceAccount`, `ClusterRole`, `ClusterRoleBinding`
- No `Service` for v1 (stdio only — pod runs but kubectl exec'd in)

### 4.C Documentation
`docs/mcp-server.md` must contain:
1. What is exposed (tool list with examples)
2. Security model (read-only, RBAC, redaction)
3. Local usage: Claude Desktop config snippet
4. In-cluster usage: `kubectl exec -it deploy/hami-mcp-server -- /bin/mcp-server`
5. Limitations: stdio only in v1, not for end-user GPU requests
6. Troubleshooting: common errors and fixes

Bilingual: also `docs/mcp-server_cn.md`.

### 4.D Verification
- [ ] `make docker-mcp` produces image, `docker run --rm <image> --help` works
- [ ] `helm template charts/hami --set mcpServer.enabled=true | kubectl apply --dry-run=server -f -` succeeds
- [ ] `helm template charts/hami` (default) produces zero MCP resources (off by default)

---

## Phase 5 — E2E Tests

### 5.A Test harness layout
Mirror existing `test/e2e/{node,pod}` style:
```
test/e2e/mcp/
  mcp_suite_test.go         # Ginkgo suite root
  fixtures/
    test-pod-gpu.yaml       # Pod that requests nvidia.com/gpumem
    test-pod-no-gpu.yaml
  client/
    mcp_client.go           # Spawns mcp-server binary, speaks JSON-RPC over stdio
  tests/
    initialize_test.go
    list_gpu_nodes_test.go
    list_gpu_pods_test.go
    get_quota_usage_test.go
    get_gpu_metrics_test.go
    describe_node_test.go
    rbac_test.go
    redaction_test.go
    error_handling_test.go
```

### 5.B Test client (the key piece)
`test/e2e/mcp/client/mcp_client.go` — minimal MCP test client:

```go
// Public API the tests will use:
type MCPTestClient struct { /* ... */ }

func NewMCPTestClient(t *testing.T, kubeconfig string) *MCPTestClient
func (c *MCPTestClient) Initialize() error
func (c *MCPTestClient) ListTools() ([]Tool, error)
func (c *MCPTestClient) CallTool(name string, args map[string]any) (*CallToolResult, error)
func (c *MCPTestClient) Close() error
```

Internally:
1. Forks `bin/mcp-server` with `--kubeconfig=$KUBE_CONF`
2. Pipes stdin/stdout for JSON-RPC line framing
3. Handles `initialize` handshake automatically in `NewMCPTestClient`
4. Tears down process in `Close()`

### 5.C Test scenarios — required coverage

#### S1: Initialize handshake
- [ ] Server responds to `initialize` with valid `serverInfo` and `capabilities.tools = {}`
- [ ] Server rejects request without prior initialize

#### S2: list_gpu_nodes
- Setup: cluster has ≥1 node with `nvidia.com/gpu` capacity
- [ ] Returns at least 1 node
- [ ] Each node has non-empty `name`, `gpuVendor`
- [ ] `labelSelector` filter narrows results
- [ ] Empty cluster returns `[]`, not error

#### S3: list_gpu_pods
- Setup: deploy `test-pod-gpu.yaml` and `test-pod-no-gpu.yaml`
- [ ] Returns the GPU pod, omits the no-GPU pod
- [ ] `phase: "Pending"` filter works when GPU pod is unschedulable (e.g., on a no-GPU cluster)
- [ ] `namespace` filter scopes correctly

#### S4: get_quota_usage
- Setup: create namespace with HAMi quota annotation; deploy pod consuming part of quota
- [ ] Returns numeric used + quota
- [ ] Used ≤ quota
- [ ] Unknown namespace returns clean error, not panic

#### S5: get_gpu_metrics
- Setup: HAMi scheduler running; Prometheus reachable (or stub HTTP server in test)
- [ ] Known metric (`hami_gpu_memory_allocated_bytes`) returns a value
- [ ] Unknown metric returns `isError: true` with helpful message
- [ ] Filter by `node` works

#### S6: describe_node
- [ ] Existing GPU node returns annotations + device list
- [ ] Non-existent node returns clean error
- [ ] Annotation values do not contain raw secrets

#### S7: RBAC enforcement (in-cluster mode)
- Setup: deploy MCP server with `hami-mcp-server` SA
- [ ] `list_gpu_nodes` succeeds (RBAC allows)
- [ ] Custom test tool that tries `client.CoreV1().Secrets("default").List()` returns 403 (RBAC denies) — proves RBAC binding is correct

#### S8: Redaction
- Setup: pod with env var `MY_SECRET_TOKEN=hunter2`
- [ ] `list_gpu_pods` response does not contain string `hunter2`
- [ ] Same for annotation `secret-key: hunter2`

#### S9: Error handling and resilience
- [ ] Invalid JSON over stdio → server returns JSON-RPC parse error and stays alive
- [ ] Tool with missing required arg → returns `isError: true`, server stays alive
- [ ] K8s API unreachable (kill apiserver port-forward) → tool returns clean error, server stays alive
- [ ] Server exits cleanly on stdin EOF

#### S10: Concurrency
- [ ] 10 parallel `tools/call` requests all complete
- [ ] No race detected: tests run with `-race`

### 5.D E2E entrypoint
Add to `hack/e2e-test.sh`:
```bash
case "$E2E_TYPE" in
  mcp)
    go test -race -timeout 30m ./test/e2e/mcp/... -v
    ;;
esac
```

Add Makefile target:
```makefile
.PHONY: e2e-test-mcp
e2e-test-mcp:
    E2E_TYPE=mcp ./hack/e2e-test.sh "${E2E_TYPE}" "${KUBE_CONF}"
```

### 5.E E2E verification
- [ ] All 10 scenarios green on a kind cluster with HAMi installed
- [ ] All 10 scenarios green on a real GPU cluster (operator validation)
- [ ] Test run produces JUnit XML: `make e2e-test-mcp 2>&1 | tee mcp-e2e.log`
- [ ] No flakiness: 5 consecutive passing runs required to merge

---

## Phase 6 — CI Integration

### 6.A GitHub Actions
Add `.github/workflows/mcp-e2e.yml`:
- Trigger: PR touching `cmd/mcp-server/**`, `pkg/mcp/**`, `test/e2e/mcp/**`, or `docs/mcp-server*.md`
- Steps: setup-go → kind cluster → install HAMi via helm → `make e2e-test-mcp`
- Required to pass before merge

### 6.B Lint guards
Append to `hack/verify-staticcheck.sh`:
```bash
# Forbid write verbs in mcp packages
if grep -rE '\.(Update|Create|Delete|Patch|Apply)\(' pkg/mcp/ cmd/mcp-server/ 2>/dev/null; then
    echo "ERROR: mcp packages must not call mutating K8s APIs"
    exit 1
fi
```

### 6.C Verification
- [ ] PR with mutating call in `pkg/mcp/` fails CI
- [ ] PR without mcp-server changes does not trigger mcp-e2e.yml
- [ ] CI green on the implementation PR

---

## Phase 7 — Final Verification and Release

### 7.A Definition of Done checklist
- [ ] RFC merged
- [ ] SDK choice documented in proposals
- [ ] All Phase 1–4 verification items checked
- [ ] All 10 E2E scenarios passing 5 consecutive runs
- [ ] CI guards active
- [ ] `docs/mcp-server.md` and `_cn.md` written and reviewed
- [ ] `gosec` clean
- [ ] No `kubectl auth can-i` write verb returns `yes` for the SA
- [ ] PR description explicitly discloses AI assistance (per HAMi policy)

### 7.B Release path
- Image tag: `${IMG_NAME}-mcp:${IMG_TAG}`
- Helm flag: `mcpServer.enabled=false` (off by default for safety in v1.X)
- Ship under `experimental` label in release notes
- Promotion to GA after one minor version of operator feedback

### 7.C Rollback plan
- Feature is opt-in via `mcpServer.enabled`; rollback = set to `false` and reapply chart
- No data migrations, no schema changes, no scheduler changes — rollback is trivial

---

## Appendix A — File-by-file deliverable list

| Path | Purpose | Phase |
|---|---|---|
| `docs/proposals/hami-mcp-server.md` | RFC | 0 |
| `cmd/mcp-server/main.go` | Entrypoint | 1 |
| `cmd/mcp-server/flags.go` | CLI flags | 1 |
| `pkg/mcp/server.go` | Server constructor | 1 |
| `pkg/mcp/client/k8s.go` | K8s client wrapper | 1 |
| `pkg/mcp/client/prometheus.go` | Prometheus client | 1 |
| `pkg/mcp/tools/*.go` (5 files) | Tool implementations | 2 |
| `pkg/mcp/tools/*_test.go` (5 files) | Unit tests | 2 |
| `pkg/mcp/redact/redact.go` + `_test.go` | Redaction | 3 |
| `charts/hami/templates/mcp-server-*.yaml` | Helm | 4 |
| `docker/Dockerfile.mcp` | Container | 4 |
| `docs/mcp-server.md` + `_cn.md` | User docs | 4 |
| `test/e2e/mcp/client/mcp_client.go` | Test client | 5 |
| `test/e2e/mcp/tests/*.go` (10 files) | E2E scenarios | 5 |
| `test/e2e/mcp/fixtures/*.yaml` | Test pods | 5 |
| `.github/workflows/mcp-e2e.yml` | CI | 6 |
| Updates to `hack/e2e-test.sh`, `hack/verify-staticcheck.sh`, `Makefile` | Wiring | 4–6 |

---

## Appendix B — Anti-pattern Checklist (paste into PR review)

- [ ] No `Update/Create/Delete/Patch` calls in `pkg/mcp/` or `cmd/mcp-server/`
- [ ] No imports of `pkg/scheduler/scheduler.go` from MCP packages
- [ ] No tools named `bind`, `allocate`, `mutate`, `apply`, `delete`, `create`
- [ ] No raw pod env vars or annotations leaked through tool output
- [ ] RBAC has zero write verbs and zero `secrets` access
- [ ] Default Helm value is `mcpServer.enabled: false`
- [ ] Server stays alive on bad input — never panics
- [ ] All errors returned as MCP `isError: true`, not stack traces
- [ ] Plan-master decision (read-only, stdio-first) reflected in code

---

## Appendix C — Out of scope (explicit non-goals for v1)

- SSE/HTTP transport (Phase 8 in a future plan)
- Authentication beyond ServiceAccount RBAC (no OIDC, no per-user scoping)
- Write operations of any kind
- Multi-tenant request scoping (each MCP session sees full cluster view per RBAC)
- Streaming long-running tool results
- Caching layer (each call hits live K8s/Prometheus)
- Custom MCP prompts (only tools + resources in v1)
