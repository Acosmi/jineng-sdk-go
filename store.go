package acosmi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// TokenStore token 持久化接口
// 桌面智能体可自行实现 (如 macOS Keychain / Windows Credential Manager)
type TokenStore interface {
	Save(tokens *TokenSet) error
	Load() (*TokenSet, error)
	Clear() error
}

// FileTokenStore 基于文件的 token 存储 (开发/测试用)
// 生产环境建议替换为系统钥匙串实现
type FileTokenStore struct {
	path string
	mu   sync.Mutex
}

// NewFileTokenStore 创建文件 token 存储
// 默认路径: ~/.acosmi/desktop-tokens.json
func NewFileTokenStore(path string) *FileTokenStore {
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".acosmi", "desktop-tokens.json")
	}
	return &FileTokenStore{path: path}
}

func (s *FileTokenStore) Save(tokens *TokenSet) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create token dir: %w", err)
	}

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tokens: %w", err)
	}

	return os.WriteFile(s.path, data, 0600)
}

func (s *FileTokenStore) Load() (*TokenSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read token file: %w", err)
	}

	var tokens TokenSet
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("unmarshal tokens: %w", err)
	}
	return &tokens, nil
}

func (s *FileTokenStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(s.path)
}
