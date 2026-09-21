# Multi-stage build: compile with the Go toolchain, run on a distroless-style
# minimal base. The final image contains the two static binaries and CA certs,
# nothing else — no shell, no package manager.
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/connector ./cmd/connector \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/providersim ./cmd/providersim

FROM scratch
# CA bundle comes from the golang:alpine build stage (it ships ca-certificates),
# so outbound TLS (provider APIs) verifies without a package install here.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/connector /connector
COPY --from=build /out/providersim /providersim
USER 65532:65532
EXPOSE 8080 9090
ENTRYPOINT ["/connector"]
