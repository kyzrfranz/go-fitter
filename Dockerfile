FROM golang:1.26 AS builder
WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o sync ./cmd/sync.go
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./main.go

FROM scratch AS syncer
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app/sync /sync
ENTRYPOINT ["/sync"]

FROM scratch AS server
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /app/server /server

EXPOSE 8080
ENTRYPOINT ["/server"]
