package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	acosmi "github.com/acosmi/jineng-sdk-go"
)

func main() {
	// nexus-v4 服务地址 (通过 nginx 统一端口或直连后端)
	serverURL := os.Getenv("ACOSMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://127.0.0.1:3300/api/v4"
	}

	// 1. 创建客户端
	client, err := acosmi.NewClient(acosmi.Config{
		ServerURL: serverURL,
		// Store: 默认文件存储 ~/.acosmi/desktop-tokens.json
		// 生产环境可替换为系统钥匙串:
		// Store: &KeychainTokenStore{service: "com.acosmi.desktop"},
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 2. 首次使用需要登录 (会打开浏览器)
	if !client.IsAuthorized() {
		fmt.Println("首次使用，将打开浏览器进行授权...")
		err := client.Login(ctx, "CrabClaw Desktop Agent", []string{
			"models:chat",     // 模型调用
			"entitlements",    // 权益余额
			"skill_store",     // 技能商店浏览/安装
			"tools",           // 工具列表/详情
			"wallet:readonly", // 钱包只读
		})
		if err != nil {
			log.Fatalf("授权失败: %v", err)
		}
		fmt.Println("授权成功!")
	}

	// 3. 查询权益余额
	balance, err := client.GetBalance(ctx)
	if err != nil {
		log.Fatalf("查询余额失败: %v", err)
	}
	fmt.Printf("Token 余额: %d / %d (剩余 %d)\n",
		balance.TotalTokenUsed, balance.TotalTokenQuota, balance.TotalTokenRemaining)
	fmt.Printf("调用次数: %d / %d (剩余 %d)\n",
		balance.TotalCallUsed, balance.TotalCallQuota, balance.TotalCallRemaining)

	// 4. 浏览技能商店
	skills, err := client.BrowseSkillStore(ctx, acosmi.SkillStoreQuery{})
	if err != nil {
		log.Fatalf("浏览技能商店失败: %v", err)
	}
	fmt.Printf("\n技能商店 (%d 个公共技能):\n", len(skills))
	for _, s := range skills {
		fmt.Printf("  - %s [%s] (%s) 下载: %d\n", s.Name, s.Category, s.Author, s.DownloadCount)
	}

	// 5. 获取已安装的工具
	tools, err := client.ListTools(ctx)
	if err != nil {
		log.Fatalf("获取工具列表失败: %v", err)
	}
	fmt.Printf("\n已安装工具 (%d 个):\n", len(tools))
	for _, t := range tools {
		provider := "unknown"
		if t.Provider != nil {
			provider = t.Provider.Name
		}
		fmt.Printf("  - %s [%s] (来源: %s)\n", t.Name, t.Category, provider)
	}

	// 6. 建立 WebSocket 长连接 (实时推送)
	fmt.Println("\n建立 WebSocket 长连接...")
	err = client.Connect(ctx, acosmi.WSConfig{
		Topics: []string{"balance", "skills", "system"},
		OnEvent: func(e acosmi.WSEvent) {
			fmt.Printf("[WS 事件] type=%s topic=%s data=%s\n", e.Type, e.Topic, string(e.Data))
		},
		OnConnect: func() {
			fmt.Println("[WS] 已连接")
		},
		OnDisconnect: func(err error) {
			fmt.Printf("[WS] 断开: %v\n", err)
		},
	})
	if err != nil {
		fmt.Printf("WebSocket 连接失败 (非致命): %v\n", err)
	} else {
		defer client.Disconnect()
		fmt.Printf("WebSocket 已连接: %v\n", client.IsConnected())
	}

	// 7. 获取可用模型
	models, err := client.ListModels(ctx)
	if err != nil {
		log.Fatalf("获取模型列表失败: %v", err)
	}
	fmt.Printf("\n可用模型 (%d 个):\n", len(models))
	for _, m := range models {
		fmt.Printf("  - %s (%s / %s)\n", m.Name, m.Provider, m.ModelID)
	}

	if len(models) == 0 {
		fmt.Println("没有可用模型，请管理员先配置托管模型")
		return
	}

	// 8. 流式聊天
	modelID := models[0].ID
	fmt.Printf("\n使用模型 %s 进行对话:\n", models[0].Name)

	eventCh, errCh := client.ChatStream(ctx, modelID, acosmi.ChatRequest{
		Messages: []acosmi.ChatMessage{
			{Role: "user", Content: "用一句话介绍你自己"},
		},
		MaxTokens: 256,
	})

	fmt.Print("AI: ")
	for event := range eventCh {
		if event.Event == "settled" || event.Event == "started" {
			continue // 跳过非内容事件
		}
		// OpenAI 兼容格式的流式 chunk
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := parseJSON(event.Data, &chunk); err == nil && len(chunk.Choices) > 0 {
			fmt.Print(chunk.Choices[0].Delta.Content)
		}
	}
	fmt.Println()

	if err := <-errCh; err != nil {
		log.Fatalf("流式聊天错误: %v", err)
	}

	// 9. 退出时可选登出
	// client.Logout(ctx)
}

func parseJSON(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}
