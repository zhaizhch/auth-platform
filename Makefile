.PHONY: build-echo image-echo push-echo release-echo clean \
	deploy-base deploy-agent deploy-agents deploy restart gateway-status

# 镜像仓库地址（可覆盖: make push-echo REGISTRY=your-registry）
REGISTRY ?= yhg.hub.bjuci.io/library

# ============================================================
# 业务镜像（echo-server）：交叉编译 → 构建 → 推送
# 本机 macOS(arm64)，K8s 集群 linux/amd64，采用「交叉编译 + FROM scratch」。
# ============================================================

build-echo:
	cd deploy/echo-server && \
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
	go build -o ../../echo-server-linux-amd64 .
	@echo "✅ echo-server 二进制: echo-server-linux-amd64"

image-echo: build-echo
	docker build --no-cache --platform linux/amd64 \
		-f deploy/echo-server/Dockerfile.scratch \
		-t $(REGISTRY)/echo-server:latest .
	@echo "✅ echo-server 镜像: $(REGISTRY)/echo-server:latest"

push-echo: image-echo
	docker push $(REGISTRY)/echo-server:latest
	@echo "✅ echo-server 镜像已推送"

release-echo: push-echo

clean:
	rm -rf bin/ echo-server-linux-amd64

# ============================================================
# 部署（Envoy Gateway + SecurityPolicy 方案）
#   base/   一次性/共享：namespace、Logto、Gateway、数据面、CA 信任、通配证书
#   agents/ 每个 Agent 一套：HTTPRoute + SecurityPolicy + OIDC secret + 后端服务
# ============================================================

# 一次性/共享部署
deploy-base:
	kubectl apply -f deploy/base/

# 部署某个 Agent（make deploy-agent NAME=agent3）
deploy-agent:
	@test -n "$(NAME)" || (echo "需要 NAME=（如 agent3）"; exit 1)
	@test -d deploy/agents/$(NAME) || (echo "deploy/agents/$(NAME) 不存在"; exit 1)
	kubectl apply -f deploy/agents/$(NAME)/

# 部署全部 Agent（跳过 _template）
deploy-agents:
	for d in deploy/agents/*/; do \
	  n=$$(basename "$$d"); \
	  [ "$$n" = "_template" ] && continue; \
	  echo "==> $$n"; \
	  kubectl apply -f "deploy/agents/$$n/"; \
	done

# 一键部署：base + 全部 agents
deploy: deploy-base deploy-agents
	@echo "✅ 已应用 deploy/base/ 与 deploy/agents/*/"

# 重启认证中心与业务服务
restart:
	kubectl rollout restart deployment logto logto-db agent1 agent2 -n auth-platform
	kubectl get pods -n auth-platform

# 查看网关与数据面状态
gateway-status:
	kubectl -n auth-platform get gateway,httproute,securitypolicy,envoypatchpolicy
	kubectl -n auth-platform get gateway agents -o wide
