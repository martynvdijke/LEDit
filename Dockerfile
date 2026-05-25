FROM golang:1.24-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o ledit .

FROM alpine:latest
RUN apk add --no-cache sqlite-libs ca-certificates
WORKDIR /app
ENV DOCKER=true
COPY --from=builder /app/ledit .
COPY --from=builder /app/web ./web
COPY --from=builder /app/fonts ./fonts
RUN mkdir -p /db /app/data && chmod 777 /db /app/data
EXPOSE 8080
CMD ["./ledit"]
