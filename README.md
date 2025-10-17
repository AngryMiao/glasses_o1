# AM glass后端服务

##  功能清单

* [x] 支持 websocket、Grpc 连接
* [x] 支持 PCM / Opus 格式语音对话
* [x] 支持大模型：ASR（豆包流式）、TTS（EdgeTTS/豆包）、LLM（OpenAI API、Ollama）
* [x] 支持语音控制调用摄像头识别图像
* [x] 支持 MCP 协议（客户端 / 本地 / 服务器），可接入高德地图、天气查询等
* [x] 支持语音控制切换角色声音
* [x] 支持语音控制切换预设角色
* [x] 支持单机部署服务
* [x] 支持本地数据库 sqlite、postgre


---

## 快速开始

###  配置 `.config.yaml`

* 文件在一级目录`config.yaml`
* 按需求配置模型、WebSocket、server 地址等字段

#### WebSocket 地址配置

```yaml
web:
  websocket: ws://your-server-ip:8000
```

#### 传输层配置

```yaml
transport:
  default: websocket
```
要使用Grpc传输层把默认选改为grpcgateway，并且进入`im-server`也启动im服务。方法：
``cd im-server``
``go run main.go``


#### 配置ASR，LLM，TTS

根据配置文件的格式，配置好相关模型服务，尽量不要增减字段

---

##  MCP 协议配置

参考：`src/core/mcp/README.md`

---

##  源码安装与运行

### 前置条件

* Go 1.24.2+
* Windows 用户需安装 CGO 和 Opus 库（见下文）

```bash
cd angrymiao-ai-server
cp config.yaml .config.yaml
```

---

### Windows 安装 Opus 编译环境

安装 [MSYS2](https://www.msys2.org/)，打开MYSY2 MINGW64控制台，然后输入以下命令：

```bash
pacman -Syu
pacman -S mingw-w64-x86_64-gcc mingw-w64-x86_64-go mingw-w64-x86_64-opus
pacman -S mingw-w64-x86_64-pkg-config
```

设置环境变量（用于 PowerShell 或系统变量）：

```bash
set PKG_CONFIG_PATH=C:\msys64\mingw64\lib\pkgconfig
set CGO_ENABLED=1
```

尽量在MINGW64环境下运行一次 “go run ./src/main.go” 命令，确保服务正常运行

GO mod如果更新较慢，可以考虑设置go代理，切换国内镜像源。

---

### 运行项目

```bash
go mod tidy
go run ./src/main.go
```


---

## 外部授权与设备绑定说明

简要说明：

1) 流程：外部授权签发 User JWT → 调用 POST /api/device/bind（Header: Authorization: Bearer <UserJWT>，Body: {"device_id":"..."}）→ 服务返回 {device_key, token} → 用 device token 连接 WebSocket/im-server。

2) 快速示例（curl）：

```bash
curl -X POST "http://your-server:8080/api/device/bind" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <UserJWT>" \
  -d '{"device_id":"device-001"}'
```

返回（成功）:

```json
{"success":true,"device_key":"<bind_key>","token":"<device_token>"}
```

3) WebSocket 连接：
- 推荐在握手时通过 Header 传 token：`Authorization: Bearer <device_token>` 和 `Device-Id: device-001`。
- 浏览器模式可用 query：`ws://your-server:8000/?device-id=device-001&token=<device_token>`（服务会把 token 转为 Authorization header）。

4) 参考实现文件：`src/device/server.go`、`src/device/types.go`、`src/core/transport/websocket/transport.go`（检查 token 有效期、绑定逻辑与验证点）。

注：请将外部授权系统的 JWT 公钥文件放在 `src/configs/casbin/jwt/public.pem`（当前未包含真实密钥，请替换为你的 public.pem）。


## Swagger 文档

* 打开浏览器访问：`http://localhost:8080/swagger/index.html`

### 更新 Swagger 文档

```bash
cd src
swag init -g main.go
```

---





