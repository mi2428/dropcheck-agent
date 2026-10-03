FROM golang:1.26.8-alpine3.23@sha256:a8fa79c5bd40d880b52bd3b6d7669ecdcfd00e85facdd427d279efb5ddd79cb1 AS build

ENV CGO_ENABLED=0 GOFLAGS=-p=1 GOMAXPROCS=2 GOTOOLCHAIN=local
WORKDIR /src

FROM build AS minio-build
# Official RELEASE.2025-10-15T17-29-55Z, including its security fix.
RUN wget -O source.tar.gz https://codeload.github.com/minio/minio/tar.gz/9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a \
    && echo "45521908307306e925c98d629e1c17d78c8b72b6ee242b1bfb1409f7d8ee5841  source.tar.gz" | sha256sum -c - \
    && tar -xzf source.tar.gz --strip-components=1 && rm source.tar.gz
RUN go build -trimpath -ldflags="-s -w -X github.com/minio/minio/cmd.Version=2025-10-15T17:29:55Z -X github.com/minio/minio/cmd.ReleaseTag=RELEASE.2025-10-15T17-29-55Z -X github.com/minio/minio/cmd.CommitID=9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a -X github.com/minio/minio/cmd.ShortCommitID=9e49d5e7a648 -X github.com/minio/minio/cmd.CopyrightYear=2025" -o /out/minio . \
    && mkdir /out/data

FROM build AS mc-build
# Official RELEASE.2025-08-13T08-35-41Z.
RUN wget -O source.tar.gz https://codeload.github.com/minio/mc/tar.gz/7394ce0dd2a80935aded936b09fa12cbb3cb8096 \
    && echo "95cd293c7119f16921a6dc515a1fb74a2227f19fd994b9c8b770a154e802ac44  source.tar.gz" | sha256sum -c - \
    && tar -xzf source.tar.gz --strip-components=1 && rm source.tar.gz
RUN go build -trimpath -ldflags="-s -w -X github.com/minio/mc/cmd.Version=2025-08-13T08:35:41Z -X github.com/minio/mc/cmd.ReleaseTag=RELEASE.2025-08-13T08-35-41Z -X github.com/minio/mc/cmd.CommitID=7394ce0dd2a80935aded936b09fa12cbb3cb8096 -X github.com/minio/mc/cmd.ShortCommitID=7394ce0dd2a8 -X github.com/minio/mc/cmd.CopyrightYear=2025" -o /out/mc .

FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3 AS minio
COPY --from=minio-build /out/minio /usr/local/bin/minio
COPY --from=minio-build /src/LICENSE /licenses/LICENSE
COPY --chown=65532:65532 --from=minio-build /out/data /data
EXPOSE 9000 9001
ENTRYPOINT ["/usr/local/bin/minio"]
CMD ["server", "/data"]

FROM alpine:3.23.6@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0 AS mc
COPY --from=mc-build /out/mc /usr/local/bin/mc
COPY --from=mc-build /src/LICENSE /src/CREDITS /licenses/
COPY --from=mc-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV HOME=/tmp
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/mc"]
