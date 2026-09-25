# Encoding

统一的编解码抽象层，为 `go-wind` 框架的所有传输层提供可拔插的序列化能力。

通过 `encoding.Codec` 接口定义统一的 Marshal/Unmarshal 契约，支持 13 种编解码格式，各传输层（HTTP / gRPC / TCP / WebSocket / SSE 等）通过 `WithCodec(name)` 切换编解码器，无需修改业务代码。

## 核心设计

- **接口驱动**：所有编解码器实现 `Codec` 接口（`Marshal` / `Unmarshal` / `Name`）
- **自动注册**：每个编解码器通过 `init()` 自注册到全局注册表
- **按需加载**：通过 `_ import` 引入需要的格式，未引入的格式不占用二进制体积
- **大小写无关**：编解码器名称大小写无关，`"json"` / `"JSON"` / `"Json"` 均可

## Codec 接口

```go
type Codec interface {
    Marshal(v any) ([]byte, error)
    Unmarshal(data []byte, v any) error
    Name() string
}
```

## 安装

基础包（定义 `Codec` 接口和注册表）：

```bash
go get github.com/tx7do/go-wind-plugins/encoding
```

各编解码格式按需引入：

```bash
go get github.com/tx7do/go-wind-plugins/encoding/json
go get github.com/tx7do/go-wind-plugins/encoding/proto
# ... 其他格式同理
```

## 快速开始

### 注册与使用

```go
import (
    "github.com/tx7do/go-wind-plugins/encoding"
    _ "github.com/tx7do/go-wind-plugins/encoding/json"  // side-effect: 注册 JSON 编解码器
)

func main() {
    codec := encoding.GetCodec("json")

    // 编码
    data, err := codec.Marshal(map[string]string{"hello": "world"})

    // 解码
    var result map[string]string
    err = codec.Unmarshal(data, &result)
}
```

### 在传输层中使用

```go
// TCP 服务器使用 JSON 编解码
srv := tcp.NewServer(
    tcp.WithAddress(":9000"),
    tcp.WithCodec("json"),
)

// HTTP 服务器使用 JSON 编解码
srv := http.NewServer(
    http.WithCodec("json"),
)

// SSE 服务器使用 JSON 编解码
srv := sse.NewServer(":8080",
    sse.WithCodec("json"),
)
```

### 注册自定义编解码器

```go
import "github.com/tx7do/go-wind-plugins/encoding"

type myCodec struct{}

func (myCodec) Marshal(v any) ([]byte, error) { /* ... */ }
func (myCodec) Unmarshal(data []byte, v any) error { /* ... */ }
func (myCodec) Name() string { return "custom" }

func init() {
    encoding.RegisterCodec(myCodec{})
}
```

## API 参考

| 函数 | 说明 |
|------|------|
| `RegisterCodec(c Codec)` | 注册编解码器（`init()` 中调用，名称大小写无关） |
| `GetCodec(name string) Codec` | 按名称获取编解码器，不存在返回 `nil` |

## 支持的编解码格式

### 文本格式

| 名称 | 包路径 | 说明 |
|------|--------|------|
| `json` | `encoding/json` | JSON（标准库），通用性最强 |
| `xml` | `encoding/xml` | XML（标准库），SOAP / 传统系统 |
| `yaml` | `encoding/yaml` | YAML（gopkg.in/yaml.v3），配置文件 |
| `toml` | `encoding/toml` | TOML（BurntSushi/toml），配置文件 |

### 二进制格式

| 名称 | 包路径 | 说明 |
|------|--------|------|
| `proto` | `encoding/proto` | Protocol Buffers，高性能 RPC |
| `msgpack` | `encoding/msgpack` | MessagePack（vmihailenco/msgpack/v5），紧凑二进制 |
| `bson` | `encoding/bson` | BSON（mongo-driver），MongoDB 原生格式 |
| `cbor` | `encoding/cbor` | CBOR（fxamacker/cbor/v2），RFC 8949，WebAuthn / COSE |
| `gob` | `encoding/gob` | Go Gob（标准库），Go-to-Go 内部通信 |
| `thrift` | `encoding/thrift` | Apache Thrift 二进制协议，需 TStruct 生成代码 |

### Schema 驱动格式

| 名称 | 包路径 | 说明 |
|------|--------|------|
| `avro` | `encoding/avro` | Apache Avro（linkedin/goavro/v2），大数据 / Kafka |
| `flatbuffers` | `encoding/flatbuffers` | Google FlatBuffers，零拷贝序列化 |

## 格式对比

| 格式 | 类型 | 可读性 | 性能 | 体积 | 跨语言 | 适用场景 |
|------|------|--------|------|------|--------|----------|
| JSON | 文本 | 高 | 中 | 大 | 全平台 | API / Web 通用 |
| XML | 文本 | 高 | 低 | 大 | 全平台 | SOAP / 传统系统 |
| YAML | 文本 | 高 | 低 | 中 | 全平台 | 配置文件 |
| TOML | 文本 | 高 | 低 | 中 | 全平台 | 配置文件 |
| Proto | 二进制 | 低 | 高 | 小 | 全平台 | gRPC / 高性能 RPC |
| MsgPack | 二进制 | 低 | 高 | 小 | 全平台 | 微服务内部通信 |
| BSON | 二进制 | 低 | 高 | 中 | 全平台 | MongoDB |
| CBOR | 二进制 | 低 | 高 | 小 | 全平台 | IoT / WebAuthn |
| Gob | 二进制 | 低 | 高 | 小 | **仅 Go** | Go 内部通信 |
| Thrift | 二进制 | 低 | 高 | 小 | 全平台 | Thrift RPC |
| Avro | 二进制 | 低 | 高 | 小 | 全平台 | Kafka / 大数据 |
| FlatBuffers | 二进制 | 低 | 极高 | 小 | 全平台 | 游戏 / 实时系统 |

## 特殊格式说明

### Avro（Schema 驱动）

默认注册的 `"avro"` 编解码器使用空 schema，仅支持原始类型。复杂记录类型需通过 `NewCodec` 创建：

```go
import "github.com/tx7do/go-wind-plugins/encoding/avro"

schema := `{"type":"record","name":"User","fields":[{"name":"name","type":"string"}]}`

codec, err := avro.NewCodec(schema)
if err != nil {
    panic(err)
}

data, err := codec.Marshal(map[string]any{"name": "Alice"})

var result map[string]any
err = codec.Unmarshal(data, &result)
```

### Protobuf

`Marshal` / `Unmarshal` 的参数必须实现 `proto.Message` 接口（即通过 protoc 生成的结构体）：

```go
data, err := codec.Marshal(&myPbMessage{Field: "value"})

var msg myPbMessage
err = codec.Unmarshal(data, &msg)
```

### Thrift

参数必须实现 `thrift.TStruct` 接口（即通过 thrift 编译器生成的结构体）：

```go
data, err := codec.Marshal(&generated.MyStruct{Field: "value"})

var msg generated.MyStruct
err = codec.Unmarshal(data, &msg)
```

### FlatBuffers（零拷贝）

`Marshal` 需要值实现 `FlatBufferMarshaler` 接口，`Unmarshal` 需要目标实现 `flatbuffers.FlatBuffer` 接口。两者均由 `flatc` 编译器生成的代码提供。

## 设计原则

- **禁止手写编解码逻辑**：所有序列化/反序列化必须通过 `encoding.Codec` 进行
- **统一可拔插**：通过 `WithCodec(name)` 一行代码切换格式，无需修改业务逻辑
- **按需加载**：只引入需要的编解码格式，减少二进制体积
- **零侵入**：编解码器与传输层解耦，同一份业务代码适配所有格式
