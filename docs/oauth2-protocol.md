# 认证中心协议（标准 OAuth2/OIDC）

认证中心为**标准 OAuth2/OIDC 服务器**，是身份与角色的权威来源。Envoy Gateway 通过
`SecurityPolicy.oidc`（OAuth2 客户端）与 `SecurityPolicy.jwt`（验签方，`localJWKS`）与之交互；
平台外（Agent 服务）只读取网关注入的 `x-auth-roles` 等 `x-auth-*` 头。

> **验证的是 `id_token` 而非 `access_token`**：Logto 默认签发的 `access_token` 是 **opaque**（非 JWT），
> 即便指定 `resource` 换取 JWT 形态的 access_token 也不含 `roles`；`roles` 只在 `id_token` 中。
> 故 `SecurityPolicy.jwt` 从 `IdToken-<app>` cookie 取出 `id_token` 并用 `localJWKS` 验签。

## 使用的标准端点

| 端点 | 协议 | 用途 |
|------|------|------|
| `./.well-known/openid-configuration` | OIDC Discovery | 自动发现端点与 JWKS（可选） |
| `authorization_endpoint` (`/auth`) | OAuth2 | 授权码登录（`response_type=code`） |
| `token_endpoint` (`/token`) | OAuth2 | `code+code_verifier` 换 token / refresh |
| `userinfo_endpoint` (`/userinfo`) | OIDC | 获取用户身份、角色声明 |
| `jwks_uri` (`/jwks`) | OIDC | 验签 token 的公钥（SecurityPolicy.jwt 用 `localJWKS` 内联 JWKS） |
| `end_session_endpoint` | OIDC | 用户登出 |

## 授权码 + PKCE 流程（登录）

```
1. GET {authorize}?response_type=code&client_id&redirect_uri&scope=openid profile roles
     &state&code_challenge=<S256>&code_challenge_method=S256
2. 用户登录
3. 回跳 redirect_uri?code=<code>&state=<state>
4. POST {token} &grant_type=authorization_code&code&code_verifier&redirect_uri&client_id&client_secret
5. 响应: access_token + refresh_token + id_token
6. 响应中的 **`id_token`**（JWT，含 `sub`/`preferred_username`/`roles`）
   → 网关存为 `IdToken-<app>` cookie → `SecurityPolicy.jwt` 用 `localJWKS` 验签
```

## 角色（roles）

- **要求**：认证中心签发的 **`id_token`** **原生带 `roles`**，且各应用的 `roles` 命名一致。
- **下发**：`SecurityPolicy.jwt` 用 `localJWKS` 验签 `id_token` 后，`claimToHeaders` 把 `roles` 透出为
  `x-auth-roles`。`roles` 为数组型 claim，Envoy 会 **base64 编码**，Agent 需先 base64 解码再 JSON 解析。
  **授权只基于 `x-auth-roles`，只返回 role。**
- 若认证中心给出的是 Keycloak 式 `realm_access.roles` / `groups` 等，需在**认证中心侧**统一映射为
  `roles`（本方案不做归一化，见下）。

## Refresh Token

```
POST {token} grant_type=refresh_token&refresh_token&client_id&client_secret
```

## 登出（end_session）

```
GET {end_session}?id_token_hint=<id_token>&post_logout_redirect_uri=<网关BaseURL>
```

## 会话

`SecurityPolicy.oidc` 把登录态存为**加密 + 签名的会话 cookie**，并把 `id_token` 存进 `IdToken-<app>`
cookie（`secure`）；未登录请求由 `SecurityPolicy.oidc` 302 到认证中心登录页。**Agent 服务自身不持有用户
会话**，只读网关注入的 `x-auth-*` 头。
