# Optional container. Standalone binary (./openbridge) remains the primary deployment.
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /openbridge ./cmd/openbridge

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /openbridge /openbridge
ENV OPENBRIDGE_HOST=0.0.0.0 OPENBRIDGE_PORT=8787 OPENBRIDGE_DATA_DIR=/data
VOLUME /data
EXPOSE 8787
ENTRYPOINT ["/openbridge"]
