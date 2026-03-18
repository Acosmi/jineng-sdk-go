package acosmi

import "time"

// ---------- OAuth ----------

// ServerMetadata OAuth Authorization Server 元数据 (RFC 8414)
type ServerMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RevocationEndpoint    string   `json:"revocation_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// TokenResponse OAuth token 响应
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// TokenSet 持久化 token 对
type TokenSet struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope"`
	ClientID     string    `json:"client_id"`
	ServerURL    string    `json:"server_url"`
}

// IsExpired token 是否已过期 (提前 30 秒视为过期)
func (t *TokenSet) IsExpired() bool {
	return time.Now().After(t.ExpiresAt.Add(-30 * time.Second))
}

// ClientRegistration 动态注册响应
type ClientRegistration struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// ---------- Managed Models ----------

// ManagedModel 托管模型
type ManagedModel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	ModelID   string `json:"modelId"`
	MaxTokens int    `json:"maxTokens"`
	IsEnabled bool   `json:"isEnabled"`
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Messages  []ChatMessage `json:"messages"`
	Stream    bool          `json:"stream,omitempty"`
	MaxTokens int           `json:"max_tokens,omitempty"`
}

// ChatResponse 同步聊天响应
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Choices []struct {
		Index   int         `json:"index"`
		Message ChatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// StreamEvent SSE 流式事件
type StreamEvent struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

// ---------- Entitlements ----------

// EntitlementBalance 权益余额
type EntitlementBalance struct {
	TotalTokenQuota     int64 `json:"totalTokenQuota"`
	TotalTokenUsed      int64 `json:"totalTokenUsed"`
	TotalTokenRemaining int64 `json:"totalTokenRemaining"`
	TotalCallQuota      int   `json:"totalCallQuota"`
	TotalCallUsed       int   `json:"totalCallUsed"`
	TotalCallRemaining  int   `json:"totalCallRemaining"`
	ActiveEntitlements  int   `json:"activeEntitlements"`
}

// ---------- Skill Store ----------

// SkillStoreItem 技能商店中的技能
type SkillStoreItem struct {
	ID            string  `json:"id"`
	PluginID      string  `json:"pluginId"`
	Key           string  `json:"key"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	Icon          string  `json:"icon"`
	Category      string  `json:"category"` // ACTION | TRIGGER | TRANSFORM
	InputSchema   string  `json:"inputSchema"`
	OutputSchema  string  `json:"outputSchema"`
	Timeout       int     `json:"timeout"`
	RetryCount    int     `json:"retryCount"`
	RetryDelay    int     `json:"retryDelay"`
	Version       string  `json:"version"`
	TotalCalls    int64   `json:"totalCalls"`
	AvgDurationMs int64   `json:"avgDurationMs"`
	SuccessRate   float64 `json:"successRate"`
	IsEnabled     bool    `json:"isEnabled"`
	SecurityLevel string  `json:"securityLevel"` // SAFE | WARNING | DANGEROUS
	SecurityScore int     `json:"securityScore"`
	Scope         string  `json:"scope"`   // PUBLIC | TENANT
	Status        string  `json:"status"`  // PENDING | APPROVED | REJECTED
	DownloadCount int64   `json:"downloadCount"`
	Readme        string  `json:"readme"`
	Tags          string  `json:"tags"`
	Author        string  `json:"author"`
	PublisherID   string  `json:"publisherId"`
	IsPublished   bool    `json:"isPublished"`
	PluginName          string  `json:"pluginName"`
	PluginIcon          string  `json:"pluginIcon"`
	UpdatedAt           string  `json:"updatedAt"`
	Visibility          string  `json:"visibility,omitempty"`          // PERSONAL | TENANT | PUBLIC (V3)
	CertificationStatus string  `json:"certificationStatus,omitempty"` // NONE | UNCERTIFIED | TESTING | CERTIFIED | FAILED (V3)
}

// SkillStoreQuery 技能商店搜索参数
type SkillStoreQuery struct {
	Category string // ACTION | TRIGGER | TRANSFORM
	Keyword  string
	Tag      string
}

// SkillSummary 技能统计概览 (V3)
type SkillSummary struct {
	Installed      int64 `json:"installed"`
	Created        int64 `json:"created"`
	Total          int64 `json:"total"`
	StoreAvailable int64 `json:"storeAvailable"`
}

// SkillBrowseResponse 技能商店分页浏览响应 (V3)
type SkillBrowseResponse struct {
	Items    []SkillStoreItem `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"pageSize"`
}

// CertificationStatus 技能认证状态响应 (V3)
type CertificationStatus struct {
	SkillID             string      `json:"skillId"`
	CertificationStatus string      `json:"certificationStatus"` // NONE | UNCERTIFIED | TESTING | CERTIFIED | FAILED
	CertifiedAt         *int64      `json:"certifiedAt,omitempty"`
	SecurityLevel       string      `json:"securityLevel,omitempty"`
	SecurityScore       int         `json:"securityScore"`
	Report              interface{} `json:"report,omitempty"`
}

// ---------- Skill Generator (V3) ----------

// GenerateSkillRequest 技能生成请求 (基于独立 LLM)
type GenerateSkillRequest struct {
	Purpose     string   `json:"purpose"`
	Examples    []string `json:"examples,omitempty"`
	InputHints  string   `json:"inputHints,omitempty"`
	OutputHints string   `json:"outputHints,omitempty"`
	Category    string   `json:"category,omitempty"`
	Language    string   `json:"language,omitempty"`
}

// GenerateSkillResult 技能生成结果
type GenerateSkillResult struct {
	SkillName    string   `json:"skillName"`
	SkillKey     string   `json:"skillKey"`
	Description  string   `json:"description"`
	SkillMd      string   `json:"skillMd"`
	InputSchema  string   `json:"inputSchema"`
	OutputSchema string   `json:"outputSchema"`
	TestCases    []string `json:"testCases"`
	Readme       string   `json:"readme"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags"`
	Timeout      int      `json:"timeout"`
}

// OptimizeSkillRequest 技能优化请求
type OptimizeSkillRequest struct {
	SkillName    string   `json:"skillName"`
	Description  string   `json:"description,omitempty"`
	InputSchema  string   `json:"inputSchema,omitempty"`
	OutputSchema string   `json:"outputSchema,omitempty"`
	Readme       string   `json:"readme,omitempty"`
	Aspects      []string `json:"aspects,omitempty"`
}

// OptimizeSkillResult 技能优化结果
type OptimizeSkillResult struct {
	OptimizedSkill GenerateSkillResult `json:"optimizedSkill"`
	Changes        []string            `json:"changes"`
	Score          int                 `json:"score"`
}

// ---------- Unified Tools ----------

// ToolView 统一工具视图 (Skill 为一等公民)
type ToolView struct {
	ID           string        `json:"id"`
	Key          string        `json:"key"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Icon         string        `json:"icon"`
	Category     string        `json:"category"`
	InputSchema  string        `json:"inputSchema"`
	OutputSchema string        `json:"outputSchema"`
	Timeout      int           `json:"timeout"`
	IsEnabled    bool          `json:"isEnabled"`
	Provider     *ToolProvider `json:"provider,omitempty"`
}

// ToolProvider 工具的来源插件
type ToolProvider struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	SourceType  string `json:"sourceType"` // NATIVE | MCP | WORKFLOW | HTTP | ENGINE
	MCPEndpoint string `json:"mcpEndpoint,omitempty"`
	IsEnabled   bool   `json:"isEnabled"`
}

// ToolListResponse 工具列表响应
type ToolListResponse struct {
	Skills []ToolView `json:"skills"`
	Total  int64      `json:"total"`
}

// ---------- API Response Wrapper ----------

// APIResponse nexus-v4 标准响应
type APIResponse[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}
