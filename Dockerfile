FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY pkg ./pkg
COPY cmd ./cmd
RUN go build -o /out/graceful-app ./cmd/demo-app

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /out/graceful-app /app/graceful-app
EXPOSE 8080
ENTRYPOINT ["/app/graceful-app"]
