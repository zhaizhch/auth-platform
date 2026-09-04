# Agent 服务接入指南（Envoy Gateway + SecurityPolicy）

## 前置条件

1. **单一标准 OAuth2/OIDC 认证中心（Logto）**已就绪，`id_token` 原生带 `roles`（各应用命名一致）。
2. auth-platform 已部署（Envoy Gateway），见 [`deploy/README.md`](../deploy/README.md)。
3. 网关入口：`https://agent2.example.com` / `https://agent1.example.com`（DNS → 网关 IP `172.31.9.53`）。

## 唯一入口：Envoy Gateway

**所有去往 Agent 的请求都必须经过 Envoy Gateway**：Agent 服务只开放 ClusterIP（不对外暴露），
外部唯一入口是网关的 `HTTPRoute` + `SecurityPolicy`。

## 步骤 1：在认证中心注册应用

为 Agent 在 Logto 注册一个 Traditional 应用，拿到 `client_id` / `client_secret`，
并允许回调 `redirect_uri = https://<DOMAIN>/oauth2/callback`。
把 `client-secret` 写入 `deploy/agents/<agent>/03-oidc-secret.yaml`。

## 步骤 2：配置网关（deploy/agents/<agent>/）

在 `deploy/agents/<agent>/` 里为这个 Agent：
- `01-route.yaml`：一条 `HTTPRoute`，`hostnames` 为该 Agent 域名，`backendRefs` 指向其后端 Service；
- `02-securitypolicy.yaml`：`SecurityPolicy`，改 `targetRefs.name / clientID / redirectURL /
  cookieNames / audiences`；
- `03-oidc-secret.yaml`：该 APP 的 `client-secret`（key 必须 `client-secret`）；
- `04-backend.yaml`：业务服务 Deployment + Service（ClusterIP:8080）。

> 可复制 `deploy/agents/_template/` 生成新 Agent 全套，并把
> `__AGENT__/__DOMAIN__/__CLIENT_ID__/__CLIENT_SECRET__` 替换成实际值。

> Logto `access_token` 是 opaque、无 `roles`，因此校验 **`id_token`**（`SecurityPolicy.jwt`）。

## 步骤 3：添加业务服务（ClusterIP）

部署业务服务为 ClusterIP（不对外暴露），端口 8080，参考 `deploy/agents/agent2/04-backend.yaml`。

## 步骤 4：业务服务读取身份与角色

网关注入的请求头：

| 请求头 | 内容 |
|--------|------|
| `x-auth-provider` | 应用标识（如 `agent1`、`agent2`） |
| `x-auth-uid` | 用户 ID |
| `x-auth-roles` | **base64 编码**的 JSON 数组（解码后如 `["job-manager"]`，**唯一授权依据**） |

业务服务只需读 `x-auth-roles`（**先 base64 解码再 JSON 解析**）做角色授权，用 `x-auth-provider` 做应用隔离。

> 只要 Agent 只被网关访问即可信任 `x-auth-roles`；若可能被绕过直连，需再对 `Authorization` token 本地验签。

参考实现见 `deploy/echo-server/main.go`。

## 步骤 5：Agent 间调用

下游透传 `x-auth-roles`（连同 `x-auth-provider`）即可。

## 安全注意事项

1. 认证中心 `id_token` 必须带 `roles`，且签名受 `SecurityPolicy.jwt`（`localJWKS`）校验（`iss`/`aud`/`exp`）；
2. Agent 只经网关访问（ClusterIP），绝不对外暴露 LoadBalancer/NodePort；
3. `client-secret` 通过 K8s Secret 注入（`deploy/agents/<agent>/03-oidc-secret.yaml`）；启用 PKCE；
4. 按 `roles` 最小授权、按应用隔离；日志不打印 token；
5. 全链路 TLS；会话 cookie 带 `secure`，**必须 HTTPS**。
