# 部署说明（Envoy Gateway + SecurityPolicy）

> 认证中心 + Envoy Gateway 网关 + 各 Agent 业务服务。**一次性/共享部署**在 `base/`，
> **每个 Agent 一套**在 `agents/<agent>/`。全部清单落在 `auth-platform` 命名空间。

## 目录结构

```
deploy/
├── base/                       # 一次性 / 共享部署（部署一次，基本不再动）
│   ├── 00-namespace.yaml       # 命名空间 auth-platform
│   ├── 01-logto.yaml           # 认证中心（Logto，OIDC 3001 / 管理台 3002，LoadBalancer 172.31.9.58）
│   ├── 02-gateway.yaml         # Gateway agents（HTTPS 443，通配证书 eg-agents-tls *.example.com）
│   ├── 03-gatewayclass.yaml    # GatewayClass + EnvoyProxy（nodeSelector 规避 seccomp）
│   ├── 04-envoypatchpolicy.yaml# 把内网 CA 注入 OIDC token 集群（数据面信任自签 Logto）
│   └── 05-gateway-tls.yaml     # 网关通配证书 Secret eg-agents-tls
├── agents/
│   ├── _template/              # 新增 Agent 的模板（占位符 __AGENT__/__DOMAIN__/...）
│   ├── agent1/         # agent1 全套（登录/验签/路由/后端）
│   │   ├── 01-route.yaml           # HTTPRoute（agent1.example.com → 后端）
│   │   ├── 02-securitypolicy.yaml  # SecurityPolicy（oidc 登录 + jwt 验签 + 注入 x-auth-*）
│   │   ├── 03-oidc-secret.yaml     # OIDC client-secret（key 必须 client-secret）
│   │   └── 04-backend.yaml         # 业务服务 Deployment + Service（ClusterIP:8080）
│   └── agent2/                 # agent2 全套（同上）
└── echo-server/                # 示例业务服务源码
```

## 部署

```bash
# 1) 一次性/共享部署（namespace、Logto、Gateway、数据面、CA 信任、通配证书）
kubectl apply -f deploy/base/

# 2) 每个 Agent 一套（按需逐个或全部）
kubectl apply -f deploy/agents/agent1/
kubectl apply -f deploy/agents/agent2/
# 全部: make deploy-agents
```

或一条命令：`make deploy`（= base + 全部 agents）。

## 当前部署形态（已实测）

- `Gateway agents`：Accepted + Programmed，外部 IP **`172.31.9.53`**（`agent*.example.com` DNS 指向它）。
  数据面因 seccomp 问题经 `EnvoyProxy` 落在 `bjcjgpu005`（`base/03-gatewayclass.yaml`）。
- 两份 `SecurityPolicy`：Accepted；`EnvoyPatchPolicy` 已把内部 CA 注入数据面，登录 token 交换
  不再报 `CERTIFICATE_VERIFY_FAILED`。
- 未登录访问 `https://agent2.example.com/` → 302 到 Logto `client_id=agent2`；
  `agent1.example.com` → `client_id=agent1`。**每 Agent 各走各的 SSO。**

## 网关注入给 Agent 的请求头

`SecurityPolicy.jwt.claimToHeaders`（`agents/<agent>/02-securitypolicy.yaml`）：

| 请求头 | claim | 说明 |
|--------|-------|------|
| `x-auth-provider` | `aud` | 应用标识（如 `agent1` / `agent2`） |
| `x-auth-uid` | `sub` | 用户 ID |
| `x-auth-roles` | `roles` | 数组型 claim，Envoy **base64 编码**，Agent 需先 base64 解码再 JSON 解析 |
| `x-auth-custom-data` | `custom_data` | **用户自定义数据整体**（对象型 claim，base64 编码的 JSON） |

## 透传用户自定义数据（一个 JSON）

把用户 `custom_data` 整体随请求带给 Agent，无需逐字段声明：

1. **Logto 侧**：default 租户 `logto_configs`（`key='idToken'`）把 `custom_data` 加入
   `enabledExtendedClaims`；给用户写 `custom_data`。
2. **Agent 侧**：`oidc.scopes` 增加 `custom_data`（否则该 claim 不进 `id_token`）；
   `jwt.claimToHeaders` 加 `x-auth-custom-data` ← `custom_data`。
3. **Agent 代码**：读 `x-auth-custom-data`，base64 解码后 JSON 解析即得完整 `custom_data`；
   以后加字段无需再动网关/Agent。

> 大数据（>2KB）不建议塞 token（cookie ~4KB 限制），改由 Agent 用网关转发的
> `access_token` 调 Logto `/oidc/me`（UserInfo）按需取。

## 新增一个 Agent（agent3）

```bash
# 1) Logto 注册应用，回调加 https://agent3.example.com/oauth2/callback
# 2) 生成全套（复制 _template 并替换占位符）
mkdir -p deploy/agents/agent3
cp deploy/agents/_template/* deploy/agents/agent3/
#    把 agent3 目录里的 __AGENT__/__DOMAIN__/__CLIENT_ID__/__CLIENT_SECRET__ 替换成实际值
# 3) 部署
kubectl apply -f deploy/agents/agent3/
# 4) 给 agent3.example.com 加 DNS → 网关 IP 172.31.9.53
```

> 若 Logto 端改用受信任域名/证书，需同步把各 `02-securitypolicy.yaml` 的
> `issuer / authorizationEndpoint / tokenEndpoint` 换成该域名，且 `jwt.issuer` 与
> `id_token` 的 `iss` 保持一致。
