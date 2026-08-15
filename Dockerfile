FROM golang:1.25

ENV TZ=Asia/Shanghai
ENV GOTOOLCHAIN=local

WORKDIR /app
COPY . .
RUN go build ./... && go vet ./... && go test ./...

CMD ["bash"]
