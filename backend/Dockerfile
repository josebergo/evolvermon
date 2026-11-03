# EvolverMon API（多阶段构建；单镜像含全部 10 个部署单元，Helm chart 按 ./<cmd> 选择进程）
# Lite=server；Micro=identity/core-erp/core-mes/core-wms/master-data/integration/scheduler/ops-admin/gateway
FROM golang:1.27-alpine AS build
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN set -e; for c in server gateway identity core-erp core-mes core-wms master-data integration scheduler ops-admin; do \
      CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /out/$c ./cmd/$c; \
    done

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 em
USER em
WORKDIR /usr/local/bin
COPY --from=build /out/ /usr/local/bin/
EXPOSE 8080 8081 8083 8084 8085 8086 8087 8088 8180
ENTRYPOINT ["./server"]
