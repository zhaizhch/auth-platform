# auth-platform 架构汇报（Envoy Gateway + SecurityPolicy）

> 面向架构师：基于标准 OAuth2/OIDC 认证中心的集中认证鉴权平台。
> **无 Adapter、无自研 Envoy 配置**，改用声明式的 **Envoy Gateway + SecurityPolicy**。

## 1. 定位

为 Agent 服务提供集中**认证 + 角色授权**。Envoy Gateway 作为唯一入口，用原生 `SecurityPolicy`
（`oidc` + `jwt`）对接**单一认证中心**完成登录与验签，把身份/角色以 `x-auth-*` 头带给 Agent。
**所有去往 Agent 的请求都必须经过 Envoy Gateway**。

## 2. 架构

```
浏览器 ──→ Envoy Gateway (唯一入口, Gateway agents, https://agent*.example.com:443)
             ├─ SecurityPolicy.oidc : 授权码+PKCE 登录（每 APP 独立 client_id / 回调域名）
             ├─ SecurityPolicy.jwt  : 本地 JWKS 验签 id_token → 注入 x-auth-*
             └─ HTTPRoute: 按 Host 分流 → Agent 服务 (ClusterIP)
认证中心(Logto, 标准 OAuth2/OIDC, 多应用) ←→ Envoy Gateway 数据面
```

## 3. 组件职责

| 组件 | 职责 |
|------|------|
| 认证中心 | 身份与角色权威来源；标准 OAuth2/OIDC，多应用(client) |
| Envoy Gateway | 唯一入口：每 APP 一条 `HTTPRoute` + `SecurityPolicy`（oidc 登录 + jwt 验签 + 注入 `x-auth-*`） |
| Agent 服务 | 读 `x-auth-provider/uid/x-auth-roles`，按 `roles` 授权（只返回 role） |

## 4. 核心设计

- 声明式网关：登录用 `SecurityPolicy.oidc`，验签用 `SecurityPolicy.jwt`（`localJWKS`），按 Host 用 `HTTPRoute` 分流；
- **校验 `id_token`**：Logto `access_token` 为 opaque、无 `roles`，`roles` 只在 `id_token`；
- 多 Agent 隔离：每 APP 一份 SecurityPolicy（各自 client_id / 回调域名 / audiences），各走各的 SSO；
- 只返回角色：授权依据即 `x-auth-roles`；
- 防御纵深：`localJWKS` 验签 + 校验 `iss/aud/exp`；`x-auth-*` 只由网关注入；
- 环境适配：`EnvoyProxy` nodeSelector 规避 seccomp；`EnvoyPatchPolicy` 注入 CA 信任自签 Logto。

## 5. 部署文件

| 文件 | 说明 |
|------|------|
| `deploy/base/00-namespace.yaml` | 命名空间 `auth-platform` |
| `deploy/base/01-logto.yaml` | 认证中心（Logto，OIDC 3001 / 管理台 3002，LoadBalancer `172.31.9.58`） |
| `deploy/base/02-gateway.yaml` | `Gateway`（HTTPS 443，通配证书 `eg-agents-tls`） |
| `deploy/base/03-gatewayclass.yaml` | 数据面 nodeSelector（规避 seccomp）+ GatewayClass |
| `deploy/base/04-envoypatchpolicy.yaml` | 注入 CA 到数据面 `trusted_ca`（信任自签 Logto） |
| `deploy/base/05-gateway-tls.yaml` | 网关通配证书 Secret `eg-agents-tls` |
| `deploy/agents/<agent>/01-route.yaml` | `HTTPRoute`（<agent>.example.com → 后端 Service） |
| `deploy/agents/<agent>/02-securitypolicy.yaml` | `SecurityPolicy`（oidc + jwt，每 APP 一份） |
| `deploy/agents/<agent>/03-oidc-secret.yaml` | 该 APP 的 OIDC `client-secret` |
| `deploy/agents/<agent>/04-backend.yaml` | 该 Agent 业务服务（Deployment + Service，ClusterIP） |

## 6. 关键流程

```
登录: 浏览器 → SecurityPolicy.oidc → 认证中心 → 回调换 token → 会话 cookie(IdToken-<app>)
访问: 浏览器(cookie) → SecurityPolicy.jwt 验签 id_token → 写 x-auth-* → HTTPRoute 分流 → Agent
```

## 7. 接入

1. 认证中心注册应用，允许回调 `redirect_uri=/oauth2/callback`；`deploy/agents/<agent>/03-oidc-secret.yaml` 写 client-secret。
2. 复制 `deploy/agents/_template/` 生成该 Agent 全套（`01-route.yaml` HTTPRoute、
   `02-securitypolicy.yaml` SecurityPolicy、`03-oidc-secret.yaml`、`04-backend.yaml` 业务服务），
   替换占位符后 `kubectl apply -f deploy/agents/<agent>/`。
3. `kubectl apply -f deploy/base/`（一次性）；DNS → 网关 IP `172.31.9.53`。

> 详细步骤见 [`integration-guide.md`](integration-guide.md)、[`deploy/README.md`](../deploy/README.md)。
