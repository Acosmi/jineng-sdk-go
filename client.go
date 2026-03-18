package acosmi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client nexus-v4 桌面智能体 API 客户端
// 自动处理 token 刷新，所有 API 调用线程安全
type Client struct {
	serverURL string
	meta      *ServerMetadata
	tokens    *TokenSet
	store     TokenStore
	http      *http.Client
	mu        sync.RWMutex
	ws        *wsState // WebSocket 长连接状态 (nil = 未连接)
}

// Config 客户端配置
type Config struct {
	// ServerURL nexus-v4 API 根地址，如 http://127.0.0.1:8009 或 http://127.0.0.1:3300/api/v4
	ServerURL string

	// Store token 持久化实现，nil 则使用默认文件存储
	Store TokenStore

	// HTTPClient 自定义 HTTP 客户端，nil 则使用默认
	HTTPClient *http.Client
}

// NewClient 创建客户端 (自动加载已保存的 token)
func NewClient(cfg Config) (*Client, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("ServerURL is required")
	}

	store := cfg.Store
	if store == nil {
		store = NewFileTokenStore("")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	c := &Client{
		serverURL: strings.TrimRight(cfg.ServerURL, "/"),
		store:     store,
		http:      httpClient,
	}

	// 尝试加载已保存的 token
	if tokens, err := store.Load(); err == nil && tokens != nil {
		c.tokens = tokens
	}

	return c, nil
}

// ---------- 授权生命周期 ----------

// IsAuthorized 是否已授权 (有可用 token)
func (c *Client) IsAuthorized() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tokens != nil
}

// Login 完整授权流程: 发现 → 注册 → 授权 → 换 token → 持久化
// appName: 桌面智能体名称 (如 "CrabClaw Desktop")
// scopes: 请求的权限范围
func (c *Client) Login(ctx context.Context, appName string, scopes []string) error {
	// 1. 发现
	meta, err := Discover(ctx, c.serverURL)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}
	c.meta = meta

	// 2. 检查是否已有 client_id
	clientID := ""
	if c.tokens != nil {
		clientID = c.tokens.ClientID
	}
	if clientID == "" {
		reg, err := Register(ctx, meta, appName)
		if err != nil {
			return fmt.Errorf("registration failed: %w", err)
		}
		clientID = reg.ClientID
	}

	// 3. 授权 (打开浏览器)
	result, verifier, err := Authorize(ctx, meta, clientID, scopes)
	if err != nil {
		return fmt.Errorf("authorization failed: %w", err)
	}

	// 4. 换 token
	tokenResp, err := ExchangeCode(ctx, meta, clientID, result.Code, result.RedirectURI, verifier)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	// 5. 持久化
	tokens := NewTokenSet(tokenResp, clientID, c.serverURL)
	c.mu.Lock()
	c.tokens = tokens
	c.mu.Unlock()

	if err := c.store.Save(tokens); err != nil {
		return fmt.Errorf("save tokens: %w", err)
	}

	return nil
}

// Logout 吊销 token 并清除本地存储
func (c *Client) Logout(ctx context.Context) error {
	c.mu.Lock()
	tokens := c.tokens
	c.tokens = nil
	c.mu.Unlock()

	if tokens != nil && c.meta != nil {
		_ = RevokeToken(ctx, c.meta, tokens.AccessToken)
		_ = RevokeToken(ctx, c.meta, tokens.RefreshToken)
	}

	return c.store.Clear()
}

// ---------- Token 管理 ----------

// ensureToken 确保有有效的 access_token，过期则自动刷新
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.RLock()
	tokens := c.tokens
	c.mu.RUnlock()

	if tokens == nil {
		return "", fmt.Errorf("not authorized, call Login() first")
	}

	if !tokens.IsExpired() {
		return tokens.AccessToken, nil
	}

	// 需要刷新
	c.mu.Lock()
	defer c.mu.Unlock()

	// 双检锁: 另一个 goroutine 可能已刷新
	if !c.tokens.IsExpired() {
		return c.tokens.AccessToken, nil
	}

	if c.meta == nil {
		meta, err := Discover(ctx, c.serverURL)
		if err != nil {
			return "", fmt.Errorf("discover for refresh: %w", err)
		}
		c.meta = meta
	}

	tokenResp, err := RefreshToken(ctx, c.meta, c.tokens.ClientID, c.tokens.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh token: %w", err)
	}

	c.tokens = NewTokenSet(tokenResp, c.tokens.ClientID, c.serverURL)
	if err := c.store.Save(c.tokens); err != nil {
		// 不阻断，只记日志
		fmt.Printf("[acosmi-sdk] warning: save refreshed token failed: %v\n", err)
	}

	return c.tokens.AccessToken, nil
}

// ---------- API: Managed Models ----------

// ListModels 获取可用的托管模型列表
func (c *Client) ListModels(ctx context.Context) ([]ManagedModel, error) {
	var resp APIResponse[[]ManagedModel]
	if err := c.doJSON(ctx, http.MethodGet, "/managed-models", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// Chat 同步聊天 (适合短回复)
func (c *Client) Chat(ctx context.Context, modelID string, req ChatRequest) (*ChatResponse, error) {
	req.Stream = false
	var resp ChatResponse
	if err := c.doJSON(ctx, http.MethodPost, "/managed-models/"+modelID+"/chat", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ChatStream 流式聊天 (SSE)，通过 channel 返回事件
// 调用方应遍历 channel 直到关闭，errCh 报告非 nil 错误
func (c *Client) ChatStream(ctx context.Context, modelID string, req ChatRequest) (<-chan StreamEvent, <-chan error) {
	eventCh := make(chan StreamEvent, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		req.Stream = true
		body, _ := json.Marshal(req)

		token, err := c.ensureToken(ctx)
		if err != nil {
			errCh <- err
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.apiURL("/managed-models/"+modelID+"/chat"),
			bytes.NewReader(body))
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Authorization", "Bearer "+token)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := c.http.Do(httpReq)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("stream: HTTP %d: %s", resp.StatusCode, string(bodyBytes))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		var currentEvent string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data == "[DONE]" {
					return
				}
				eventCh <- StreamEvent{Event: currentEvent, Data: data}
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	return eventCh, errCh
}

// ---------- API: Skill Store ----------

// BrowseSkillStore 浏览技能商店 (公共区已审核技能)
func (c *Client) BrowseSkillStore(ctx context.Context, query SkillStoreQuery) ([]SkillStoreItem, error) {
	path := "/skill-store"
	qv := url.Values{}
	if query.Category != "" {
		qv.Set("category", query.Category)
	}
	if query.Keyword != "" {
		qv.Set("keyword", query.Keyword)
	}
	if query.Tag != "" {
		qv.Set("tag", query.Tag)
	}
	if encoded := qv.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var resp APIResponse[[]SkillStoreItem]
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetSkillDetail 获取技能商店中某个技能的详情
func (c *Client) GetSkillDetail(ctx context.Context, skillID string) (*SkillStoreItem, error) {
	var resp APIResponse[SkillStoreItem]
	if err := c.doJSON(ctx, http.MethodGet, "/skill-store/"+skillID, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// InstallSkill 安装技能到当前用户的租户空间
func (c *Client) InstallSkill(ctx context.Context, skillID string) (*SkillStoreItem, error) {
	var resp APIResponse[SkillStoreItem]
	if err := c.doJSON(ctx, http.MethodPost, "/skill-store/"+skillID+"/install", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// DownloadSkill 下载技能 ZIP 包 (返回原始二进制数据)
func (c *Client) DownloadSkill(ctx context.Context, skillID string) ([]byte, string, error) {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return nil, "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.apiURL("/skill-store/"+skillID+"/download"), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download skill: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("download skill: HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read download body: %w", err)
	}

	// 从 Content-Disposition 提取文件名
	filename := "skill.zip"
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if idx := strings.Index(cd, "filename"); idx != -1 {
			parts := strings.SplitN(cd[idx:], "=", 2)
			if len(parts) == 2 {
				filename = strings.Trim(parts[1], "\"' ")
			}
		}
	}

	return data, filename, nil
}

// ---------- API: Skill Store V3 ----------

// GetSkillSummary 获取技能统计概览 (installed/created/total/storeAvailable)
func (c *Client) GetSkillSummary(ctx context.Context) (*SkillSummary, error) {
	var resp APIResponse[SkillSummary]
	if err := c.doJSON(ctx, http.MethodGet, "/skills/summary", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// BrowseSkills 浏览公共技能商店 (V3 分页接口)
func (c *Client) BrowseSkills(ctx context.Context, page, pageSize int, category, keyword, tag string) (*SkillBrowseResponse, error) {
	qv := url.Values{}
	qv.Set("page", fmt.Sprintf("%d", page))
	qv.Set("pageSize", fmt.Sprintf("%d", pageSize))
	if category != "" {
		qv.Set("category", category)
	}
	if keyword != "" {
		qv.Set("keyword", keyword)
	}
	if tag != "" {
		qv.Set("tag", tag)
	}

	var resp APIResponse[SkillBrowseResponse]
	if err := c.doJSON(ctx, http.MethodGet, "/skill-store?"+qv.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// UploadSkill 上传技能 ZIP 包
// scope: "TENANT" (租户级)
// intent: "PERSONAL" (仅自己用) 或 "PUBLIC_INTENT" (走认证→公开)
func (c *Client) UploadSkill(ctx context.Context, zipData []byte, scope, intent string) (*SkillStoreItem, error) {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return nil, err
	}

	// multipart form
	var buf bytes.Buffer
	boundary := "----AcosmiBoundary"
	w := func(field, value string) {
		buf.WriteString("--" + boundary + "\r\n")
		buf.WriteString(fmt.Sprintf("Content-Disposition: form-data; name=\"%s\"\r\n\r\n", field))
		buf.WriteString(value + "\r\n")
	}
	w("scope", scope)
	w("intent", intent)
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Disposition: form-data; name=\"file\"; filename=\"skill.zip\"\r\n")
	buf.WriteString("Content-Type: application/zip\r\n\r\n")
	buf.Write(zipData)
	buf.WriteString("\r\n--" + boundary + "--\r\n")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiURL("/skill-store/upload"), &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 401 时刷新 token 并重试一次 (与 doJSON 行为一致)
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if refreshErr := c.forceRefresh(ctx); refreshErr != nil {
			return nil, fmt.Errorf("upload: unauthorized and refresh failed: %w", refreshErr)
		}
		return c.UploadSkill(ctx, zipData, scope, intent)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upload: HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Data struct {
			Skill SkillStoreItem `json:"skill"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result.Data.Skill, nil
}

// CertifySkill 触发技能认证管线 (异步)
func (c *Client) CertifySkill(ctx context.Context, skillID string) error {
	return c.doJSON(ctx, http.MethodPost, "/skill-store/"+skillID+"/certify", nil, nil)
}

// GetCertificationStatus 查询技能认证状态
func (c *Client) GetCertificationStatus(ctx context.Context, skillID string) (*CertificationStatus, error) {
	var resp APIResponse[CertificationStatus]
	if err := c.doJSON(ctx, http.MethodGet, "/skill-store/"+skillID+"/certification", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ---------- API: Skill Generator (V3) ----------

// GenerateSkill 根据自然语言描述生成技能定义 (基于独立 LLM)
func (c *Client) GenerateSkill(ctx context.Context, req GenerateSkillRequest) (*GenerateSkillResult, error) {
	var resp APIResponse[GenerateSkillResult]
	if err := c.doJSON(ctx, http.MethodPost, "/skill-generator/generate", req, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// OptimizeSkill 优化已有技能定义
func (c *Client) OptimizeSkill(ctx context.Context, req OptimizeSkillRequest) (*OptimizeSkillResult, error) {
	var resp APIResponse[OptimizeSkillResult]
	if err := c.doJSON(ctx, http.MethodPost, "/skill-generator/optimize", req, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ---------- API: Unified Tools ----------

// ListTools 获取当前用户租户下的所有工具 (Skill 优先 + Plugin 兜底)
func (c *Client) ListTools(ctx context.Context) ([]ToolView, error) {
	var resp APIResponse[ToolListResponse]
	if err := c.doJSON(ctx, http.MethodGet, "/tools", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data.Skills, nil
}

// GetTool 获取单个工具详情
func (c *Client) GetTool(ctx context.Context, toolID string) (*ToolView, error) {
	var resp APIResponse[ToolView]
	if err := c.doJSON(ctx, http.MethodGet, "/tools/"+toolID, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ---------- API: Entitlements ----------

// GetBalance 查询当前用户的权益余额
func (c *Client) GetBalance(ctx context.Context) (*EntitlementBalance, error) {
	var resp APIResponse[EntitlementBalance]
	if err := c.doJSON(ctx, http.MethodGet, "/entitlements/balance", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ---------- Internal HTTP ----------

func (c *Client) apiURL(path string) string {
	base := c.serverURL
	// 如果 serverURL 已包含 /api/v4 则不再拼接
	if !strings.Contains(base, "/api/v4") {
		base += "/api/v4"
	}
	return base + path
}

func (c *Client) doJSON(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return err
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.apiURL(path), bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	// 401 时尝试一次 refresh 后重试
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if refreshErr := c.forceRefresh(ctx); refreshErr != nil {
			return fmt.Errorf("unauthorized and refresh failed: %w", refreshErr)
		}
		return c.doJSON(ctx, method, path, body, result)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) forceRefresh(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.tokens == nil {
		return fmt.Errorf("no tokens to refresh")
	}
	if c.meta == nil {
		meta, err := Discover(ctx, c.serverURL)
		if err != nil {
			return err
		}
		c.meta = meta
	}

	tokenResp, err := RefreshToken(ctx, c.meta, c.tokens.ClientID, c.tokens.RefreshToken)
	if err != nil {
		return err
	}

	c.tokens = NewTokenSet(tokenResp, c.tokens.ClientID, c.serverURL)
	_ = c.store.Save(c.tokens)
	return nil
}
