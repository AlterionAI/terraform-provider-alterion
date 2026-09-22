.PHONY: build test testacc lint fmt docs snapshot clean

build:
	go build -o terraform-provider-alterion .

# Unit tests only (internal/client + provider validators): no network, no
# terraform binary required.
test:
	go test ./... -race -v

# Acceptance-style tests (internal/provider) against an in-process fake
# Orion server. Requires a `terraform` binary on PATH (or
# TF_ACC_TERRAFORM_PATH); never talks to a real Orion deployment.
testacc:
	TF_ACC=1 go test ./internal/provider/... -run TestAcc -v -timeout 20m

# Live acceptance tests against a real Orion deployment. Opt-in: requires
# TF_ACC_LIVE=1 plus ALTERION_ORION_URL / ALTERION_API_TOKEN /
# ALTERION_TEST_ACCOUNT_ID / ALTERION_TEST_REGION. See README "Testing".
testacc-live:
	TF_ACC_LIVE=1 go test ./internal/acceptance_live/... -v -timeout 20m

lint:
	gofmt -l . | tee /tmp/gofmt-out; test ! -s /tmp/gofmt-out
	go vet ./...
	golangci-lint run ./...

fmt:
	gofmt -w .
	terraform fmt -recursive examples/

docs:
	tfplugindocs generate --provider-name alterion

# Regenerate testdata/schema.json after an intentional schema change.
snapshot:
	UPDATE_SNAPSHOT=1 go test ./internal/provider/... -run TestSchemaSnapshot -v

clean:
	rm -f terraform-provider-alterion
