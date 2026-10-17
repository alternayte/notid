FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/notid ./cmd/notid \
 && mkdir -p /out/data/media /out/data/certs

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/notid /usr/local/bin/notid
COPY --from=build --chown=nonroot:nonroot /out/data /data
VOLUME /data
EXPOSE 80 443
ENTRYPOINT ["/usr/local/bin/notid"]
CMD ["all"]
