# 构建阶段：国内 Go 代理，避免 go mod download 超时
FROM docker.m.daocloud.io/library/golang:1.27-alpine AS builder
WORKDIR /app
ENV GOPROXY=https://goproxy.cn,direct \
    GOSUMDB=off \
    CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -o /server ./cmd/server

# 运行阶段（apk 换阿里云源：alpine 官方源国内被墙；tzdata 提供 Asia/Shanghai 时区）
FROM docker.m.daocloud.io/library/alpine:3.20
RUN sed -i 's|https://dl-cdn.alpinelinux.org/alpine|https://mirrors.aliyun.com/alpine|g' /etc/apk/repositories \
    && apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /server /app/server
EXPOSE 8080
ENTRYPOINT ["/app/server"]
