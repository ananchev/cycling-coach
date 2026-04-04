FROM golang:1.24-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o cycling-coach ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache sqlite-libs ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/cycling-coach .
COPY --from=builder /app/config ./config
EXPOSE 8080
CMD ["./cycling-coach"]
