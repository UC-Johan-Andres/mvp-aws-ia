FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod main.go ./
RUN go build -o launcher .

FROM alpine:3.19
COPY --from=builder /app/launcher /app/launcher
WORKDIR /app
EXPOSE 8090
CMD ["/app/launcher"]
