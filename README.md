# Observo Connector

一个通用的可观测性数据连接器，用于直接与各种可观测性数据源通信，包括Pixie Vizier、Beyla等。支持数据导出为多种格式，包括Prometheus metrics、JSON、CSV等。

## 功能特性

- 支持多种可观测性数据源
- 支持TLS和mTLS认证，包括证书管理
- 执行查询并获取结构化数据
- 健康检查功能
- HTTP API服务器，支持RESTful接口
- 多种数据导出格式：Prometheus、JSON、CSV
- 实时数据转换和指标提取
- 支持Prometheus metrics格式输出
- 可扩展的连接器架构

## 支持的数据源

- **Pixie Vizier**: eBPF可观测性平台
- **Beyla**: 应用性能监控
- 更多数据源支持开发中...

## 支持的导出格式

- **Prometheus**: 兼容Prometheus的metrics格式
- **JSON**: 结构化JSON数据
- **CSV**: 表格数据格式
- **OpenTelemetry**: OTLP格式 (计划中)

## 项目结构

```
observo-connector/
├── cmd/
│   └── main.go              # 主程序入口，支持多种命令
├── pkg/
│   ├── config/
│   │   ├── tls.go          # TLS配置管理
│   │   └── certificates.go # 证书管理
│   ├── common/
│   │   └── types.go        # 公共类型定义
│   ├── export/
│   │   └── exporters.go    # 数据导出器
│   ├── server/
│   │   └── http.go         # HTTP API服务器
│   └── vizier/
│       └── client.go       # Vizier客户端，支持数据提取
├── proto/
│   ├── vizierapi_simple.proto     # Vizier API定义
│   ├── vizierapi_simple.pb.go     # 生成的protobuf代码
│   └── vizierapi_simple_grpc.pb.go # 生成的gRPC代码
├── certs/                  # 证书文件目录
├── config.example.yaml     # 配置文件示例
├── go.mod
├── Makefile
├── test.sh               # 测试脚本
└── README.md
```

## 快速开始

### 1. 安装依赖

确保你已经安装了：
- Go 1.23+
- protoc (Protocol Buffers compiler)
- make

### 2. 构建项目

```bash
# 克隆项目
git clone https://github.com/wrongerror/observo-connector.git
cd observo-connector

# 安装依赖并构建
make build
```

### 3. 配置

复制示例配置文件并修改：

```bash
cp config.example.yaml config.yaml
# 编辑config.yaml以匹配你的环境
```

### 4. 运行

#### 命令行模式

```bash
# 健康检查
./observo-connector health --cluster-id=my-cluster --address=localhost:50051

# 执行查询
./observo-connector query "dx.display_name" --cluster-id=my-cluster

# 使用配置文件
./observo-connector query "dx.display_name" --config=config.yaml
```

#### HTTP服务器模式

```bash
# 启动HTTP API服务器
./observo-connector server --port=8080 --cluster-id=my-cluster

# 使用配置文件启动
./observo-connector server --config=config.yaml
```

## HTTP API接口

启动服务器后，可以使用以下API接口：

### 内置脚本系统

Observo Connector 包含多个预定义的监控脚本，可以自动从Pixie收集常见的可观测性指标：

#### 查看可用脚本

```bash
# 列出所有内置脚本
./observo-connector --list-scripts

# 查看特定脚本帮助
./observo-connector --script-help resource_usage
```

#### 执行内置脚本

```bash
# 执行资源使用情况脚本
./observo-connector --builtin-script resource_usage \
  --script-params "start_time=-5m" \
  --address vizier-query-broker-svc.pl.svc.cluster.local:50300 \
  --cluster-id demo-cluster \
  --jwt-service demo-service \
  --jwt-key <your-jwt-key> \
  --tls=true \
  --skip-verify=true

# 执行HTTP概览脚本
./observo-connector --builtin-script http_overview \
  --script-params "start_time=-10m,namespace=default" \
  --address vizier-query-broker-svc.pl.svc.cluster.local:50300 \
  --cluster-id demo-cluster \
  --tls=true \
  --skip-verify=true
```

### Prometheus 指标端点

#### 启动HTTP服务器

```bash
# 启动服务器以提供Prometheus指标
./observo-connector --server \
  --port 8080 \
  --address vizier-query-broker-svc.pl.svc.cluster.local:50300 \
  --cluster-id demo-cluster \
  --tls=true \
  --skip-verify=true \
  --jwt-service demo-service \
  --jwt-key <your-jwt-key>
```

#### 获取指标

```bash
# 获取所有内置脚本的指标（自动执行resource_usage, http_overview, network_stats等）
curl http://localhost:8080/api/v1/metrics

# 获取特定脚本的指标
curl "http://localhost:8080/api/v1/metrics?script=resource_usage"
curl "http://localhost:8080/api/v1/metrics?script=http_overview"
curl "http://localhost:8080/api/v1/metrics?script=network_stats"

# 带参数获取指标
curl "http://localhost:8080/api/v1/metrics?script=resource_usage&start_time=-10m&namespace=default"
```

#### 支持的指标类型

**基础设施指标:**
- `pixie_pod_cpu_usage_ratio` - Pod CPU使用率
- `pixie_pod_memory_usage_ratio` - Pod内存使用率

**HTTP指标:**
- `pixie_http_requests_total` - HTTP请求总数
- `pixie_http_errors_total` - HTTP错误总数
- `pixie_http_request_duration_milliseconds` - 平均请求延迟
- `pixie_http_request_duration_milliseconds_quantile` - 请求延迟分位数

**网络指标:**
- `pixie_network_bytes_sent_total` - 发送字节总数
- `pixie_network_bytes_received_total` - 接收字节总数
- `pixie_network_packets_sent_total` - 发送数据包总数
- `pixie_network_packets_received_total` - 接收数据包总数

#### 在Prometheus中配置

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'observo-connector'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: '/api/v1/metrics'
    scrape_interval: 30s
```

### 查询接口

#### POST /api/v1/query
执行查询并返回结构化数据

```bash
curl -X POST http://localhost:8080/api/v1/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "dx.display_name",
    "cluster_id": "my-cluster",
    "format": "json"
  }'
```

#### GET /api/v1/query
通过URL参数执行查询

```bash
curl "http://localhost:8080/api/v1/query?query=dx.display_name&cluster_id=my-cluster&format=json"
```

### 健康检查

#### GET /api/v1/health
检查Vizier集群健康状态

```bash
curl "http://localhost:8080/api/v1/health?cluster_id=my-cluster"
```

### Prometheus Metrics

#### GET /api/v1/metrics
获取Prometheus格式的指标

```bash
# 获取内部指标
curl http://localhost:8080/api/v1/metrics

# 从查询结果获取指标
curl "http://localhost:8080/api/v1/metrics?query=http_events&cluster_id=my-cluster"
```

### 数据导出

#### POST /api/v1/export
导出查询结果为指定格式

```bash
curl -X POST http://localhost:8080/api/v1/export \
  -H "Content-Type: application/json" \
  -d '{
    "query": "http_events",
    "cluster_id": "my-cluster",
    "format": "prometheus",
    "options": {
      "compression": false
    }
  }'
```

## TLS/mTLS 配置

### 服务器端TLS

```yaml
disable_ssl: false
ca_cert: "certs/ca.crt"
server_name: "vizier.example.com"
skip_verify: false
```

### 双向TLS (mTLS)

```yaml
disable_ssl: false
ca_cert: "certs/ca.crt"
client_cert: "certs/client.crt"
client_key: "certs/client.key"
server_name: "vizier.example.com"
```

### 证书管理

connector支持自动证书管理和验证：

- 证书有效性检查
- 证书路径解析（支持相对和绝对路径）
- 证书目录管理

## 数据导出格式

### Prometheus格式

```
# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="GET",status="200"} 1234 1691234567000
```

### JSON格式

```json
{
  "query": "http_events",
  "executed_at": "2024-08-05T10:30:00Z",
  "duration": "1.2s",
  "row_count": 100,
  "columns": ["time", "method", "url", "status"],
  "data": [
    {
      "time": "2024-08-05T10:29:00Z",
      "method": "GET",
      "url": "/api/users",
      "status": 200
    }
  ]
}
```

### CSV格式

```csv
time,method,url,status
2024-08-05T10:29:00Z,GET,/api/users,200
2024-08-05T10:29:01Z,POST,/api/users,201
```

## 高级功能

### 数据转换

connector自动将Pixie查询结果转换为标准化格式：

- 时间戳处理（纳秒精度转换）
- 数据类型映射
- 标签提取和规范化
- 指标类型推断

### 批处理和流式处理

- 支持大结果集的批处理
- 流式数据处理，降低内存使用
- 可配置的批次大小

## 部署

### Kubernetes部署

```bash
# 手动部署（需要自己准备k8s manifests）
kubectl apply -f k8s/
```

### Docker部署

```bash
# 构建镜像
docker build -t observo-connector .

# 运行容器
docker run -p 8080:8080 \
  -v $(pwd)/certs:/app/certs \
  -v $(pwd)/config.yaml:/app/config.yaml \
  observo-connector server --config=/app/config.yaml
```

## 开发

### 添加新的数据源

1. 在 `pkg/` 下创建新的包
2. 实现 `common.DataSource` 接口
3. 在主程序中注册新的数据源

### 添加新的导出格式

1. 在 `pkg/export/exporters.go` 中实现 `DataExporter` 接口
2. 在 `ExporterFactory` 中注册新格式
3. 更新 `common.ExportFormat` 常量

### 运行测试

```bash
make test
```

### 生成protobuf代码

```bash
make proto
```

## 监控和日志

### 内置指标

connector提供以下内置指标：

- `observo_connector_queries_total`: 总查询数
- `observo_connector_up`: 服务状态
- `observo_connector_build_info`: 构建信息

### 日志配置

支持结构化日志，包含以下信息：

- 查询执行时间
- 数据处理统计
- 错误和警告信息
- 性能指标

## 故障排除

### 常见问题

1. **连接失败**
   - 检查Vizier地址和端口
   - 验证TLS证书配置
   - 确认网络连通性

2. **证书错误**
   - 验证证书文件路径
   - 检查证书有效期
   - 确认CA证书配置

3. **查询超时**
   - 增加查询超时时间
   - 检查查询复杂度
   - 验证集群资源状况

### 调试模式

```bash
# 启用详细日志
./observo-connector server --log-level=debug
```

## 贡献

欢迎提交Pull Request和Issue。请确保：

1. 代码通过所有测试
2. 添加适当的文档
3. 遵循Go代码规范

## 许可证

本项目采用MIT许可证 - 详见 [LICENSE](LICENSE) 文件。
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
    --address=vizier.pixie.svc.cluster.local:50300 \
    --skip-verify=true

# 执行PxL查询
./observo-connector query "df.head(5)" \
    --cluster-id=your-cluster-id \
    --address=vizier.pixie.svc.cluster.local:50300 \
    --skip-verify=true
```

## TLS配置

### 开发环境（跳过验证）
```bash
./observo-connector query "df.head(5)" \
    --address=vizier.example.com:50300 \
    --skip-verify=true
```

### 生产环境（完整TLS验证）
```bash
./observo-connector query "df.head(5)" \
    --address=vizier.example.com:50300 \
    --ca-cert=certs/ca.crt \
    --client-cert=certs/client.crt \
    --client-key=certs/client.key \
    --server-name=vizier.example.com
```

## 测试

### 本地测试
```bash
# 运行基础功能测试
make test

# 运行测试脚本
./test.sh
```

## Kubernetes部署

```bash
# 使用kubectl直接部署（需要准备manifests）
kubectl apply -f k8s/

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
