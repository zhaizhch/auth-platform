# auth-platform 详细模块说明与数据流转

## 一、模块全景

```
浏览器 ──→ Envoy Gateway（统一入口，Gateway agents）
              ├─ SecurityPolicy.oidc : 授权码+PKCE 登录（每 APP 独立 client/回调）
              ├─ SecurityPolicy.jwt  : 本地 JWKS 验签 id_token → 注入 x-auth-provider/uid/roles
              └─ HTTPRoute → Agent (ClusterIP, 仅经网关可达)
认证中心(标准 OAuth2/OIDC, 多应用) ←→ Envoy Gateway 数据面
```

## 二、组件

### 1. 认证中心（Logto，标准 OAuth2/OIDC）

单一中心，统一身份 + 多应用(client)。签发带 `roles` 的 `id_token`（`access_token` 为 opaque、无 roles）；
暴露 authorize / token / userinfo / jwks / end_session（见 `oauth2-protocol.md`）。

### 2. Envoy Gateway 网关

**唯一入口**，由 Gateway API 声明式定义（一次性/共享在 `deploy/base/`，每个 Agent 一套在
`deploy/agents/<agent>/`，见 `deploy/README.md`）：

- **Gateway**：HTTPS 443，通配证书 `eg-agents-tls`（`*.example.com`），GatewayClass → EnvoyProxy。
- **HTTPRoute**：每个 APP 一条，`hostname` 分流到对应后端 Service。
- **SecurityPolicy.oidc**：未登录 302 认证中心；登录后把 `id_token` 存为会话 cookie（`IdToken-<app>`）。
- **SecurityPolicy.jwt**：`localJWKS` 验签 `id_token`；`claimToHeaders` 注入：
  `x-auth-provider`(aud)、`x-auth-uid`(sub)、`x-auth-roles`(roles，base64 编码数组)。
- **EnvoyPatchPolicy**：把内网 CA 注入 OIDC token 集群的 `trusted_ca`，使数据面能信任自签 Logto。

> 数据面 Pod 由 EG 控制器生成（`aipilot-system`），`EnvoyProxy` 指定 `nodeSelector: bjcjgpu005`
> 以规避节点 seccomp 问题。

### 3. Agent 服务

读网关注入的 `x-auth-provider/uid/x-auth-roles`，按 `x-auth-roles` 授权（只返回 role）；
只开放 ClusterIP。

## 三、数据流转

### 登录
```
1 浏览器 GET /（无会话 cookie）
2 SecurityPolicy.oidc: 302 → 认证中心 authorize（code+PKCE，client_id=该 APP）
3 用户登录 → 认证中心回跳 {redirectURL}?code&state
4 数据面校验 state → code+verifier → token → 签发会话 cookie（IdToken-<app>）
5 302 回原 URL
```

### 访问（核心）
```
6 浏览器 GET /（带 IdToken-<app> cookie）
7 SecurityPolicy.jwt: localJWKS 验签 id_token → 写 x-auth-provider/uid/x-auth-roles
8 HTTPRoute: 按 hostname 分流 → 转发 Agent
9 Agent: 读 x-auth-roles（base64 解码）→ 角色授权 → 返回
```

## 四、设计要点

| 原则 | 体现 |
|------|------|
| 声明式网关 | Envoy Gateway `SecurityPolicy`(oidc+jwt)，无自研 Adapter；验签 `id_token` |
| 只返回角色 | `x-auth-roles` 为唯一授权依据 |
| 多 Agent 隔离 | 每 APP 一条 HTTPRoute + SecurityPolicy，按 Host 分流、各走各的 SSO |
| 防御纵深 | `localJWKS` 验签 + iss/aud/exp；`x-auth-*` 仅由网关注入 |
| 环境适配 | `EnvoyProxy` nodeSelector 规避 seccomp；`EnvoyPatchPolicy` 注入 CA 信任自签 Logto |
