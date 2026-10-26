FROM golang:alpine AS builder

ENV GOTOOLCHAIN=auto

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /food_tracker ./cmd/food_tracker

FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata && \
    adduser -D -u 10001 appuser

WORKDIR /app
COPY --from=builder /food_tracker /app/food_tracker

USER appuser
ENTRYPOINT ["/app/food_tracker"]
