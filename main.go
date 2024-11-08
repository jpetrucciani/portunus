package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// KeyProvider defines the interface for different key sources
type KeyProvider interface {
	GetKeys(username string) ([]string, error)
}

// Config represents the application configuration
type Config struct {
	Mappings map[string]UserMapping `json:"mappings"`
	Cache    CacheConfig            `json:"cache"`
	GitHub   GitHubConfig           `json:"github,omitempty"`
	GitLab   GitLabConfig           `json:"gitlab,omitempty"`
	LDAP     LDAPConfig             `json:"ldap,omitempty"`
}

type UserMapping struct {
	GitHub     string   `json:"github,omitempty"`
	GitLab     string   `json:"gitlab,omitempty"`
	LDAPUser   string   `json:"ldap,omitempty"`
	StaticKeys []string `json:"static_keys,omitempty"`
}

type CacheConfig struct {
	Enabled bool          `json:"enabled"`
	TTL     time.Duration `json:"ttl"`
	MaxSize int           `json:"max_size"`
}

type GitHubConfig struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
}

type GitLabConfig struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
}

type LDAPConfig struct {
	URL          string `json:"url"`
	BindDN       string `json:"bind_dn"`
	BindPassword string `json:"bind_password"`
	BaseDN       string `json:"base_dn"`
	KeyAttribute string `json:"key_attribute"`
}

// type KeyCache struct {
// 	mu    sync.RWMutex
// 	items map[string]cacheItem
// 	ttl   time.Duration
// }

// type cacheItem struct {
// 	keys      []string
// 	timestamp time.Time
// }

// GitHubProvider implements key fetching from GitHub
type GitHubProvider struct {
	client  *http.Client
	baseURL string
	token   string
}

func NewGitHubProvider(baseURL string, token string) *GitHubProvider {
	if baseURL == "" {
		baseURL = "https://github.com/"
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &GitHubProvider{
		client:  &http.Client{Timeout: 10 * time.Second},
		baseURL: baseURL,
		token:   token,
	}
}

func (p *GitHubProvider) GetKeys(username string) ([]string, error) {
	url := fmt.Sprintf("%s%s.keys", p.baseURL, username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if p.token != "" {
		req.Header.Set("Authorization", "token "+p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	keys := strings.Split(strings.TrimSpace(string(body)), "\n")
	// var keys []struct {
	// 	Key string `json:"key"`
	// }
	// if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
	// 	return nil, err
	// }

	// result := make([]string, len(keys))
	// for i, k := range keys {
	// 	result[i] = k
	// }
	return keys, nil
}

// GitLabProvider implements key fetching from GitLab
type GitLabProvider struct {
	client  *http.Client
	baseURL string
	token   string
}

func NewGitLabProvider(baseURL string, token string) *GitLabProvider {
	if baseURL == "" {
		baseURL = "https://gitlab.com/"
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &GitLabProvider{
		client:  &http.Client{Timeout: 10 * time.Second},
		baseURL: baseURL,
		token:   token,
	}
}

func (p *GitLabProvider) GetKeys(username string) ([]string, error) {
	url := fmt.Sprintf("%s%s.keys", p.baseURL, username)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if p.token != "" {
		req.Header.Set("PRIVATE-TOKEN", p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitLab API returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	keys := strings.Split(strings.TrimSpace(string(body)), "\n")
	// var keys []struct {
	// 	Key string `json:"key"`
	// }
	// if err := json.NewDecoder(resp.Body).Decode(&keys); err != nil {
	// 	return nil, err
	// }

	// result := make([]string, len(keys))
	// for i, k := range keys {
	// 	result[i] = k
	// }
	return keys, nil
}

// LDAPProvider implements key fetching from LDAP
type LDAPProvider struct {
	config LDAPConfig
}

func NewLDAPProvider(config LDAPConfig) *LDAPProvider {
	return &LDAPProvider{config: config}
}

func (p *LDAPProvider) GetKeys(username string) ([]string, error) {
	l, err := ldap.DialURL(p.config.URL)
	if err != nil {
		return nil, err
	}
	defer l.Close()

	if err := l.Bind(p.config.BindDN, p.config.BindPassword); err != nil {
		return nil, err
	}

	searchRequest := ldap.NewSearchRequest(
		p.config.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(uid=%s)", username),
		[]string{p.config.KeyAttribute},
		nil,
	)

	result, err := l.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	if len(result.Entries) == 0 {
		return nil, fmt.Errorf("user not found: %s", username)
	}

	entry := result.Entries[0]
	keys := entry.GetAttributeValues(p.config.KeyAttribute)
	return keys, nil
}

// KeyManager orchestrates the key providers and caching
type KeyManager struct {
	config Config
	// cache  *KeyCache
	github *GitHubProvider
	gitlab *GitLabProvider
	ldap   *LDAPProvider
}

func NewKeyManager(configPath string) (*KeyManager, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}

	km := &KeyManager{
		config: config,
	}

	// if config.Cache.Enabled {
	// 	km.cache = &KeyCache{
	// 		items: make(map[string]cacheItem),
	// 		ttl:   config.Cache.TTL,
	// 	}
	// }

	// if config.GitHub.Token != "" {
	km.github = NewGitHubProvider(config.GitHub.URL, config.GitHub.Token)
	// }

	// if config.GitLab.URL != "" {
	km.gitlab = NewGitLabProvider(config.GitLab.URL, config.GitLab.Token)
	// }

	if config.LDAP.URL != "" {
		km.ldap = NewLDAPProvider(config.LDAP)
	}

	return km, nil
}

func (km *KeyManager) GetKeys(username string) ([]string, error) {
	mapping, ok := km.config.Mappings[username]
	if !ok {
		return nil, fmt.Errorf("no mapping found for user: %s", username)
	}

	// if km.cache != nil {
	// 	if keys, ok := km.cache.Get(username); ok {
	// 		return keys, nil
	// 	}
	// }

	var allKeys []string

	// Add static keys if present
	if len(mapping.StaticKeys) > 0 {
		allKeys = append(allKeys, fmt.Sprintf("# static: %s", username))
		allKeys = append(allKeys, mapping.StaticKeys...)
	}

	// Fetch from GitHub if configured
	if mapping.GitHub != "" && km.github != nil {
		keys, err := km.github.GetKeys(mapping.GitHub)
		if err != nil {
			log.Printf("Error fetching GitHub keys for %s: %v", username, err)
		} else {
			allKeys = append(allKeys, fmt.Sprintf("# github: %s (%s)", username, mapping.GitHub))
			allKeys = append(allKeys, keys...)
		}
	}

	// Fetch from GitLab if configured
	if mapping.GitLab != "" && km.gitlab != nil {
		keys, err := km.gitlab.GetKeys(mapping.GitLab)
		if err != nil {
			log.Printf("Error fetching GitLab keys for %s: %v", username, err)
		} else {
			allKeys = append(allKeys, fmt.Sprintf("# gitlab: %s (%s)", username, mapping.GitLab))
			allKeys = append(allKeys, keys...)
		}
	}

	// Fetch from LDAP if configured
	if mapping.LDAPUser != "" && km.ldap != nil {
		keys, err := km.ldap.GetKeys(mapping.LDAPUser)
		if err != nil {
			log.Printf("Error fetching LDAP keys for %s: %v", username, err)
		} else {
			allKeys = append(allKeys, fmt.Sprintf("# ldap: %s", username))
			allKeys = append(allKeys, keys...)
		}
	}

	if len(allKeys) == 0 {
		return nil, fmt.Errorf("no keys found for user: %s", username)
	}

	// if km.cache != nil {
	// 	km.cache.Set(username, allKeys)
	// }

	return allKeys, nil
}

func loadConfig(path string) (Config, error) {
	var config Config
	file, err := os.Open(path)
	if err != nil {
		return config, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	return config, err
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config-path> <username>\n", os.Args[0])
		os.Exit(1)
	}

	configPath := os.Args[1]
	username := os.Args[2]

	km, err := NewKeyManager(configPath)
	if err != nil {
		log.Fatalf("Error initializing key manager: %v", err)
	}

	keys, err := km.GetKeys(username)
	if err != nil {
		log.Fatalf("Error getting keys: %v", err)
	}

	for _, key := range keys {
		fmt.Println(key)
	}
}
