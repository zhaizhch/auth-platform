# 安全设计文档（Envoy Gateway + SecurityPolicy）

## 威胁模型

| 威胁 | 缓解措施 |
|------|---------|
| token 伪造/重放 | `SecurityPolicy.jwt` 用 `localJWKS` 验签 `id_token`，校验 `iss/aud/exp` |
| `x-auth-*` 头伪造 | 只由网关注入；Agent 仅经网关访问（ClusterIP） |
| Agent 被外部直连 | Agent 只开 ClusterIP，不对外暴露 LoadBalancer/NodePort |
| 授权码劫持 | 标准 OAuth2 PKCE + 一次性 state |
| 登录会话被篡改 | `SecurityPolicy.oidc` 会话 cookie 加密 + 签名（EG 数据面） |
| 会话 cookie 被明文窃取 | `IdToken-<app>` 等会话 cookie 带 `secure` 属性，**必须走 HTTPS** |
| client_secret 泄露 | K8s Secret 注入（`deploy/agents/<agent>/03-oidc-secret.yaml`），不写镜像/仓库 |
| 中间人攻击 | 全链路 TLS；数据面与 Logto 之间用 HTTPS（`EnvoyPatchPolicy` 注入 CA 信任） |

## 强制流量经网关

- Agent 服务**不对外暴露**（ClusterIP，无 LoadBalancer/NodePort）；
- 外部唯一入口是 Envoy Gateway 的 `HTTPRoute` + `SecurityPolicy`；
- 新增 Agent 的业务服务保持 ClusterIP，不额外开外部入口。

## 认证中心对接

- 单一标准 OAuth2/OIDC（Logto），`id_token` 携带 `roles`（`access_token` 为 opaque、无 roles）；启用 PKCE；
- `SecurityPolicy.jwt` 用 `localJWKS` 校验 `id_token`；`roles` 命名在各应用间一致；
- 登出用标准 end_session，记录审计日志。

## 密钥管理

- `client-secret`：认证中心注册应用的客户端密钥，K8s Secret 注入（`deploy/agents/<agent>/03-oidc-secret.yaml`）；
- 通配证书 `eg-agents-tls`（`*.example.com`）与 Logto 证书 `logto-tls` 均为内网 CA 签发；
- 定期轮换（90 天）。

## TLS / 日志

- 全链路 HTTPS；Gateway 对外 443，数据面与 Logto 之间 HTTPS（`EnvoyPatchPolicy` 将内网 CA 注入
  OIDC token 集群的 `trusted_ca`）；
- 会话 cookie 带 `secure`，**必须 HTTPS**；
- 日志不打印 token / `client_secret`；
- 数据面 admin 端口不对外暴露。

## 生产检查清单

- [ ] 认证中心 `id_token` 带统一 `roles`
- [ ] `SecurityPolicy.jwt` 指向 `localJWKS`，对 `id_token` 启用 `iss/aud/exp` 校验
- [ ] Agent 只开 ClusterIP，不对外暴露
- [ ] `deploy/agents/<agent>/03-oidc-secret.yaml` 用真实 client-secret，未提交到 Git
- [ ] 全链路 TLS；日志不打 token
- [ ] rate limiting、审计日志、密钥轮换、Logto 数据持久化（PVC）
