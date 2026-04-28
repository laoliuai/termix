.PHONY: generate test-go fmt-go build-web check-web-dist check-web-dist-clean build-go build smoke web-dev web-test

generate:
	@if [ -f openapi/control.openapi.yaml ]; then \
		command -v oapi-codegen >/dev/null 2>&1 || { echo "oapi-codegen binary is required"; exit 1; }; \
		mkdir -p go/gen/openapi; \
		cd go && oapi-codegen -generate types,client,gin,spec -package openapi -o gen/openapi/control.gen.go ../openapi/control.openapi.yaml; \
	else \
		echo "Skipping OpenAPI generation: openapi/control.openapi.yaml not found"; \
	fi
	@if [ -f go/sqlc.yaml ] && [ -d go/sql/queries ]; then \
		command -v sqlc >/dev/null 2>&1 || { echo "sqlc binary is required"; exit 1; }; \
		cd go && sqlc generate -f sqlc.yaml; \
	else \
		echo "Skipping sqlc generation: go/sqlc.yaml or go/sql/queries missing"; \
	fi
	@PROTO_INPUTS=""; \
	for proto_file in proto/daemon.proto proto/relay_control.proto; do \
		if [ -f "$$proto_file" ]; then \
			PROTO_INPUTS="$$PROTO_INPUTS $$proto_file"; \
		fi; \
	done; \
	if [ -n "$$PROTO_INPUTS" ]; then \
		command -v protoc >/dev/null 2>&1 || { echo "protoc binary is required"; exit 1; }; \
		mkdir -p go/gen/proto; \
		protoc --go_out=go --go_opt=module=github.com/termix/termix/go --go-grpc_out=go --go-grpc_opt=module=github.com/termix/termix/go -I proto $$PROTO_INPUTS; \
	else \
		echo "Skipping proto generation: no supported proto inputs found (proto/daemon.proto proto/relay_control.proto)"; \
	fi

test-go:
	cd go && go test ./...

fmt-go:
	cd go && gofmt -w ./cmd ./internal ./tests

# --- Web UI targets ---

web-dev:
	cd web/app && npm run dev

web-test:
	cd web/app && npm test -- --run

build-web:
	cd web/app && npm install && npm run build
	rsync -a --delete --exclude .gitignore web/app/dist/ go/internal/controlapi/web_dist/

check-web-dist:
	node web/app/scripts/check-web-dist.mjs

check-web-dist-clean:
	node web/app/scripts/check-web-dist.mjs --tracked

build-go: build-web
	$(MAKE) check-web-dist
	cd go && go build -o bin/termix-control ./cmd/termix-control
	cd go && go build -o bin/termix-relay   ./cmd/termix-relay
	cd go && go build -o bin/termixd        ./cmd/termixd
	cd go && go build -o bin/termix         ./cmd/termix

build: build-web build-go

smoke:
	./web/app/scripts/smoke.sh
