.PHONY: generate test-go fmt-go

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
