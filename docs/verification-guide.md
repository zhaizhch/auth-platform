# auth-platform 整套方案验证指南（Envoy Gateway + SecurityPolicy）

> 目标：从**部署**到**端到端验证**的完整操作手册。
> 覆盖：①部署相关服务 → ②确认服务正常启动 → ③如何访问确认 → ④正向 / 反向用例验证。
> 关联文档：[`deploy/README.md`](../deploy/README.md)、
> [Agent 接入指南](agent-developer-guide.md)、[架构](architecture.md)、[安全](security.md)。

---

## 0. 前置条件与关键地址

| 项 | 要求 |
|----|------|
| 集群访问 | `kubectl` 可访问目标集群（namespace `auth-platform`，Gateway 在 `aipilot-system` 的数据面） |
| 工具 | `kubectl`、`curl`、`docker`、`go`、`openssl`、`python3` |
| 自签证书 | Logto 与网关用内网自签 CA，浏览器验证需先信任 `certs/ca.crt`（curl 用 `-k` 即可） |

> 当前测试环境固定值：
>
> | 用途 | 地址 |
> |------|------|
> | 网关入口 | `https://agent2.example.com` / `https://agent1.example.com`（DNS → `172.31.9.53`） |
> | 网关外部 IP | `172.31.9.53` |
> | Logto 用户端/OIDC | `https://172.31.9.58:3001` |
> | Logto 管理控制台 | `https://172.31.9.58:3002` |
> | 测试账号 | `alice` / `bob`，密码 `Test@123456`，角色 `job-manager` |

---

## 1. 第一步：部署相关服务

### 1.1 需要部署的组件

| 组件 | 说明 | 对外暴露 |
|------|------|---------|
| `logto-db` | PostgreSQL，Logto 数据库 | 否（ClusterIP） |
| `logto` | OIDC 认证中心（用户端 3001 / 管理台 3002） | LoadBalancer `172.31.9.58` |
| `agent1` / `agent2` | 示例业务服务（读 `x-auth-*` 头） | 否（ClusterIP） |
| Envoy Gateway 数据面 | 网关代理（`aipilot-system` 命名空间） | 网关 LoadBalancer `172.31.9.53` |

### 1.2 部署

```bash
cd auth-platform
make push-echo            # 构建并推送业务镜像（首次）
make deploy               # 一次性/共享(base) + 全部 Agent（或见下方手动）
```

或手动：

```bash
kubectl apply -f deploy/base/                 # 一次性/共享：namespace、Logto、Gateway、数据面、CA、证书
kubectl apply -f deploy/agents/agent1/  # 每个 Agent 一套
kubectl apply -f deploy/agents/agent2/
```

### 1.3 前置

- Logto 已注册 `agent1` / `agent2` 应用，回调分别为
  `https://agent1.example.com/oauth2/callback` 与 `https://agent2.example.com/oauth2/callback`
  （`deploy/agents/<agent>/03-oidc-secret.yaml` 已填 client-secret）。
- `agent2.example.com`、`agent1.example.com` 的 DNS 指向网关 IP `172.31.9.53`
  （无 DNS 时用 `curl --resolve` 或 `/etc/hosts`）。

---

## 2. 第二步：确认服务正常启动

```bash
kubectl get pods,svc -n auth-platform
kubectl -n auth-platform get gateway,httproute,securitypolicy,envoypatchpolicy
```

期望：
- Pod `logto-*`、`logto-db-*`、`agent1-*`、`agent2-*` 全部 `Running`（`READY 1/1`）；
- `Gateway agents`：`Accepted` + `Programmed`（ADDRESS `172.31.9.53`）；
- 两条 `HTTPRoute`：`Accepted`；
- 两份 `SecurityPolicy`：`Accepted`；
- `EnvoyPatchPolicy eg-trust-logto-ca`：`Accepted` + `Programmed`（CA 已注入数据面）。

查看数据面 Pod（在 `aipilot-system`）：

```bash
kubectl -n aipilot-system get pods -l gateway.envoyproxy.io/owning-gateway-name=agents
```

---

## 3. 第三步：如何访问确认

| 用途 | 地址 | 说明 |
|------|------|------|
| 网关入口 | `https://agent2.example.com` / `https://agent1.example.com` | 唯一入口 |
| Logto OIDC 端点 | `https://172.31.9.58:3001` | authorize / token / jwks |
| Logto 管理控制台 | `https://172.31.9.58:3002` | 注册应用 / 建用户 / 配角色 |

无 DNS 时用 `--resolve`（以 agent2 为例）：

```bash
curl -sk --resolve agent2.example.com:443:172.31.9.53 \
     -o /dev/null -w '%{http_code} %{redirect_url}\n' https://agent2.example.com/
# → 302 ...client_id=agent2... （未登录被拦，跳到 Logto）
```

浏览器登录：打开 `https://agent2.example.com/`（需先信任 `certs/ca.crt`）→ 跳到 Logto →
输入 `alice` / `Test@123456` → 同意授权 → 回跳网关 → 显示身份与角色（`roles: ["job-manager"]`）。

---

## 4. 第四步：如何验证（正向 + 反向）

### 4.1 正向 case（Positive）

| # | 用例 | 操作 | 期望结果 |
|---|------|------|---------|
| P1 | 网关 TLS | `curl -skv https://agent2.example.com/` | 返回 `*.example.com` 证书（AuthPlatform Internal CA） |
| P2 | 未登录被拦截 | `curl -sk --resolve agent2.example.com:443:172.31.9.53 ... https://agent2.example.com/` | `302`（跳到 Logto，`client_id=agent2`） |
| P3 | 多 Agent 分流 | 同样访问 `agent1.example.com` | `302`，`client_id=agent1`（各自 SSO） |
| P4 | 认证中心正常 | `curl -sk https://172.31.9.58:3001/oidc/.well-known/openid-configuration` | 返回 OIDC JSON |
| P5 | 完整登录后访问 | 走完整授权码流程，携带会话 cookie 访问 `/` | `200`，返回 `{"user":{...roles:["job-manager"]}}` |
| P6 | 角色正确注入 | 观察 `X-Auth-Provider` / `X-Auth-Roles` 请求头 | 含 `agent2` / 角色 |
| P7 | 网关配置 | `kubectl -n auth-platform get securitypolicy,httproute` | 全部 `Accepted` |

> `P5` 完整登录核心请求（等价的脚本内实现）：
> ```
> GET  https://agent2.example.com/                         # 302 -> Logto，拿 state / 建 Logto 会话
> PUT  https://172.31.9.58:3001/api/interaction           # {event:SignIn, identifier:{username,password}, redirectTo}
> POST https://172.31.9.58:3001/api/interaction/submit    # -> {redirectTo: resume}
> POST https://172.31.9.58:3001/api/interaction/consent   # 授予 consent -> {redirectTo: resume2}
> GET  resume2                                            # -> 302 到网关 /oauth2/callback?code=...
> GET  https://agent2.example.com/oauth2/callback?code=.. # 换会话 cookie -> 302
> GET  https://agent2.example.com/                        # 携带会话 cookie -> 200 + roles
> ```

### 4.2 反向 case（Negative / 拒绝路径）

| # | 用例 | 操作 | 期望结果 |
|---|------|------|---------|
| N1 | 未登录访问受保护路径 | `curl -sk ... https://agent2.example.com/`（无 cookie） | 不返回业务数据，而是 `302` 到登录页 |
| N2 | 伪造 `x-auth-*` 头直连业务 | 直接访问 `agent2` Service（不经网关） | 业务服务只 ClusterIP，无外部入口 |
| N3 | 无效/过期会话访问 | 使用伪造或过期的 `IdToken-agent2` cookie | `302` 重新登录（`SecurityPolicy.jwt` 验签失败） |
| N4 | 直连业务端口 | 尝试从外部访问 `agent2` / `agent1` | 无外网暴露，无法直连 |

### 4.3 判定标准

- 正向 P1–P7 全部符合预期；
- 反向 N1–N4 全部被正确拒绝；
- 则整套「统一登录 + `id_token` 验签 + 角色注入 + 多 Agent 隔离」方案验证通过。

---

## 5. 注意事项 / 常见坑

1. **secure cookie + HTTPS**：会话 cookie 带 `secure`，浏览器必须走 `https://agent*.example.com`；
   证书为自签，需信任 `certs/ca.crt`。
2. **DNS**：`agent2.example.com` / `agent1.example.com` 必须解析到网关 IP `172.31.9.53`，
   否则用 `curl --resolve`。
3. **token 交换信任 CA**：Logto 是自签证书，须有 `EnvoyPatchPolicy eg-trust-logto-ca`（已 Programmed），
   否则数据面 token 交换报 `CERTIFICATE_VERIFY_FAILED`。
4. **`x-auth-roles` 是 base64**：Envoy 对数组 claim 会 base64 编码，业务侧先解码再 JSON 解析。
5. **生产建议**：改用受信 CA/正式域名证书、轮换测试密钥、Logto 数据改用 PVC。
