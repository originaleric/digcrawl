# 宿主 HTTP 客户端（client）

供**云端宿主或业务服务**调用远端 DigCrawl HTTP Gateway 的 Go SDK。

## 用法

```go
c := client.New("http://127.0.0.1:8787", "<AuthToken>")
manifest, _ := c.Manifest(ctx)
result, _ := c.Call(ctx, protocol.ToolCall{
    ID: "1", Tool: "gateway.ping", Input: map[string]any{},
})
```

支持 `GET /manifest`、`POST /tools/call`、`GET /artifacts/{id}`。

与 **Collector 模式**（本机连宿主 WebSocket）的区别见 [USAGE.md §11](../USAGE.md#11-宿主项目接入go-sdk)。
