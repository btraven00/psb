FROM golang:1.25-alpine AS builder
RUN apk add --no-cache gcc musl-dev git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -ldflags "-X github.com/btraven00/psb/internal/handlers.commitHash=$(git rev-parse --short HEAD)" -o main ./cmd/server/main.go

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /root/
COPY --from=builder /app/main .
EXPOSE 8080
CMD ["./main"]
