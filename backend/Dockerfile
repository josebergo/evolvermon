# EvolverMon API（多阶段构建；Lite=evolvermon 单二进制，Micro=identity/server/integration/scheduler/ops-admin/gateway）
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /evolvermon ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /identity ./cmd/identity \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /server ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /integration ./cmd/integration \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /scheduler ./cmd/scheduler \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /ops-admin ./cmd/ops-admin \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gateway ./cmd/gateway

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 em
USER em
COPY --from=build /evolvermon /usr/local/bin/evolvermon
COPY --from=build /identity /usr/local/bin/identity
COPY --from=build /server /usr/local/bin/server
COPY --from=build /integration /usr/local/bin/integration
COPY --from=build /scheduler /usr/local/bin/scheduler
COPY --from=build /ops-admin /usr/local/bin/ops-admin
COPY --from=build /gateway /usr/local/bin/gateway
EXPOSE 8080
ENTRYPOINT ["evolvermon"]
