# syntax=docker/dockerfile:1

FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/aud ./cmd/aud

FROM gcr.io/distroless/static:nonroot
USER nonroot:nonroot
COPY --from=build /out/aud /aud
EXPOSE 8080
ENTRYPOINT ["/aud"]
