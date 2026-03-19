FROM golang:1.26 AS builder

WORKDIR /src

COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/meshserver ./cmd/meshserver

FROM alpine:3

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/meshserver /app/meshserver
COPY migrations /app/migrations
COPY proto /app/proto
COPY scripts /app/scripts

RUN mkdir -p /app/data/blobs /app/data/logs /app/data/config

EXPOSE 4001/tcp 8080

CMD ["/app/meshserver"]
