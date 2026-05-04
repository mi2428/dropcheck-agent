FROM golang:1.26-alpine3.23 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dropcheck-ingester ./cmd/dropcheck-ingester

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /out/dropcheck-ingester /usr/local/bin/dropcheck-ingester
USER nonroot:nonroot
EXPOSE 8082
ENTRYPOINT ["/usr/local/bin/dropcheck-ingester"]
