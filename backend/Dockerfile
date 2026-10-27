# EvolverMon API（多阶段构建，单二进制 ~20MB）
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /evolvermon ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 em
USER em
COPY --from=build /evolvermon /usr/local/bin/evolvermon
EXPOSE 8080
ENTRYPOINT ["evolvermon"]
