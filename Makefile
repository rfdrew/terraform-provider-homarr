.DEFAULT_GOAL := help
.PHONY: help build install test testacc docs fmt vet lint homarr-up homarr-down clean

BINARY  := terraform-provider-homarr
VERSION ?= dev

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## Build the provider binary
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) .

install: ## Build and install into the Go bin directory
	go install -ldflags "-X main.version=$(VERSION)" .

fmt: ## Format Go and Terraform sources
	gofmt -w .
	@command -v terraform >/dev/null && terraform fmt -recursive ./examples || true

vet: ## Run go vet
	go vet ./...

lint: fmt vet ## Format and vet

test: ## Run unit tests (no Homarr instance needed)
	go test ./internal/client/ -v

# Acceptance tests create and destroy real objects. Point them at a throwaway
# instance -- `make homarr-up` prints the exports you need.
testacc: ## Run acceptance tests (requires HOMARR_URL and HOMARR_API_KEY)
	TF_ACC=1 go test ./... -v -timeout 30m

homarr-up: ## Boot a throwaway Homarr and print HOMARR_URL/HOMARR_API_KEY exports
	@./scripts/bootstrap-test-homarr.sh

homarr-down: ## Remove the throwaway Homarr container
	@./scripts/bootstrap-test-homarr.sh --teardown

docs: ## Regenerate docs/ from the schemas and examples
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@latest generate \
		--provider-name homarr \
		--rendered-provider-name Homarr

clean: ## Remove build artefacts
	rm -f $(BINARY)
