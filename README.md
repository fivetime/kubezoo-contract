# kubezoo-contract

租户视角与上游集群之间的**翻译规则**,以及描述这个边界的类型。

`kubezoo-proxy`(请求路径)和 `kubezoo-controller`(对账循环)**都**依赖它,而且**只能有这一份**。

## 为什么单独一个仓库

两边都要算前缀:proxy 改写租户发来的请求,controller 派生对象时要算出同样的名字。
**这两边一旦漂移,后果是跨租户 bug,而且是静默的** —— 历史上这类问题的形态都一样:

- Group 主体没被改写 ⇒ 租户把角色绑给 `system:authenticated`,**集群里每个身份**都拿到了它
- namespace 二次前缀 ⇒ operator 读自己的 namespace 全部 NotFound
- CR 的 managedFields 里 apiVersion 没改写 ⇒ 对象建得出来,**第二次 apply 挂掉**

所以这个仓库承载的是**安全边界**,不是"公共工具"。往里加东西之前先问:
**proxy 和 controller 是不是都需要它,并且必须一致?** 不是的话,它属于那两个仓库之一。

## 内容

| | |
|---|---|
| `pkg/convert` | 每种资源的改写规则(名字/namespace/组/引用/主体) |
| `pkg/util` | 前缀的加与减、租户 ID、作用域表、错误改写 |
| `pkg/common` | 标签、保留名字、`ObjectConvertor` 接口 |

## 测试

```
make test            # 单测 + 需要真 apiserver 的那条
make test-unit       # 只跑单测
```

⭐ `make test-integration` 里那条**不是普通单测**:它把作用域表(哪些 kind 是
namespace 级的)拿去和**真实 apiserver 的 discovery** 对照。所有前缀化决策都建立在这张表上,
而**拿表跟表自己对照是查不出表过期的** —— 所以它必须起一个真的 apiserver。

## 策略层(`config/policy/`)

Kyverno 和原生 `ValidatingAdmissionPolicy` 的规则:PSA 等价约束、落点注入、
Ingress 主机名、端点注入、冻结拒写。

⭐ **为什么策略在这个仓库,而不在 proxy 或 controller**:它们用 JMESPath/CEL
**重新实现了一遍**同一套租户词汇。最直白的是 `tenant-own-namespace-name.yaml` 里那句:

```
regex_replace_all('^[^-]+-', request.namespace, '')
```

它算的就是 `util.TrimTenantIDPrefix`。**分隔符或租户 ID 长度一改,那条策略会静默地
开始算错** —— 这正是这个仓库存在的理由。

```
hack/lab/policies.sh <kubectl-context> [租户域名后缀]
```

⚠️ 装完会**等所有 ClusterPolicy 变 ready 再返回**。一条列出来却没有状态的策略是
**什么都没在管**,而 `READY=<none>` 读起来像"还在同步"不像"坏了" —— 这事真发生过:
我们自己的一条策略拒掉了 Kyverno 注册 webhook 需要的写操作,三条策略永远不 ready。
