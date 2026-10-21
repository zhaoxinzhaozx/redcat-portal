package sub2api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type User struct {
	ID    int64
	Email string
	Role  string
}

func (u User) IsAdmin() bool {
	return u.Role == "admin"
}

type Client struct {
	baseURL     string
	adminEmail  string
	adminPass   string
	claimPrefix string
	http        *http.Client

	mu       sync.Mutex
	adminTok string
	adminExp time.Time
}

func New(baseURL, adminEmail, adminPassword, claimPrefix string) *Client {
	if strings.TrimSpace(claimPrefix) == "" {
		claimPrefix = "portal-claim"
	}
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		adminEmail:  adminEmail,
		adminPass:   adminPassword,
		claimPrefix: claimPrefix,
		http:        &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) Me(ctx context.Context, token string) (User, error) {
	body, err := c.getJSON(ctx, "/api/v1/auth/me", token)
	if err != nil {
		return User{}, err
	}
	var payload struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return User{}, err
	}
	if payload.ID == 0 || payload.Email == "" {
		return User{}, fmt.Errorf("主站没有返回用户信息")
	}
	return User{ID: payload.ID, Email: payload.Email, Role: payload.Role}, nil
}

func (c *Client) AddBalance(ctx context.Context, userID int64, amount float64, activityID int64, siteName string) error {
	if c.adminEmail == "" || c.adminPass == "" {
		return fmt.Errorf("未配置发奖管理员账号，暂时不能领取")
	}
	token, err := c.adminToken(ctx)
	if err != nil {
		return err
	}
	label := strings.TrimSpace(siteName)
	if label == "" {
		label = "抽奖"
	}
	payload, _ := json.Marshal(map[string]any{
		"balance":   amount,
		"operation": "add",
		"notes":     fmt.Sprintf("%s活动 #%d 奖励", label, activityID),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/api/v1/admin/users/%d/balance", c.baseURL, userID), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", fmt.Sprintf("%s-%d-%d", c.claimPrefix, activityID, userID))
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("主站入账失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		c.adminTok = ""
		c.mu.Unlock()
	}
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &envelope)
	if resp.StatusCode >= 300 || envelope.Code != 0 {
		message := envelope.Message
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("主站入账失败: %s", message)
	}
	return nil
}

func (c *Client) adminToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.adminTok != "" && time.Now().Before(c.adminExp) {
		return c.adminTok, nil
	}
	payload, _ := json.Marshal(map[string]string{
		"email":    c.adminEmail,
		"password": c.adminPass,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/auth/login", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("管理员登录失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", fmt.Errorf("管理员登录失败")
	}
	var data struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Requires2FA bool   `json:"requires_2fa"`
	}
	_ = json.Unmarshal(envelope.Data, &data)
	if data.Requires2FA {
		return "", fmt.Errorf("发奖账号开启了二次验证，请换一个未开启的管理员账号")
	}
	if envelope.Code != 0 || data.AccessToken == "" {
		return "", fmt.Errorf("管理员登录失败")
	}
	ttl := 10 * time.Minute
	if data.ExpiresIn > 120 {
		ttl = time.Duration(data.ExpiresIn-60) * time.Second
	}
	c.adminTok = data.AccessToken
	c.adminExp = time.Now().Add(ttl)
	return c.adminTok, nil
}

func (c *Client) getJSON(ctx context.Context, path, token string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接主站")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, errUnauthorized
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("主站响应无法识别")
	}
	if envelope.Code != 0 {
		if envelope.Message == "" {
			envelope.Message = "主站请求失败"
		}
		return nil, fmt.Errorf("%s", envelope.Message)
	}
	return envelope.Data, nil
}

var errUnauthorized = fmt.Errorf("请先登录")

func ErrUnauthorized() error {
	return errUnauthorized
}

func IsUnauthorized(err error) bool {
	return err == errUnauthorized
}
