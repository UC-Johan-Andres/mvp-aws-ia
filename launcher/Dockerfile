FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o launcher .

FROM alpine:3.19
COPY --from=builder /app/launcher /app/launcher
WORKDIR /app
RUN mkdir -p /app/data
EXPOSE 8090
CMD ["/app/launcher"]
