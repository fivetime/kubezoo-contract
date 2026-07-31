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
