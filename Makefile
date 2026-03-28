IMG ?= ghcr.io/ai-volund/volund-operator:latest
BIN = bin/volund-operator

.PHONY: build test lint docker clean install uninstall deploy undeploy manifests

build:
	go build -o $(BIN) ./cmd/operator

test:
	go test ./...

lint:
	golangci-lint run ./...

docker:
	docker build -t $(IMG) .

clean:
	rm -rf bin/

##@ Deployment

install: ## Install CRDs into the K8s cluster
	kubectl apply -k config/crd

uninstall: ## Uninstall CRDs from the K8s cluster
	kubectl delete -k config/crd

deploy: ## Deploy operator to the K8s cluster
	kubectl apply -k config/default

undeploy: ## Undeploy operator from the K8s cluster
	kubectl delete -k config/default

manifests: ## Generate manifests (placeholder — CRDs maintained manually)
	@echo "CRDs maintained manually — see config/crd/bases/"
