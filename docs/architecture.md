# auth-platform 架构文档（Envoy Gateway + SecurityPolicy）

## 设计动机

认证中心为**标准 OAuth2/OIDC**。为避免每个 Agent 服务都重复实现 OAuth 客户端、token 管理与角色提取，
平台让 **Envoy Gateway** 用原生 `SecurityPolicy`（`oidc` + `jwt`）统一完成登录与验签，并把身份/角色以
`x-auth-*` 头带给 Agent。**所有去往 Agent 的请求都必须经过 Envoy Gateway**。Agent 零感知接入。

## 核心设计决策

### D1: 声明式网关（Envoy Gateway + SecurityPolicy）

| 环节 | 承担者 |
|------|--------|
| 登录（授权码 + PKCE） | `SecurityPolicy.oidc`（每 APP 一份，各自 client_id / 回调域名） |
| 验签（认证中心公钥） | `SecurityPolicy.jwt` + `localJWKS`（验签 `id_token`） |
| 角色下发 | `claimToHeaders` → `x-auth-roles`（数组 base64 编码） |
| 身份 | `x-auth-provider`（aud）、`x-auth-uid`（sub） |
| 按 Host 分流 | 每 APP 一条 `HTTPRoute`（hostname → 后端 Service） |

> **为什么验签 `id_token`**：认证中心为 Logto，其 `access_token` 默认是 **opaque**（非 JWT），
> 即便用 `resource` 参数换取 JWT 形态的 access_token，其中也**不含 `roles`**（`roles` 只在 `id_token`）。
> 因此从 `id_token` 提取并验签 `roles`。
>
> **`x-auth-roles` 的编码**：Envoy `claimToHeaders` 对数组型 claim 会 **base64 编码**，
> Agent 需先 base64 解码再 JSON 解析（参考 `deploy/echo-server/main.go`）。

### D2: 只返回角色

授权依据即 `x-auth-roles`（认证中心 `roles` 声明原样透出），不再下发权限码/scope，语义简单。

### D3: 多 Agent 隔离（每 APP 独立 SSO）

同一 Envoy Gateway、同一 Logto，每个 APP 一条 `HTTPRoute` + 一份 `SecurityPolicy`
（各自 `clientID` / `clientSecret` / `redirectURL` / `audiences`）。按 Host 分流，各走各的 SSO。

### D4: 防御纵深

`SecurityPolicy.jwt` 用 `localJWKS` 校验 token 签名、`iss`/`aud`/`exp`；
`x-auth-*` 头只由网关注入，Agent 只经网关访问。`EnvoyPatchPolicy` 把内网 CA 注入数据面
`trusted_ca`，使 token 交换能信任自签的 Logto。

### D5: 数据面与 seccomp

EG 数据面 Pod 由控制器生成（`aipilot-system` 命名空间）；因节点 runc/seccomp 问题，用
`EnvoyProxy` 的 `nodeSelector: bjcjgpu005` 指定可运行节点（`deploy/base/03-gatewayclass.yaml`）。

## 登录 / 访问链路

```
登录:
浏览器 → Envoy Gateway(SecurityPolicy.oidc) → 302 认证中心 → 用户登录 → 认证中心回跳
       → 数据面换 token（EnvoyPatchPolicy 注入 CA 以信任 Logto）→ 发会话 cookie → 回原 URL

访问:
浏览器(带 cookie) → Envoy Gateway(SecurityPolicy.jwt + localJWKS) 验签 id_token
       → 写 x-auth-provider/uid/x-auth-roles 头 → HTTPRoute 分流 → 转发 Agent
```

## 组件

- **认证中心（Logto）**：单一标准 OAuth2/OIDC，身份与角色权威来源，多应用(client)。
- **Envoy Gateway**：唯一入口，`Gateway` + 每 APP 的 `HTTPRoute`/`SecurityPolicy`，登录、验签、角色注入、路由。
- **Agent 服务**：读 `x-auth-provider/uid/x-auth-roles`，按角色授权（只返回 role）。
