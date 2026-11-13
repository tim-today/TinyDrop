package internal

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ShareItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	IsDir     bool      `json:"is_dir"`
	Size      int64     `json:"size"`
	Mode      string    `json:"mode"` // "map" (zero-copy path mapping) or "copy" (archive copy)
	CreatedAt time.Time `json:"created_at"`
}

type Config struct {
	DeviceName          string      `json:"device_name"`
	Port                int         `json:"port"`
	ShareEnabled        bool        `json:"share_enabled"`
	Password            string      `json:"password"`
	PasswordVersion     string      `json:"password_version"`
	AllowClientEdit     bool        `json:"allow_client_edit"`
	ShareMode           string      `json:"share_mode"` // "map" or "copy"
	TempDir             string      `json:"temp_dir"`
	AutoKeepAlive       bool        `json:"auto_keep_alive"`
	AdminSecret         string      `json:"admin_secret"`
	SharedText          string      `json:"shared_text"`
	SharedTextUpdatedAt int64       `json:"shared_text_updated_at"`
	SharedTextSource    string      `json:"shared_text_source"`
	Items               []ShareItem `json:"items"`
}

type ConfigManager struct {
	mu         sync.RWMutex
	Config     Config
	ConfigPath string
}

func NewConfigManager() (*ConfigManager, error) {
	baseDir := "."
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		baseDir = filepath.Join(home, ".tinydrop")
		_ = os.MkdirAll(baseDir, 0755)
	} else if cwd, err := os.Getwd(); err == nil && cwd != "/" {
		baseDir = cwd
	}

	cfgPath := filepath.Join(baseDir, "tinydrop_config.json")
	defaultTemp := filepath.Join(baseDir, "tinydrop_shared")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		defaultTemp = filepath.Join(home, "TinyDrop_Shared")
	}
	_ = os.MkdirAll(defaultTemp, 0755)

	defaultDeviceName := GetDefaultDeviceName()

	cm := &ConfigManager{
		ConfigPath: cfgPath,
		Config: Config{
			DeviceName:      defaultDeviceName,
			Port:            20999,
			ShareEnabled:    true,
			Password:        Generate6DigitPassword(),
			PasswordVersion: GenerateRandomHex(8),
			AllowClientEdit: false,
			ShareMode:       "map",
			TempDir:         defaultTemp,
			AutoKeepAlive:   true,
			AdminSecret:     GenerateRandomHex(16),
			Items:           []ShareItem{},
		},
	}

	if _, err := os.Stat(cfgPath); err == nil {
		data, err := os.ReadFile(cfgPath)
		if err == nil {
			_ = json.Unmarshal(data, &cm.Config)
		}
	}

	if strings.TrimSpace(cm.Config.DeviceName) == "" {
		cm.Config.DeviceName = defaultDeviceName
	}
	if cm.Config.Port <= 0 || cm.Config.Port > 65535 {
		cm.Config.Port = 20999
	}
	if len(cm.Config.Password) != 6 {
		cm.Config.Password = Generate6DigitPassword()
		cm.Config.PasswordVersion = GenerateRandomHex(8)
	}
	if cm.Config.PasswordVersion == "" {
		cm.Config.PasswordVersion = GenerateRandomHex(8)
	}
	if cm.Config.AdminSecret == "" {
		cm.Config.AdminSecret = GenerateRandomHex(16)
	}
	if cm.Config.ShareMode != "map" && cm.Config.ShareMode != "copy" {
		cm.Config.ShareMode = "map"
	}
	if cm.Config.TempDir == "" {
		cm.Config.TempDir = defaultTemp
	}
	_ = os.MkdirAll(cm.Config.TempDir, 0755)

	_ = cm.Save()
	return cm, nil
}

func (cm *ConfigManager) Save() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.saveLocked()
}

func (cm *ConfigManager) saveLocked() error {
	data, err := json.MarshalIndent(cm.Config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cm.ConfigPath, data, 0644)
}

func (cm *ConfigManager) Update(fn func(c *Config)) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	fn(&cm.Config)
	return cm.saveLocked()
}

func (cm *ConfigManager) GetSnapshot() Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	cp := cm.Config
	itemsCopy := make([]ShareItem, len(cm.Config.Items))
	copy(itemsCopy, cm.Config.Items)
	cp.Items = itemsCopy
	return cp
}

func Generate6DigitPassword() string {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "123456"
	}
	return fmt.Sprintf("%06d", n.Int64()+100000)
}

func GenerateRandomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func GetDefaultDeviceName() string {
	if h, err := os.Hostname(); err == nil {
		h = strings.TrimSpace(h)
		h = strings.TrimSuffix(h, ".local")
		if h != "" {
			return h
		}
	}
	return GenerateRandomHex(4) // 8-character hex
}
