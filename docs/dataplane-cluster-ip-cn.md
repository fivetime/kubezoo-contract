# 数据面契约:租户视角的 Service 地址

**一句话**:数据面把租户真正能拨通的地址写进上游 Service 的注解
`kubezoo.io/cluster-ip`,kubezoo 就把它当作租户视角的 `spec.clusterIP` 报出去。

已实现方:kubezoo-gateway(`pkg/convert/service.go` + `pkg/proxy/proxy.go`)。
待接入方:kubezun(Octavia/OVN)、kubetron。

## 为什么需要它(实测,不是推理)

租户的 pod 是 Zun capsule,接的是**租户自己的 OVN 网络**。上游集群的 service CIDR
(`254.51.x.x`)在那个网络里根本不存在 ⇒ kubezoo 原样报出去的 ClusterIP,
**租户的任何负载都拨不通**。真正能用的是数据面在租户网络上为该 Service 分配的
VIP(Octavia 场景下是 `192.168.200.x`)。

⚠️ 一度以为「Octavia 把 LB 的 VIP 设成该 Service 的 ClusterIP,所以 ClusterIP 可达」——
**那条是错的**,VIP 是在租户网络上分配的,和上游的 ClusterIP 不是同一个地址。
本文档存在的全部理由就是补这个差。

## ⭐ 顺带解决 DNS,而且解析器一行不用改

每租户的 CoreDNS 是**经 kubezoo、以租户身份**读 Service 的
(见 kubezoo-controller 的 `pkg/controller/tenantdns.go`),所以它读到的就是翻译后的值,
答出去的地址直接可用。**不需要 rewrite 规则,不需要渲染 zone。**

## 契约

```yaml
# 上游集群里的 Service,由数据面写
metadata:
  annotations:
    kubezoo.io/cluster-ip: "192.168.200.7"
```

| | |
|---|---|
| 键 | `kubezoo.io/cluster-ip` |
| 值 | 一个 IP 字面量。解析不出 IP 一律当作没写 |
| 写 | **数据面**,写在**上游**对象上 |
| 读 | kubezoo,替换租户视角的 `spec.clusterIP` **与** `spec.clusterIPs[0]` |
| 租户 | **只读**。租户提交一律剥掉 |

⭐ **键放在 `kubezoo.io/` 而不是 `knaas.io/` 或 `kubetron.io/`**:kubezoo 不该知道
这套部署的数据面是谁。任何能为 Service 分配可达地址的实现都填这一个键,
kubezoo 不用为每家改一次。

## ⛔ 方向和 `lbipam.cilium.io/ips` 相反 —— 这是最容易接错的一点

Cilium 那个注解是**租户写的请求**(我要这个 IP),结果落在 `status.loadBalancer.ingress`。
这里是**平台写的回报**,租户只读。

**租户如果能写这个键,它就能让 kubezoo 把任意地址报成自己的 ClusterIP,
而它自己的 CoreDNS 会照着答** ⇒ 该租户内的任何名字都能被指到任何地方。

kubezoo 侧已经堵住:每次写入都剥掉这个键,并把平台存的值放回去。
⚠️ **"放回去"这半不能省** —— 租户每次读都会看到这个注解,所以任何
read-modify-write(`kubectl get -o yaml | kubectl apply -f -`、operator 回写对象)
都会把它带上来。只剥不还,平台的记录就被租户一次 `apply` 抹掉,
租户随即静默退回不可达的地址,直到数据面下次再写一遍。

## kubezoo 的行为(已实现,有守卫测试)

| 情况 | 行为 |
|---|---|
| 注解在、是合法 IP | 报注解的值(`clusterIP` 和 `clusterIPs[0]` 一起翻) |
| 注解缺失 / 空 / 不是 IP | **报上游的 ClusterIP,不报空** |
| 租户自己写了 `clusterIP: None` | 完全不碰,注解在也不覆盖 |
| 租户提交它被展示的那个 VIP | 换回上游真值,静默通过 |
| 租户提交第三个地址 | 拒绝,报错说明地址由平台分配 |
| 租户提交这个注解键 | 剥掉,并把平台存的值放回去 |

⚠️ **`clusterIP` 和 `clusterIPs` 必须一起翻**。上游会拿这两个字段互相校验,
只翻一个:双栈下是被拒的写入,单栈下是一个静默不自洽的对象。

⚠️ **真 headless 不覆盖**。`None` 是租户自己的话(通常是 StatefulSet 的
governing Service),覆盖成单个地址会把 per-pod DNS 变成一个地址,
而且**不会有任何人报告这件事**。

## 对数据面实现方的要求

1. **尽早写,别等 LB 变 ACTIVE。** kubezun 的做法:拿到 Octavia 返回的地址就写
   (~3s),不等 ACTIVE(几十秒)。窗口期内 kubezoo 报的是上游 ClusterIP —— 字段有值、
   语义正常,只是那个地址租户拨不通;窗口越短越好。
2. **幂等**。这是普通的注解写入,重复写同一个值必须无副作用。
3. **Service 删除时不用清理** —— 注解随对象一起消失。这正是选注解而不是
   kubezoo 侧边表的理由:没有 GC 问题,没有两份真相漂移。
4. **不要碰 `spec.clusterIP` 本身**。上游那个值是上游的,改它会撞上
   `may not change once set`。

## ⛔ 不要改成「上游存 headless」

原始提案是上游存 `clusterIP: None`、把要显示的值存在 kubezoo 自己的 etcd 里。
没有采纳,三条理由:

1. **会结构性禁掉租户的 NodePort / LoadBalancer。**
   `k8s.io/kubernetes/pkg/apis/core/validation/validation.go:7022,7026`:
   ```
   clusterIPs[0] may not be set to 'None' for LoadBalancer services
   clusterIPs[0] may not be set to 'None' for NodePort services
   ```
2. **不可变语义会从上游手里掉到 kubezoo 手里。** 租户把一个普通 Service 改成
   `None`,上游看到的是 `None → None`,**直接放行** —— 而 k8s 本来禁止这个转换。
   整套规则就得在 kubezoo 里重写一遍,而**写漏了不报错**。
3. **「区分租户写的 None vs 存储层 headless」这个难题会自动消失** —— 因为上游存的
   就是租户的原意。

代价是上游继续为每个 Service 消耗一个 service CIDR 地址。
**只有当那个池子成为真约束时,才值得重议并接受上面三条负担。**

## 实现备注(给改 kubezoo 的人)

- **读侧挂在单一漏斗** `convertUpstreamObjectToTenantObject`,不是某个 `Backward` 方法。
  get / list / watch / create 与 update 的返回全过它。⚠️ List **单独处理过**:
  informer 的首次 LIST 是最要紧的那条路,漏掉它等于"除了最重要的地方哪儿都对"。
  这条参见任务 #110 的教训 —— 守卫只走了名叫 `Backward` 的方法,漏掉它调用的辅助函数。
- **写侧不能把字段清空**。`validateUpgradeDowngradeClusterIPs`(validation.go:9518)
  明确拒绝 `primary clusterIP can not be unset` ⇒ 必须替换成上游真值 ⇒
  守卫只能挂在**已经持有旧对象**的那几条路径上(create / update / apply 重试)。
