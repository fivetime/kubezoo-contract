#!/usr/bin/env bash
# 把策略层装进一个已经起好的集群。
#
# 为什么策略在 contract 而不在 proxy 或 controller:它们和 Go 代码用的是**同一套
# 租户词汇**,而且是用 JMESPath/CEL **重新实现**了一遍。最直白的例子是
# tenant-own-namespace-name.yaml 里那句:
#
#     regex_replace_all('^[^-]+-', request.namespace, '')
#
# 它算的就是 util.TrimTenantIDPrefix。⭐ 分隔符或租户 ID 长度一改,那条策略会
# **静默地**开始算错 —— 这正是 contract 这个仓库存在的理由。
#
# 用法:  policies.sh <kubectl-context> [租户域名后缀]
set -euo pipefail

CONTEXT=${1:?用法: policies.sh <kubectl-context> [租户域名后缀] [kubezoo 的可达地址]}
DOMAIN_SUFFIX=${2:-${TENANT_DOMAIN_SUFFIX:-apps.example.com}}
# ⛔ 必填,而且以前**漏了**。tenant-api-endpoint 把这个地址作为
# KUBERNETES_SERVICE_HOST 注进每个租户 Pod;占位符没被替换时,租户负载
# **连不上任何 apiserver**,而整套 lab 照样绿 —— 因为没有一条断言是从 Pod 里
# 发出去的。真装一个 operator 才暴露:它的每个 Pod 都在解析那个字面量。
KUBEZOO_ADDRESS=${3:-${KUBEZOO_ADDRESS:-}}
# 允许租户拉镜像的仓库,逗号分隔的主机名(带端口就写端口)。
# ⚠️ 默认只有 docker.io,因为 lab 里租户用的全是 Docker Hub 上的镜像
# (busybox / busybox:1.36 / curlimages/curl)。真实平台请换成自己的。
# 裸名字(busybox)和 org/name(library/busybox)都算 docker.io,见策略里的注释。
TENANT_IMAGE_REGISTRIES=${TENANT_IMAGE_REGISTRIES:-docker.io}
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

if ! kubectl --context "$CONTEXT" get ns kyverno >/dev/null 2>&1; then
  helm repo add kyverno https://kyverno.github.io/kyverno/ >/dev/null 2>&1 || true
  helm repo update kyverno >/dev/null 2>&1 || true
  helm install kyverno kyverno/kyverno -n kyverno --create-namespace \
    --version "${KYVERNO_CHART_VERSION:-3.8.2}" \
    --kube-context "$CONTEXT" --wait --timeout 8m >/dev/null
fi

# 主机名策略里的域名后缀只有在真实部署里才有意义。这里替换成本环境的,而不是
# 原样装一个占位符 —— 那会拒掉每一个租户 Ingress。
cp "$HERE"/config/policy/*.yaml "$WORK/"
sed -i "s/TENANT_DOMAIN_SUFFIX/$DOMAIN_SUFFIX/" "$WORK/tenant-ingress-hostnames.yaml"
# ⚠️ 这个占位符在 CEL 里是一个**列表字面量的内容**,不是一个字符串:
# `[TENANT_IMAGE_REGISTRIES]` 要变成 `["a","b"]`。所以逗号分隔的输入必须逐项加引号,
# 直接塞进去会是 `[a,b]` —— 那是两个未定义的标识符,策略连编译都过不了。
registries_cel=$(printf '%s' "$TENANT_IMAGE_REGISTRIES" | sed 's/[^,]*/"&"/g')
sed -i "s|TENANT_IMAGE_REGISTRIES|$registries_cel|" "$WORK/tenant-image-registries.yaml"
if [ -z "$KUBEZOO_ADDRESS" ]; then
  echo "FATAL: 没有给 kubezoo 的可达地址。装上去的策略会把字面量" >&2
  echo "       KUBEZOO_ADDRESS_PLACEHOLDER 注进每个租户 Pod,而那些 Pod" >&2
  echo "       将连不上任何 apiserver —— 且没有任何断言会发现。" >&2
  echo "       用法:policies.sh <context> <域名后缀> <地址>" >&2
  exit 1
fi
sed -i "s/KUBEZOO_ADDRESS_PLACEHOLDER/$KUBEZOO_ADDRESS/" "$WORK/tenant-api-endpoint.yaml"
kubectl --context "$CONTEXT" apply -f "$WORK/" 2>&1 | grep -v '^Warning' || true

# config/policy/ 里有原生 ValidatingAdmissionPolicy,`get clusterpolicy` 看不到它们。
kubectl --context "$CONTEXT" get validatingadmissionpolicy \
  -o custom-columns=NAME:.metadata.name --no-headers 2>/dev/null | sed 's/^/vap: /'

# ⚠️ 一条列出来但没有状态的策略是**什么都没在管**,而 READY=<none> 读起来像
# "还在同步"而不是"坏了"。这事真发生过:我们自己的一条策略拒掉了 Kyverno 注册
# webhook 所需的写操作,于是三条策略永远不 ready,Pod 全部起不来。
#
# ⛔ 这个守卫**自己烂过一次**:它原先读 `.status.ready`,而 Kyverno 后来改成在
# `.status.conditions[type=Ready]` 里报。于是读到的永远是空 —— 每轮白等满 120s,
# **而且它想守的那件事从此永远守不住**(空和"坏了"长得一模一样)。
# ⇒ 现在读 conditions,并且**一个就绪信号都拿不到就报错退出**,不静默继续:
#   沉默的守卫比没有守卫更坏,因为它让人以为有东西在看着。
READY_JSONPATH='{range .items[*]}{.metadata.name}={.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}'
for _ in $(seq 60); do
  notready=$(kubectl --context "$CONTEXT" get clusterpolicy \
    -o jsonpath="$READY_JSONPATH" 2>/dev/null | grep -cv '=True$' || true)
  [ "$notready" = 0 ] && break
  sleep 2
done
policy_states=$(kubectl --context "$CONTEXT" get clusterpolicy -o jsonpath="$READY_JSONPATH" 2>/dev/null | sed '/^$/d')
echo "$policy_states" | sed 's/^/policy: /'
if [ -n "$policy_states" ] && ! echo "$policy_states" | grep -q '=True$'; then
  echo "FATAL: 一条 ClusterPolicy 都没有报告就绪。" >&2
  echo "       要么策略真的全坏了,要么 Kyverno 又换了报告就绪的字段 ——" >&2
  echo "       两种都必须停下来看:继续跑只会让后面的断言以看不懂的方式失败。" >&2
  exit 1
fi
