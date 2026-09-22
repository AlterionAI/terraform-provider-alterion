@.claude/alterion-standards.md

# CLAUDE.md

This repo follows the Alterion engineering standards imported above (PR
workflow, CI gates, multi-agent coordination, merge flow, cost discipline).
This file holds only what's specific to `terraform-provider-alterion`.

## What this is

A Terraform provider (`terraform-plugin-framework`) for Alterion Orion. It
talks to Orion web app REST routes under `/api/v1/agents/asserted/*` to
resolve an agent's deterministic gateway path key ahead of runtime
deployment (`alterion_agent_path_key` data source), and to register/update/
delete the asserted agent row itself (`alterion_agent` resource). See
`examples/agentcore/README.md` for the compute-early/register-late pattern
this exists to support.

## Go specifics

- `gofmt -l .` must be empty before any commit.
- `go vet ./...` and `go build ./...` must pass.
- Table-driven tests (`internal/client/client_test.go` is the reference) —
  prefer one `httptest.Server` per behavior over one giant fixture.
- No network access in unit tests (`internal/client`) — everything goes
  through `httptest`. Acceptance-style tests in `internal/provider` use
  `terraform-plugin-testing`'s `resource.Test` against an in-process fake
  Orion server (`httptest`), never a real Orion deployment; they require
  `TF_ACC=1` and a `terraform` binary on `PATH` (or `TF_ACC_TERRAFORM_PATH`)
  to run, same as any `terraform-plugin-framework` provider.
- `internal/client/contract_test.go` pins the short-id derivation
  (`sha256("asserted|<env>|<identityKey>")[:12]`, where `identityKey` is
  `"<cloudProvider>.<cloudAccountId>.<cloudRegion>.<workloadName>"` — see
  `client.IdentityKey`) to golden vectors from the Orion server. If Orion's
  derivation ever changes, this test's failure is the signal — update the
  vectors together with a coordinated Orion change.

## Local build/test

```
go build ./...
go vet ./...
gofmt -l .            # must print nothing
go test ./...                                    # unit tests only (internal/client + provider validators)
TF_ACC=1 go test ./... -run TestAcc -v            # + acceptance-style tests
UPDATE_SNAPSHOT=1 go test ./internal/provider/... -run TestSchemaSnapshot   # after an intentional schema change
```

Or via the Makefile: `make lint test testacc snapshot docs`. See README
"Testing" for the four coverage layers (client unit / validator unit /
in-process acceptance / opt-in live) and what each CI job in
`.github/workflows/ci.yml` + `nightly-live.yml` checks.

## Module path & registry address

- Go module: `github.com/AlterionAI/terraform-provider-alterion`
- Terraform registry address: `registry.terraform.io/alterion/alterion`
- Resource/data source prefix: `alterion_`
