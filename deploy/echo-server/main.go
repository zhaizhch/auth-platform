// echo-server 极简用户信息服务
// 读取 Envoy Gateway SecurityPolicy(oidc+jwt) 验签后注入的 x-auth-* 头（身份 + 角色）。
// 授权只基于 x-auth-roles（角色），身份头用于展示/审计。
// 注意：本服务只能被 Envoy 访问（NetworkPolicy 强制），不对外暴露。
package main

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"os"
)

// userClaims 只返回身份与角色（授权只依赖 Roles）
type userClaims struct {
	Username   string         `json:"username"`
	UserID     string         `json:"uid"`
	Provider   string         `json:"provider"`
	Roles      []string       `json:"roles"`
	CustomData map[string]any `json:"custom_data,omitempty"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}

	http.HandleFunc("/", handler)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "service": "userinfo-demo"})
	})

	log.Printf("userinfo-demo 启动于 :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handler(w http.ResponseWriter, r *http.Request) {
	u, ok := userFromRequest(r)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"error": "未登录"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"user": u})
}

// userFromRequest 从 Envoy 注入的 x-auth-* 头构造用户信息
func userFromRequest(r *http.Request) (*userClaims, bool) {
	if r.Header.Get("x-auth-provider") == "" {
		return nil, false // 未认证（网关不会放行，理论上到不了这里）
	}
	return &userClaims{
		Provider:   r.Header.Get("x-auth-provider"),
		UserID:     r.Header.Get("x-auth-uid"),
		Username:   r.Header.Get("x-auth-username"),
		Roles:      parseJSONArray(r.Header.Get("x-auth-roles")),
		CustomData: parseJSONObject(r.Header.Get("x-auth-custom-data")),
	}, true
}

// parseJSONArray 解析 x-auth-roles 头。Envoy claimToHeaders 对数组型 claim 会 base64 编码，
// 因此先尝试 base64 解码，再 JSON 解析为字符串数组（如 ["admin","viewer"]）。
func parseJSONArray(s string) []string {
	if s == "" {
		return []string{}
	}
	// 若为 base64 编码（Envoy claim_to_headers 对数组 claim 的处理），先解码
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		var out []string
		if err := json.Unmarshal(b, &out); err == nil {
			return out
		}
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []string{}
	}
	return out
}

// parseJSONObject 解析 x-auth-custom-data 头。对象型 claim 会被 Envoy base64 编码，
// 先 base64 解码再 JSON 解析为 map；失败则当原文尝试。
func parseJSONObject(s string) map[string]any {
	if s == "" {
		return nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		b = []byte(s)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}
