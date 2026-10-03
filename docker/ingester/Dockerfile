FROM golang:1.26.8-alpine3.23@sha256:a8fa79c5bd40d880b52bd3b6d7669ecdcfd00e85facdd427d279efb5ddd79cb1 AS build

ENV GOFLAGS=-p=1 GOMAXPROCS=2 GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dropcheck-ingester ./cmd/dropcheck-ingester

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

COPY --from=build /out/dropcheck-ingester /usr/local/bin/dropcheck-ingester
USER nonroot:nonroot
EXPOSE 8082
ENTRYPOINT ["/usr/local/bin/dropcheck-ingester"]
