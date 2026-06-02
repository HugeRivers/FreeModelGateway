FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/fmg ./cmd/fmg/

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
RUN adduser -D -u 1000 fmg
WORKDIR /app
COPY --from=builder /app/fmg /app/fmg
USER fmg
EXPOSE 10086
ENTRYPOINT ["/app/fmg"]
CMD ["-c", "/app/config.yaml"]
