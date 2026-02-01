# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder

WORKDIR /app/web/admin

# Copy package files first for better caching
COPY web/admin/package*.json ./
RUN npm ci

# Copy frontend source and build
COPY web/admin/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN GOTOOLCHAIN=auto go mod download

# Copy source code
COPY . .

# Copy frontend build output from previous stage
COPY --from=frontend-builder /app/web/admin/dist ./web/admin/dist

# Build the application binaries with build time injection
ARG BUILD_TIME
RUN GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags "-X github.com/m0rjc/OsmDeviceAdapter/internal/admin.buildTime=${BUILD_TIME:-unknown}" \
    -o /app/bin/server ./cmd/server
RUN GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/bin/cleanup ./cmd/cleanup

# Stage 3: Runtime
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binaries from builder
COPY --from=builder /app/bin/server .
COPY --from=builder /app/bin/cleanup .

# Expose port
EXPOSE 8080

# Run the application
CMD ["./server"]
