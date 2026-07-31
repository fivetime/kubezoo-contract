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

CONTEXT=${1:?用法: policies.sh <kubectl-context> [租户域名后缀]}
DOMAIN_SUFFIX=${2:-${TENANT_DOMAIN_SUFFIX:-apps.example.com}}
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
kubectl --context "$CONTEXT" apply -f "$WORK/" 2>&1 | grep -v '^Warning' || true

# config/policy/ 里有原生 ValidatingAdmissionPolicy,`get clusterpolicy` 看不到它们。
kubectl --context "$CONTEXT" get validatingadmissionpolicy \
  -o custom-columns=NAME:.metadata.name --no-headers 2>/dev/null | sed 's/^/vap: /'

# ⚠️ 一条列出来但没有状态的策略是**什么都没在管**,而 READY=<none> 读起来像
# "还在同步"而不是"坏了"。这事真发生过:我们自己的一条策略拒掉了 Kyverno 注册
# webhook 所需的写操作,于是三条策略永远不 ready,Pod 全部起不来。
for _ in $(seq 60); do
  notready=$(kubectl --context "$CONTEXT" get clusterpolicy \
    -o jsonpath='{range .items[*]}{.metadata.name}={.status.ready}{"\n"}{end}' 2>/dev/null \
    | grep -cv '=true$' || true)
  [ "$notready" = 0 ] && break
  sleep 2
done
kubectl --context "$CONTEXT" get clusterpolicy \
  -o custom-columns=NAME:.metadata.name,READY:.status.ready --no-headers 2>/dev/null
