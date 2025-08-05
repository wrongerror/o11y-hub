# Observo Connector

一个通用的可观测性数据连接器，用于直接与各种可观测性数据源通信，包括Pixie Vizier、Beyla等。

## 功能特性

- 支持多种可观测性数据源
- 支持TLS和mTLS认证
- 执行查询并获取结果
- 健康检查功能
- 简单的命令行界面
- 可扩展的连接器架构

## 支持的数据源

- **Pixie Vizier**: eBPF可观测性平台
- **Beyla**: 应用性能监控
- 更多数据源支持开发中...

## 项目结构

```
observo-connector/
├── cmd/
│   ├── main.go              # 主程序入口
│   └── mock-server/
│       └── main.go          # Mock服务器（用于测试）
├── pkg/
│   ├── config/
│   │   └── tls.go          # TLS配置管理
│   └── vizier/
│       └── client.go       # Vizier客户端
├── proto/
│   ├── vizierapi.proto     # Vizier API定义
│   ├── vizierapi.pb.go     # 生成的protobuf代码
│   └── vizierapi_grpc.pb.go # 生成的gRPC代码
├── certs/                  # 证书文件目录
├── go.mod
├── Makefile
├── deploy.sh              # Kubernetes部署脚本
├── test.sh               # 测试脚本
└── README.md
```

## 快速开始

### 1. 安装依赖

确保你已经安装了：
- Go 1.23+
- protoc (Protocol Buffers编译器)
- protoc-gen-go 和 protoc-gen-go-grpc 插件

```bash
# 安装protoc (Ubuntu/Debian)
sudo apt install protobuf-compiler

# 安装Go插件
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 安装项目依赖
go mod tidy
```

### 2. 构建项目

```bash
# 使用Makefile构建
make build

# 或者手动构建
go build -o observo-connector ./cmd
```

### 3. 使用示例

#### 连接Pixie Vizier

```bash
# 健康检查
./observo-connector health \
    --cluster-id=your-cluster-id \
    --address=vizier.pixie.svc.cluster.local:51400 \
    --skip-verify=true

# 执行PxL查询
./observo-connector query "df.head(5)" \
    --cluster-id=your-cluster-id \
    --address=vizier.pixie.svc.cluster.local:51400 \
    --skip-verify=true
```

## TLS配置

### 开发环境（跳过验证）
```bash
./observo-connector query "df.head(5)" \
    --address=vizier.example.com:51400 \
    --skip-verify=true
```

### 生产环境（完整TLS验证）
```bash
./observo-connector query "df.head(5)" \
    --address=vizier.example.com:51400 \
    --ca-cert=certs/ca.crt \
    --client-cert=certs/client.crt \
    --client-key=certs/client.key \
    --server-name=vizier.example.com
```

## 测试

### 本地测试
```bash
# 启动mock服务器
./mock-server &

# 运行测试
make test

# 清理
pkill -f mock-server
```

### 容器测试
```bash
# 构建并测试
make test-mock
```

## Kubernetes部署

```bash
# 部署到Kubernetes
./deploy.sh

# 验证部署
kubectl get pods -n observo
```

## 扩展新的数据源

要添加新的数据源支持，你需要：

1. 在`pkg/`下创建新的客户端包
2. 实现相应的API接口
3. 添加protobuf定义（如果需要）
4. 更新命令行接口
5. 添加相应的测试

示例结构：
```
pkg/
├── beyla/
│   └── client.go
├── jaeger/
│   └── client.go
└── prometheus/
    └── client.go
```

## 命令行选项

```
通用选项:
      --address string       数据源地址
      --cluster-id string    集群ID
      --config string        配置文件

TLS选项:
      --ca-cert string       CA证书文件
      --client-cert string   客户端证书文件
      --client-key string    客户端私钥文件
      --server-name string   TLS服务器名称
      --disable-ssl          禁用SSL
      --skip-verify          跳过TLS证书验证

子命令:
  health                     健康检查
  query <query>             执行查询
  completion                自动补全脚本
```

## 常见用例

### 1. 数据导出
将可观测性数据导出到数据仓库或分析系统

### 2. 自定义监控
构建专门的监控仪表板和告警系统

### 3. 数据集成
与现有的监控栈进行集成

### 4. 开发调试
直接查询可观测性数据进行问题排查

## 贡献指南

1. Fork 项目
2. 创建特性分支 (`git checkout -b feature/new-datasource`)
3. 提交更改 (`git commit -am 'Add new datasource support'`)
4. 推送到分支 (`git push origin feature/new-datasource`)
5. 创建Pull Request

## 许可证

本项目采用 Apache 2.0 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 路线图

- [ ] 支持Beyla数据源
- [ ] 支持Jaeger追踪数据
- [ ] 支持Prometheus指标
- [ ] Web界面
- [ ] 数据流式处理
- [ ] 插件系统
- [ ] 配置文件支持

## 社区

- 提交Issue: [GitHub Issues](https://github.com/observo-io/observo-connector/issues)
- 讨论: [GitHub Discussions](https://github.com/observo-io/observo-connector/discussions)
