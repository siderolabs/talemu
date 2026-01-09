# syntax = docker/dockerfile-upstream:1.20.0-labs

# THIS FILE WAS AUTOMATICALLY GENERATED, PLEASE DO NOT EDIT.
#
# Generated on 2026-01-09T15:19:02Z by kres 0e8da31.

ARG TOOLCHAIN=scratch

FROM ghcr.io/siderolabs/ca-certificates:v1.12.0 AS image-ca-certificates

FROM ghcr.io/siderolabs/fhs:v1.12.0 AS image-fhs

# runs markdownlint
FROM docker.io/oven/bun:1.3.5-alpine AS lint-markdown
WORKDIR /src
RUN bun i markdownlint-cli@0.47.0 sentences-per-line@0.5.0
COPY .markdownlint.json .
COPY ./README.md ./README.md
RUN bunx markdownlint --ignore "CHANGELOG.md" --ignore "**/node_modules/**" --ignore '**/hack/chglog/**' --rules markdownlint-sentences-per-line .

# collects proto specs
FROM scratch AS proto-specs
ADD api/specs/specs.proto /api/specs/

# base toolchain image
FROM --platform=${BUILDPLATFORM} ${TOOLCHAIN} AS toolchain
RUN apk --update --no-cache add bash build-base curl jq protoc protobuf-dev

# build tools
FROM --platform=${BUILDPLATFORM} toolchain AS tools
ENV GO111MODULE=on
ARG CGO_ENABLED
ENV CGO_ENABLED=${CGO_ENABLED}
ARG GOTOOLCHAIN
ENV GOTOOLCHAIN=${GOTOOLCHAIN}
ARG GOEXPERIMENT
ENV GOEXPERIMENT=${GOEXPERIMENT}
ENV GOPATH=/go
ARG GOIMPORTS_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go install golang.org/x/tools/cmd/goimports@v${GOIMPORTS_VERSION}
RUN mv /go/bin/goimports /bin
ARG GOMOCK_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go install go.uber.org/mock/mockgen@v${GOMOCK_VERSION}
RUN mv /go/bin/mockgen /bin
ARG PROTOBUF_GO_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go install google.golang.org/protobuf/cmd/protoc-gen-go@v${PROTOBUF_GO_VERSION}
RUN mv /go/bin/protoc-gen-go /bin
ARG GRPC_GO_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v${GRPC_GO_VERSION}
RUN mv /go/bin/protoc-gen-go-grpc /bin
ARG GRPC_GATEWAY_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@v${GRPC_GATEWAY_VERSION}
RUN mv /go/bin/protoc-gen-grpc-gateway /bin
ARG VTPROTOBUF_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go install github.com/planetscale/vtprotobuf/cmd/protoc-gen-go-vtproto@v${VTPROTOBUF_VERSION}
RUN mv /go/bin/protoc-gen-go-vtproto /bin
ARG DEEPCOPY_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go install github.com/siderolabs/deep-copy@${DEEPCOPY_VERSION} \
	&& mv /go/bin/deep-copy /bin/deep-copy
ARG GOLANGCILINT_VERSION
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCILINT_VERSION} \
	&& mv /go/bin/golangci-lint /bin/golangci-lint
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go install golang.org/x/vuln/cmd/govulncheck@latest \
	&& mv /go/bin/govulncheck /bin/govulncheck
ARG GOFUMPT_VERSION
RUN go install mvdan.cc/gofumpt@${GOFUMPT_VERSION} \
	&& mv /go/bin/gofumpt /bin/gofumpt

# tools and sources
FROM tools AS base
WORKDIR /src
COPY go.mod go.mod
COPY go.sum go.sum
RUN cd .
RUN --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go mod download
RUN --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go mod verify
COPY ./api ./api
COPY ./cmd ./cmd
COPY ./internal ./internal
RUN --mount=type=cache,target=/go/pkg,id=talemu/go/pkg go list -mod=readonly all >/dev/null

# runs protobuf compiler
FROM tools AS proto-compile
COPY --from=proto-specs / /
RUN protoc -I/api --go_out=paths=source_relative:/api --go-grpc_out=paths=source_relative:/api --go-vtproto_out=paths=source_relative:/api --go-vtproto_opt=features=marshal+unmarshal+size+equal+clone /api/specs/specs.proto
RUN rm /api/specs/specs.proto
RUN goimports -w -local github.com/siderolabs/talemu /api
RUN gofumpt -w /api

# runs gofumpt
FROM base AS lint-gofumpt
RUN FILES="$(gofumpt -l .)" && test -z "${FILES}" || (echo -e "Source code is not formatted with 'gofumpt -w .':\n${FILES}"; exit 1)

# runs golangci-lint
FROM base AS lint-golangci-lint
WORKDIR /src
COPY .golangci.yml .
ENV GOGC=50
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/root/.cache/golangci-lint,id=talemu/root/.cache/golangci-lint,sharing=locked --mount=type=cache,target=/go/pkg,id=talemu/go/pkg golangci-lint run --config .golangci.yml

# runs golangci-lint fmt
FROM base AS lint-golangci-lint-fmt-run
WORKDIR /src
COPY .golangci.yml .
ENV GOGC=50
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/root/.cache/golangci-lint,id=talemu/root/.cache/golangci-lint,sharing=locked --mount=type=cache,target=/go/pkg,id=talemu/go/pkg golangci-lint fmt --config .golangci.yml
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/root/.cache/golangci-lint,id=talemu/root/.cache/golangci-lint,sharing=locked --mount=type=cache,target=/go/pkg,id=talemu/go/pkg golangci-lint run --fix --issues-exit-code 0 --config .golangci.yml

# runs govulncheck
FROM base AS lint-govulncheck
WORKDIR /src
COPY --chmod=0755 hack/govulncheck.sh ./hack/govulncheck.sh
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg ./hack/govulncheck.sh ./...

# runs unit-tests with race detector
FROM base AS unit-tests-race
WORKDIR /src
ARG TESTPKGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg --mount=type=cache,target=/tmp,id=talemu/tmp CGO_ENABLED=1 go test -race ${TESTPKGS}

# runs unit-tests
FROM base AS unit-tests-run
WORKDIR /src
ARG TESTPKGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg --mount=type=cache,target=/tmp,id=talemu/tmp go test -covermode=atomic -coverprofile=coverage.txt -coverpkg=${TESTPKGS} ${TESTPKGS}

# cleaned up specs and compiled versions
FROM scratch AS generate
COPY --from=proto-compile /api/ /api/

# clean golangci-lint fmt output
FROM scratch AS lint-golangci-lint-fmt
COPY --from=lint-golangci-lint-fmt-run /src .

FROM scratch AS unit-tests
COPY --from=unit-tests-run /src/coverage.txt /coverage-unit-tests.txt

# builds talemu-darwin-amd64
FROM base AS talemu-darwin-amd64-build
COPY --from=generate / /
WORKDIR /src/cmd/talemu
ARG GO_BUILDFLAGS
ARG GO_LDFLAGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg GOARCH=amd64 GOOS=darwin go build ${GO_BUILDFLAGS} -ldflags "${GO_LDFLAGS}" -o /talemu-darwin-amd64

# builds talemu-darwin-arm64
FROM base AS talemu-darwin-arm64-build
COPY --from=generate / /
WORKDIR /src/cmd/talemu
ARG GO_BUILDFLAGS
ARG GO_LDFLAGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg GOARCH=arm64 GOOS=darwin go build ${GO_BUILDFLAGS} -ldflags "${GO_LDFLAGS}" -o /talemu-darwin-arm64

# builds talemu-infra-provider-darwin-amd64
FROM base AS talemu-infra-provider-darwin-amd64-build
COPY --from=generate / /
WORKDIR /src/cmd/talemu-infra-provider
ARG GO_BUILDFLAGS
ARG GO_LDFLAGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg GOARCH=amd64 GOOS=darwin go build ${GO_BUILDFLAGS} -ldflags "${GO_LDFLAGS}" -o /talemu-infra-provider-darwin-amd64

# builds talemu-infra-provider-darwin-arm64
FROM base AS talemu-infra-provider-darwin-arm64-build
COPY --from=generate / /
WORKDIR /src/cmd/talemu-infra-provider
ARG GO_BUILDFLAGS
ARG GO_LDFLAGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg GOARCH=arm64 GOOS=darwin go build ${GO_BUILDFLAGS} -ldflags "${GO_LDFLAGS}" -o /talemu-infra-provider-darwin-arm64

# builds talemu-infra-provider-linux-amd64
FROM base AS talemu-infra-provider-linux-amd64-build
COPY --from=generate / /
WORKDIR /src/cmd/talemu-infra-provider
ARG GO_BUILDFLAGS
ARG GO_LDFLAGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg GOARCH=amd64 GOOS=linux go build ${GO_BUILDFLAGS} -ldflags "${GO_LDFLAGS}" -o /talemu-infra-provider-linux-amd64

# builds talemu-infra-provider-linux-arm64
FROM base AS talemu-infra-provider-linux-arm64-build
COPY --from=generate / /
WORKDIR /src/cmd/talemu-infra-provider
ARG GO_BUILDFLAGS
ARG GO_LDFLAGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg GOARCH=arm64 GOOS=linux go build ${GO_BUILDFLAGS} -ldflags "${GO_LDFLAGS}" -o /talemu-infra-provider-linux-arm64

# builds talemu-linux-amd64
FROM base AS talemu-linux-amd64-build
COPY --from=generate / /
WORKDIR /src/cmd/talemu
ARG GO_BUILDFLAGS
ARG GO_LDFLAGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg GOARCH=amd64 GOOS=linux go build ${GO_BUILDFLAGS} -ldflags "${GO_LDFLAGS}" -o /talemu-linux-amd64

# builds talemu-linux-arm64
FROM base AS talemu-linux-arm64-build
COPY --from=generate / /
WORKDIR /src/cmd/talemu
ARG GO_BUILDFLAGS
ARG GO_LDFLAGS
RUN --mount=type=cache,target=/root/.cache/go-build,id=talemu/root/.cache/go-build --mount=type=cache,target=/go/pkg,id=talemu/go/pkg GOARCH=arm64 GOOS=linux go build ${GO_BUILDFLAGS} -ldflags "${GO_LDFLAGS}" -o /talemu-linux-arm64

FROM scratch AS talemu-darwin-amd64
COPY --from=talemu-darwin-amd64-build /talemu-darwin-amd64 /talemu-darwin-amd64

FROM scratch AS talemu-darwin-arm64
COPY --from=talemu-darwin-arm64-build /talemu-darwin-arm64 /talemu-darwin-arm64

FROM scratch AS talemu-infra-provider-darwin-amd64
COPY --from=talemu-infra-provider-darwin-amd64-build /talemu-infra-provider-darwin-amd64 /talemu-infra-provider-darwin-amd64

FROM scratch AS talemu-infra-provider-darwin-arm64
COPY --from=talemu-infra-provider-darwin-arm64-build /talemu-infra-provider-darwin-arm64 /talemu-infra-provider-darwin-arm64

FROM scratch AS talemu-infra-provider-linux-amd64
COPY --from=talemu-infra-provider-linux-amd64-build /talemu-infra-provider-linux-amd64 /talemu-infra-provider-linux-amd64

FROM scratch AS talemu-infra-provider-linux-arm64
COPY --from=talemu-infra-provider-linux-arm64-build /talemu-infra-provider-linux-arm64 /talemu-infra-provider-linux-arm64

FROM scratch AS talemu-linux-amd64
COPY --from=talemu-linux-amd64-build /talemu-linux-amd64 /talemu-linux-amd64

FROM scratch AS talemu-linux-arm64
COPY --from=talemu-linux-arm64-build /talemu-linux-arm64 /talemu-linux-arm64

FROM talemu-infra-provider-linux-${TARGETARCH} AS talemu-infra-provider

FROM scratch AS talemu-infra-provider-all
COPY --from=talemu-infra-provider-darwin-amd64 / /
COPY --from=talemu-infra-provider-darwin-arm64 / /
COPY --from=talemu-infra-provider-linux-amd64 / /
COPY --from=talemu-infra-provider-linux-arm64 / /

FROM talemu-linux-${TARGETARCH} AS talemu

FROM scratch AS talemu-all
COPY --from=talemu-darwin-amd64 / /
COPY --from=talemu-darwin-arm64 / /
COPY --from=talemu-linux-amd64 / /
COPY --from=talemu-linux-arm64 / /

FROM scratch AS image-talemu-infra-provider
ARG TARGETARCH
COPY --from=talemu-infra-provider talemu-infra-provider-linux-${TARGETARCH} /talemu-infra-provider
COPY --from=image-fhs / /
COPY --from=image-ca-certificates / /
LABEL org.opencontainers.image.source=https://github.com/siderolabs/talemu
ENTRYPOINT ["/talemu-infra-provider"]

FROM scratch AS image-talemu
ARG TARGETARCH
COPY --from=talemu talemu-linux-${TARGETARCH} /talemu
COPY --from=image-fhs / /
COPY --from=image-ca-certificates / /
LABEL org.opencontainers.image.source=https://github.com/siderolabs/talemu
ENTRYPOINT ["/talemu"]

