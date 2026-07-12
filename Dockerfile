# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aud ./cmd/aud
RUN mkdir -p /out/data

FROM gcr.io/distroless/static:nonroot
USER nonroot:nonroot
ENV AUD_DB_PATH=/data/aud.db
COPY --from=build /out/aud /aud
COPY --from=build --chown=nonroot:nonroot /out/data /data
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/aud"]
