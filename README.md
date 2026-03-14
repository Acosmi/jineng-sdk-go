# Acosmi Jineng SDK for Go

**acosmi/jineng-sdk-go** 是创宇太虚 (Acosmi) 技能 (Jineng) 生态的 Go 语言 SDK。

在 OAuth 授权的基础上，提供技能商店浏览/安装/下载、统一工具管理、托管模型调用等完整能力，让第三方桌面智能体快速接入 Acosmi 技能生态。

## 安装

```bash
go get github.com/acosmi/jineng-sdk-go
```

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    acosmi "github.com/acosmi/jineng-sdk-go"
)

func main() {
    client, err := acosmi.NewClient(acosmi.Config{
        ServerURL: "http://127.0.0.1:3300/api/v4",
    })
    if err != nil {
        log.Fatal(err)
    }

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    // 首次使用需要登录 (自动打开浏览器)
    if !client.IsAuthorized() {
        err := client.Login(ctx, "My Agent", []string{
            "models:chat", "skill_store", "tools", "entitlements",
        })
        if err != nil {
            log.Fatal(err)
        }
    }

    // 浏览技能商店
    skills, _ := client.BrowseSkillStore(ctx, acosmi.SkillStoreQuery{})
    for _, s := range skills {
        fmt.Printf("技能: %s [%s] by %s\n", s.Name, s.Category, s.Author)
    }

    // 获取已安装工具
    tools, _ := client.ListTools(ctx)
    for _, t := range tools {
        fmt.Printf("工具: %s [%s]\n", t.Name, t.Category)
    }
}
```

## 功能特性

| 功能 | 描述 |
|------|------|
| **OAuth 2.1 + PKCE** | 完整的授权码流程，无需 client_secret |
| **动态客户端注册** | RFC 7591，自动注册桌面客户端 |
| **Token 自动刷新** | access_token 过期前自动刷新，调用方无感 |
| **Token 持久化** | 内置文件存储，支持自定义实现（如系统钥匙串） |
| **技能商店** | 浏览/搜索/安装/下载技能 ZIP 包 |
| **统一工具管理** | 查询租户下所有工具（Skill + Plugin） |
| **托管模型调用** | 同步聊天 + SSE 流式聊天 |
| **权益查询** | 查询 Token 余额和调用次数 |
| **线程安全** | 所有 API 调用线程安全 |

## API 参考

### 授权

```go
client.Login(ctx, "appName", scopes)   // 完整 OAuth 流程
client.Logout(ctx)                      // 吊销 token
client.IsAuthorized()                   // 检查授权状态
```

### 技能商店

```go
// 浏览/搜索技能
skills, _ := client.BrowseSkillStore(ctx, acosmi.SkillStoreQuery{
    Category: "ACTION",     // ACTION | TRIGGER | TRANSFORM
    Keyword:  "搜索",
    Tag:      "web",
})

// 获取技能详情
detail, _ := client.GetSkillDetail(ctx, skillID)

// 安装技能到租户
installed, _ := client.InstallSkill(ctx, skillID)

// 下载技能 ZIP 包
data, filename, _ := client.DownloadSkill(ctx, skillID)
```

### 统一工具

```go
tools, _ := client.ListTools(ctx)       // 获取所有工具
tool, _ := client.GetTool(ctx, toolID)  // 获取工具详情
```

### 模型调用

```go
// 同步聊天
resp, _ := client.Chat(ctx, modelID, acosmi.ChatRequest{...})

// 流式聊天 (SSE)
eventCh, errCh := client.ChatStream(ctx, modelID, acosmi.ChatRequest{...})
```

### 权益

```go
balance, _ := client.GetBalance(ctx)
```

## 自定义 Token 存储

实现 `TokenStore` 接口替换默认文件存储：

```go
type TokenStore interface {
    Save(tokens *TokenSet) error
    Load() (*TokenSet, error)
    Clear() error
}
```

## 完整示例

请参考 [example/main.go](./example/main.go) 获取完整的使用示例。

## 支持平台

- macOS
- Linux
- Windows

## 许可证

PolyForm Noncommercial License 1.0.0
