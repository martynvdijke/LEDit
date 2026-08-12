FROM node:22-alpine AS assets
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY web ./web
COPY scripts ./scripts
COPY vite.config.ts tsconfig.frontend.json ./
RUN npm run build

FROM golang:1.26-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=assets /app/web/static/pwa ./web/static/pwa
COPY --from=assets /app/web/static/assets ./web/static/assets
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
