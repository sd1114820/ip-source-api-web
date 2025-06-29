# 🌍 IP地理位置查询API

这是一个基于 Go 语言实现的高性能、高可用性 IP 地理位置查询 API 服务，支持全球 IP 地址查询，中国大陆地区精确到区县级别。

## ✨ 核心功能

### 🎯 地理位置查询
- **全球覆盖**: 支持全球 IPv4/IPv6 地址查询
- **高精度定位**: 中国大陆地区精确到区县级别
- **多数据源**: 集成 MaxMind GeoLite2 和 GeoCN 数据库
- **丰富字段**: 提供30+地理位置、ISP、ASN等信息字段

### ⚡ 性能优化
- **内存缓存**: 智能缓存机制，自动过期管理
- **并发安全**: 读写锁保护，支持高并发访问
- **快速响应**: 毫秒级查询响应时间
- **资源优化**: 合理的内存和CPU使用

### 🔒 安全防护
- **速率限制**: 智能速率控制保护
- **输入验证**: 严格的IP地址格式验证
- **错误处理**: 完善的错误分类和响应
- **超时控制**: 防止慢速攻击和资源耗尽

### 🔄 自动化运维
- **自动更新**: 每24小时自动检查并更新数据库
- **原子更新**: 安全的数据库热更新机制
- **优雅关闭**: 支持信号处理和优雅停机
- **健康监控**: 完整的日志记录和错误追踪

## 🛡️ 防护机制

### 速率限制 (Rate Limiting)
- **算法**: 基于令牌桶算法的速率控制
- **限制规则**: 基于令牌桶算法的智能限流
- **存储方式**: 内存中维护客户端限制器映射
- **超限处理**: 返回 `429 Too Many Requests` 状态码

### 输入安全验证
- **IP格式验证**: 使用 `net.ParseIP()` 严格验证
- **特殊IP处理**: 自动识别私有IP、环回地址、链路本地地址
- **错误分类**: 区分客户端错误和服务器内部错误
- **友好响应**: 针对不同错误类型返回相应消息

### HTTP安全配置
- **超时设置**: 读取5秒、写入10秒、空闲120秒
- **CORS控制**: 限制请求方法为GET和OPTIONS
- **路由保护**: 只暴露必要的API端点
- **协议安全**: 支持HTTPS部署

### 资源保护
- **内存管理**: 定期清理过期缓存项
- **连接管理**: 合理的数据库连接池
- **并发控制**: 防止资源竞争和死锁
- **错误恢复**: 自动重试和故障转移机制

## API 使用说明

### 端点

- `GET /json/{ip}`: 查询指定 IP 地址的地理位置信息。
- `GET /json`: 查询客户端自身 IP 地址的地理位置信息。

### 请求示例

查询特定 IP (例如 `8.8.8.8`):

```bash
curl http://localhost:8180/json/8.8.8.8
```

查询自己的 IP:

```bash
curl http://localhost:8180/json
```

### HTTP状态码

| 状态码 | 说明 | 响应示例 |
|--------|------|----------|
| 200 OK | 查询成功 | 正常的JSON数据 |
| 200 OK | 私有IP地址 | `{"ip": "192.168.1.1", "message": "private range"}` |
| 200 OK | 保留IP地址 | `{"ip": "127.0.0.1", "message": "reserved range"}` |
| 200 OK | IP不在数据库中 | `{"ip": "1.2.3.4", "message": "not in database"}` |
| 400 Bad Request | IP格式无效 | `{"ip": "invalid.ip", "message": "invalid query"}` |
| 429 Too Many Requests | 请求频率超限 | `Too Many Requests` |
| 500 Internal Server Error | 服务器内部错误 | `{"ip": "8.8.8.8", "message": "internal error"}` |

### 响应格式

成功的响应将返回一个包含地理位置信息的 JSON 对象。

```json
{
  "ip": "8.8.8.8",
  "network": "8.8.8.0/24",
  "version": "IPv4",
  "city": "山景城",
  "city_code": 0,
  "region": "加利福尼亚州",
  "region_code": "CA",
  "province_code": 0,
  "districts": "",
  "districts_code": 0,
  "country": "US",
  "country_name": "美国",
  "country_code": "US",
  "country_code_iso3": "USA",
  "country_capital": "华盛顿",
  "country_tld": ".us",
  "continent_code": "NA",
  "in_eu": false,
  "postal": "94043",
  "latitude": 37.4056,
  "longitude": -122.0775,
  "timezone": "America/Los_Angeles",
  "utc_offset": "-08:00",
  "country_calling_code": "+1",
  "currency": "USD",
  "currency_name": "美元",
  "languages": "en-US,es-US,haw,fr",
  "country_area": 9629091.0,
  "country_population": 310232863,
  "asn": "AS15169",
  "org": "Google LLC",
  "isp": "Google"
}
```

### 字段过滤

您可以通过 `fields` 查询参数来指定返回的字段，多个字段用逗号分隔。

```bash
curl "http://localhost:8180/json/8.8.8.8?fields=ip,country_name,city_name,asn"
```

响应:

```json
{
  "ip": "8.8.8.8",
  "country_name": "United States",
  "city_name": "Mountain View",
  "asn": 15169
}
```

## 🏗️ 技术架构

### 核心组件

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   HTTP Server   │    │   Middleware    │    │   API Handler   │
│                 │────│                 │────│                 │
│ • 路由管理      │    │ • 速率限制      │    │ • IP查询处理    │
│ • 静态文件      │    │ • CORS支持      │    │ • 缓存管理      │
│ • 优雅关闭      │    │ • 错误处理      │    │ • 响应格式化    │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                ↓
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   GeoIP Module  │    │   Cache System  │    │   Auto Updater  │
│                 │    │                 │    │                 │
│ • 数据库管理    │    │ • 内存缓存      │    │ • 定时更新      │
│ • 并发安全      │    │ • 过期清理      │    │ • 原子替换      │
│ • 多数据源      │    │ • 命中统计      │    │ • 错误重试      │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

### 数据流程

1. **请求接收**: HTTP服务器接收客户端请求
2. **中间件处理**: 速率限制 → CORS检查 → 请求验证
3. **缓存查询**: 检查内存缓存是否存在结果
4. **数据库查询**: 缓存未命中时查询GeoIP数据库
5. **结果处理**: 格式化响应数据并更新缓存
6. **响应返回**: 返回JSON格式的查询结果

### 技术栈

- **语言**: Go 1.18+
- **HTTP框架**: 标准库 `net/http`
- **GeoIP库**: `github.com/oschwald/geoip2-golang`
- **缓存**: `github.com/patrickmn/go-cache`
- **限流**: `golang.org/x/time/rate`
- **数据库**: MaxMind GeoLite2 + GeoCN MMDB

## 🚀 快速开始

### 前提条件

- [Go](https://golang.org/dl/) (版本 1.18 或更高)
- 网络连接（用于下载数据库文件）

### 安装与运行

1. **克隆项目**
   ```bash
   git clone https://github.com/sd1114820/ip-source-api-web.git
   cd ip-source-api-web
   ```

2. **下载依赖**
   ```bash
   go mod tidy
   ```

3. **编译项目**
   ```bash
   go build -o ip-source-api-web
   ```

4. **运行服务**
   ```bash
   ./ip-source-api-web
   ```

5. **验证服务**
   ```bash
   curl http://localhost:8180/json/8.8.8.8
   ```

### 开发模式

```bash
# 直接运行（开发模式）
go run main.go

# 热重载（需要安装air）
go install github.com/cosmtrek/air@latest
air
```

## ⚙️ 配置说明

本项目的所有配置项均已在代码中硬编码，如需修改请编辑 `config/config.go` 文件并重新编译。

### 主要配置项

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| ListenAddr | `0.0.0.0:8180` | 服务监听地址和端口 |
| DataDir | `data` | 数据库文件存储目录 |
| UpdateInterval | `24` | 数据库更新间隔（小时） |
| ReadTimeout | `5s` | HTTP读取超时时间 |
| WriteTimeout | `10s` | HTTP写入超时时间 |
| IdleTimeout | `120s` | HTTP空闲超时时间 |
| MaxMindLicenseKey | `已内置` | MaxMind API密钥 |

### 速率限制配置

- **请求频率**: X次/分钟
- **突发允许**: X次
- **算法**: 令牌桶
- **存储**: 内存映射

### 缓存配置

- **过期时间**: X分钟
- **清理间隔**: X分钟
- **存储方式**: 内存
- **键格式**: `{ip}?fields={fields}`

## 📊 性能指标

### 基准测试

在标准硬件配置下的性能表现：

| 指标 | 数值 | 说明 |
|------|------|------|
| 响应时间 | < 5ms | 缓存命中时 |
| 响应时间 | < 50ms | 数据库查询时 |
| 并发处理 | 1000+ QPS | 单核心处理能力 |
| 内存使用 | < 100MB | 包含数据库和缓存 |
| CPU使用 | < 5% | 正常负载下 |

### 容量规划

- **数据库大小**: ~200MB（所有MMDB文件）
- **内存缓存**: 根据访问模式动态调整
- **磁盘空间**: 建议预留1GB用于日志和临时文件
- **网络带宽**: 每个请求约1-2KB响应数据

## 🔍 监控与日志

### 日志级别

- **INFO**: 正常操作日志
- **WARN**: 警告信息（如IP不在数据库中）
- **ERROR**: 错误信息（如数据库访问失败）

### 监控指标

建议监控以下关键指标：

1. **请求指标**
   - QPS（每秒请求数）
   - 响应时间分布
   - 错误率统计

2. **缓存指标**
   - 缓存命中率
   - 缓存大小
   - 过期清理频率

3. **系统指标**
   - CPU使用率
   - 内存使用量
   - 磁盘I/O

### 健康检查

```bash
# 基本健康检查
curl -f http://localhost:8180/json/8.8.8.8 || exit 1

# 详细状态检查
curl -s http://localhost:8180/json/8.8.8.8 | jq '.ip' || exit 1
```

## 部署

### 直接部署

将编译好的二进制文件 `ip-source-api-web` 和 `data` 目录（如果需要保留现有数据库）上传到您的服务器。设置好环境变量后，直接运行即可。建议使用 `systemd` 或 `supervisor` 等工具来管理进程，以确保服务在后台持续运行并能自动重启。

### Docker 部署 (推荐)

您也可以为该应用创建一个 `Dockerfile` 来进行容器化部署，这样可以简化环境依赖和部署流程。

### Docker部署 (推荐)

**示例 `Dockerfile`:**

```dockerfile
# 使用官方 Go 镜像作为构建环境
FROM golang:1.23-alpine AS builder

WORKDIR /app

# 复制 go.mod 和 go.sum 并下载依赖
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 编译应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o ip-source-api-web .

# 使用一个轻量的基础镜像来运行应用
FROM alpine:latest

# 安装必要的工具
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# 从构建阶段复制编译好的二进制文件
COPY --from=builder /app/ip-source-api-web .
COPY --from=builder /app/index.html .

# 创建数据目录
RUN mkdir ./data

# 暴露端口
EXPOSE 8180

# 运行应用
CMD ["./ip-source-api-web"]
```

**构建和运行:**

```bash
# 构建镜像
docker build -t ip-source-api-web .

# 运行容器
docker run -d -p 8180:8180 --name ip-source-api-web ip-source-api-web

# 使用docker-compose
cat > docker-compose.yml << EOF
version: '3.8'
services:
  ip-source-api-web:
    build: .
    ports:
      - "8180:8180"
    volumes:
      - ./data:/root/data
    restart: unless-stopped
EOF

docker-compose up -d
```

## ❓ 常见问题

### Q: 为什么查询某些IP返回"not in database"？
A: 这是正常现象，可能的原因：
- IP地址是新分配的，尚未被数据库收录
- IP地址属于特殊用途（如测试、文档示例等）
- 数据库更新存在延迟

### Q: 如何提高查询性能？
A: 可以通过以下方式优化：
- 增加服务器内存以扩大缓存容量
- 使用SSD存储以提高数据库读取速度
- 部署多个实例并使用负载均衡
- 调整缓存过期时间（在准确性和性能间平衡）

### Q: 数据库多久更新一次？
A: 系统每24小时自动检查并下载最新的数据库文件。MaxMind通常每周二更新GeoLite2数据库。

### Q: 支持IPv6吗？
A: 是的，系统完全支持IPv6地址查询，使用相同的API接口。

### Q: 如何自定义返回字段？
A: 使用`fields`参数指定需要的字段，例如：
```bash
curl "http://localhost:8180/json/8.8.8.8?fields=ip,country_name,city"
```

## 🛠️ 故障排除

### 服务无法启动
1. 检查端口是否被占用：`netstat -tlnp | grep 8180`
2. 检查数据目录权限：`ls -la data/`
3. 查看启动日志中的错误信息

### 查询返回错误
1. 验证IP地址格式是否正确
2. 检查数据库文件是否完整：`ls -la data/*.mmdb`
3. 查看服务日志中的详细错误信息

### 性能问题
1. 监控缓存命中率
2. 检查系统资源使用情况
3. 分析访问模式和热点IP

## 🤝 贡献指南

欢迎提交Issue和Pull Request！

### 开发环境设置
```bash
# 克隆项目
git clone https://github.com/sd1114820/ip-source-api-web.git
cd ip-source-api-web

# 安装依赖
go mod tidy

# 运行测试
go test ./...

# 代码格式化
go fmt ./...

# 静态检查
go vet ./...
```

### 提交规范
- 使用清晰的提交信息
- 确保代码通过所有测试
- 遵循Go代码规范
- 更新相关文档

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 🙏 致谢

- [MaxMind](https://www.maxmind.com/) - 提供GeoLite2数据库
- [GeoCN](https://github.com/ljxi/GeoCN) - 提供中国地区精确数据
- [geoip2-golang](https://github.com/oschwald/geoip2-golang) - Go语言GeoIP库

---

**⭐ 如果这个项目对你有帮助，请给个Star支持一下！**