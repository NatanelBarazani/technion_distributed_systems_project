FROM golang:1.23 AS builder

WORKDIR /app

# Disable CGO so that the binary is statically linked.
ENV CGO_ENABLED=0

# Copy go.mod and go.sum first and download dependencies.
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire repository (build context should be the repository root)
COPY . .

# Build the server binary using the entry point in cmd/server/main.go and name it "s"
RUN go build -ldflags="-s -w" -o s ./cmd/server/main.go

FROM alpine:latest
# Alpine uses musl, but our binary is statically linked so it should run without issues.

# Copy the built binary from the builder stage into /usr/local/bin/s
COPY --from=builder /app/s /usr/local/bin/s

# Ensure the binary has execute permissions.
RUN chmod +x /usr/local/bin/s

# Expose the necessary ports.
EXPOSE 8081
EXPOSE 50051

# Set the entrypoint to our binary.
ENTRYPOINT ["/usr/local/bin/s"]
