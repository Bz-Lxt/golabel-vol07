# 官方 Go 镜像，自带完整工具链（评测时模型需在容器内改代码、编译、跑测试）
FROM golang:1.22
ENV GOTOOLCHAIN=local
ENV GOPROXY=https://goproxy.cn,direct
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY go.mod ./
COPY . .
RUN go build ./...
CMD ["bash"]
