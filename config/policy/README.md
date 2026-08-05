# 租户准入策略(策略层)

这些**不是 kubezoo 的代码**,是**策略层**要执行的约束。放在本仓库是因为
**测试环境必须带上它们** —— 少了它们,lab 里跑的就不是完整形态,
而下面每一条不生效时都是一条实测可用的越权。

归属判据见 `docs/kaaas-platform-architecture-cn.md` §8.0:
准入只有写路径、碰不到响应 ⇒ 需要租户**看到**翻译后视图的事归 kubezoo;
只在写路径且**换个平台会变**的归这里。

## ⭐ 铁律:匹配一律反向写

**`exclude` 平台自己的 namespace,匹配其余全部。**

禁止用正向 selector 选租户 namespace 的标签 —— 除非那个标签**租户改不动**。
`kubezoo.io/tenant` 恰好是这种(kubezoo 无条件重写,四种摘法实测都失败),
但**排除法更稳**:新增一个租户 namespace 时不依赖任何标签就已经被覆盖。

⛔ **配额组件违反过这条三次,三次形状都不同 —— 所以这不是一次疏忽,是这个规则很难自觉遵守。**

1. 用 `objectSelector` 按 `app` 标签排除自己的 Pod,而标签是租户提交对象的一部分 ——
   超额 Pod 抄上那个标签就放行了。
2. 协调器**收养**了租户伪造的、带 `autoupdate` 标签但无 ownerReference 的 ResourceQuota:
   取名让它排在前面,平台自己那份被当成重复项删掉,而收养来的那份没有 `autoupdate` 标签
   ⇒ 聚合配额停止执行。
3. 协调器按 `createdby` 找配额、准入 webhook 按 `autoupdate` 找配额,而**修复只比对 spec**
   ⇒ 租户摘掉 `autoupdate`、留下 `createdby`,协调器照样找得到、看不出变化、什么都不做,
   而 webhook 什么都选不到。**永久静默失效。**

⭐ 第 3 条的教训不止"别用租户可写的标签",还有:**两个组件用不同的键找同一个对象时,
修复方必须连"识别键"本身一起修**,否则修复方永远看不见执行方已经瞎了。

⚠️ 而且 ResourceQuota 就在租户自己的 namespace 里,那里租户是 `*` on `*`(有意的,
为了覆盖租户 CRD)⇒ 标签、ownerReference、整个对象它都能写。**没有哪个字段是安全锚点**,
所以最终答案不是换个字段,是 kubezoo 在入口直接拒绝租户写平台派生的那份配额。

## 策略清单

| 文件 | 作用 | 不生效时 |
|---|---|---|
| `tenant-platform-classes.yaml` | 删掉租户设的 `runtimeClassName` / `priorityClassName` / `priority` / `ingressClassName` 及废弃的 `ingress.class` 注解 | 租户可跑出沙箱、可拿 `system-cluster-critical` 抢占全集群 |
| `tenant-deny-daemonset.yaml` | 拒绝租户建 DaemonSet | 租户可往平台每个节点投放 Pod |
| `tenant-pod-security.yaml` | Pod 强制 `restricted`;并把 namespace 的 PSA 标签钉回 `restricted` | 租户拿到 privileged + hostNetwork 的 Pod。⛔ **原生 PSA 顶不上**:它的判定输入是 namespace 标签,而那个标签租户自己能写 |
| `tenant-scheduling.yaml` | 拒 `spec.nodeName` | 租户可绕过调度器钉节点 |
| `tenant-placement.yaml` | **整体替换**租户的 `nodeSelector`/`tolerations`/`affinity`/`topologySpreadConstraints`/`schedulerName`,换成该租户池子的 | 租户可自选落点。⭐ 注入的 `nodeSelector` 还是 `pods/binding` 那条路上的唯一拦阻(kubelet 不检 NoSchedule 污点但检它)—— **前提是池子标签每租户专属**,已用负向对照证实 |
| `tenant-own-namespace-name.yaml` | 租户只能用**自己视角**的 namespace 名 | 见文件顶部 |
| `tenant-api-endpoint.yaml` | 把 `KUBERNETES_SERVICE_HOST` 注进租户 Pod,指向 kubezoo | 租户负载直连上游 apiserver,绕过整个翻译层 |
| `tenant-ingress-hostnames.yaml` | 主机名只能落在租户自己的子域 —— ⭐**三张表**:`spec.rules[].host`、`spec.tls[].hosts`、以及**不在 spec 里**的 `server-alias` 注解 | 任何租户可声明任何主机名,由 ingress 控制器按创建顺序裁决,**落败方零报错** |
| `tenant-ingress-annotations.yaml` | 拒绝 ingress 控制器**上游自己标为 Critical/High** 的注解(`server-alias` 除外,归上一条管) | 租户能往**与所有租户共用的 nginx.conf** 注入原始配置,并让**平台的**控制器向任意地址发起连接 |

| `tenant-deny-binding.yaml` | 租户不得直接 bind Pod 到节点(**原生 VAP,不是 Kyverno**) | 租户可把 Pod 绑到别的租户节点(API 层成功,靠 kubelet 遏制) |

| `tenant-frozen-deny-writes.yaml` | 冻结的租户,其 ServiceAccount 也写不动(**原生 VAP**) | 冻结只冻住租户的 kubectl;它预置的 Pod 拿 SA token 直连上游照常读写 |

⚠️ **`tenant-frozen-deny-writes.yaml` 的表达式是"放行不属于本租户的身份",不是"拒绝租户"** ——
写成无条件拒绝会把 controller-manager 一起拦掉,症状是**租户的 Deployment 永远不出 Pod**,
且不会有任何报错指向策略。改它之前先读审计 §T 的复测表。

⚠️ **`tenant-deny-binding.yaml` 用原生 VAP 是实测逼出来的**:Kyverno 3.8.2 的
`kinds: [Pod/binding]` 子资源匹配**不生效**(策略 Ready、webhook 注册了、日志里连请求都没有)。
详见审计 §R⑤。
⛔ 打散别用 required podAntiAffinity(扫全量 Pod,调度吞吐杀手);跨租户共驻只能靠节点池。

## ⚠️⚠️ 两个会让策略"Ready 但什么都不做"的坑(都实测踩过)

### 1. `patchStrategicMerge` 里的 `null` 会被 apiserver 剪掉

Kyverno 文档里删字段的写法是置 `null`。但存进 CRD 字段时 **`null` 被剪掉了**,
实测存下来的是:

```json
{"patchStrategicMerge":{"spec":{}}}
```

策略 `READY=True`、`rulecount.mutate=2`,**而它什么都不做**。
⇒ 所以这里用 **`patchesJson6902`**(`op: remove` 能真删)。

### 2. 用了 JSON6902 就没有 autogen,pod controller 必须自己列全

Kyverno 的 `autogen` **只从 `patchStrategicMerge` 派生**。用 JSON6902 时
`.status.autogen` 是空的 —— 实测现象是:**Deployment 的模板里 `runtimeClassName: kata`
原样留着**,只有它生出来的 Pod 过准入时才被清掉。
"跑起来的东西"是对的,但**存下来的对象在撒谎**,而且这依赖"Pod 也会过一遍准入"这个间接性质。

⇒ 所以本目录里 pod controller 的规则是**显式写全的**:
7 个控制器走 `/spec/template/spec`,`CronJob` 多一层单列,`PodTemplate` 再单列。

### 3. `op: remove` 打在不存在的路径上会怎样

⚠️ 这条最危险:`failurePolicy: Fail` + patch 失败 = **所有 Pod 都建不了**。
实测**不会** —— 一个字段都没设的普通 Pod 照常创建。但改这些策略后**必须重测这一条**。

## ⚠️ 写这些策略时踩过的坑

1. **`runtimeClassName` / `priorityClassName` 在 `PodSpec` 里,PodSpec 嵌在 9 个 kind 里**。
   Kyverno 的 `autogen` 会从一条 Pod 规则自动派生出 pod controller 的规则,
   但它覆盖的是 **7 个控制器 + Pod = 8 个,不含 `PodTemplate`**
   (`pkg/autogen/v1/autogen.go`)。只写 Pod 而不靠 autogen,会漏掉 **Deployment 这条最常见的路径**
2. **`spec.priority` 要跟 `priorityClassName` 一起清** —— 只清名字会留下直接写进去的数值
3. **废弃的 `kubernetes.io/ingress.class` 注解要跟 `ingressClassName` 一起删** ——
   多数 ingress 控制器仍认它,只清字段等于没清

### 4. 空更新不触发准入(验策略时最容易骗到自己)

`kubectl label --overwrite` 把标签设成**和当前一样的值**,kubectl 发出的 patch 是空的,
apiserver 直接短路,**mutate/validate webhook 根本不会被调用**。看起来就像策略没生效。
验证时务必改成一个**不同的值**。

### 5. 注入型策略与验证型策略会打架(实测踩过)

准入链是**所有 mutating 跑完再跑 validating**。`tenant-placement` 注入了池子 toleration,
而当时 `tenant-scheduling` 还有一条白名单 validate —— 它**把平台自己注入的结果拒掉了**。
**通用规则:上线注入型策略时,必须同时清掉针对同一字段的验证型策略。**

### 6. 多条策略并存时,别把拒绝归错策略

一个对象可能同时违反好几条规则,谁先拒的不一定是你在测的那条。实测踩过:测调度策略时
Deployment 被拒,以为规则生效了,实际是 `kubectl create deploy` 的默认模板不满足
`restricted`,拒它的是 pod security 那条。
**判据是拒绝消息里的 `策略名: 规则名`**,并且要把被测对象改成**只剩下被测的那一处违规**。

### 6. ⛔⛔ VAP 的 `namespaceSelector` 不限定范围,`scope: Namespaced` 才限定

`matchResources.namespaceSelector` 对**集群级资源根本不过滤** ——
k8s 的语义是"cluster-scoped 资源永不跳过策略"。少了 `scope: Namespaced`,
一条本意只管租户 namespace 的规则会套到**全集群每一次集群级写入**上。

**实测后果**:`tenant-frozen-deny-writes` 曾因此让 **Kyverno 注册不了自己的 webhook**,
三条策略永远不就绪,`pods` 的 validate webhook 根本没注册,
租户的 `hostNetwork`/`hostPID`/`nodeName` 全部放行 ——
而外在症状只是 `kubectl get clusterpolicy` 显示 `READY=<none>`,看着像还在同步。

⚠️ 并且:`failurePolicy: Fail` 下 **CEL 表达式出错 == 拒绝**。
`request.namespace` 在集群级请求里是空串,`split('-')[0]` 越界 ⇒ 一个越界就是全集群故障。
表达式里凡是索引都要有兜底。

### 7. 改完策略跑 `hack/lab/verify.sh`

每条断言都**提交一个必须被拒的东西再看它被谁拒**,并且都验证过摘掉策略会红。
⚠️ 这里原本写着"21 条断言" —— 那个数字早就不对了(现在两百多条),而**写死的计数会
悄悄变成假话**:它只在有人去数的时候才被发现,而没人会去数。要看规模就跑一次。
`READY=True` 不是证据 —— 本项目在这上面栽过四次。

⛔ **跑之前必须带 `KUBEZOO_CONTRACT_DIR`,否则你改的策略根本不上场。**

```bash
KUBEZOO_CONTRACT_DIR=/root/kubezoo-contract hack/lab/up.sh && hack/lab/verify.sh
```

lab **默认装 go.mod 钉住的那个 contract 版本的策略**,不是隔壁工作区的 —— 那是有意的
(策略和 Go 代码是同一套规则的两种表达,只有来自同一个 release 才会一致)。

⚠️ 副作用是:**不带这个变量时,新规则不生效,而断言失败看起来完全像"CEL 写错了"**。
踩过一次:新加的 `spec.tls` 校验断言报"was accepted",查了半天才发现跑的是旧策略。

⭐ 反过来也要做一次:**改动发布成 tag 之后,不带 override 再跑一遍**。带 override 的绿
只证明"我改的对",不证明"发出去的对"。

## ⚠️ 部署注意

### 装上策略之后,必须做一次存量修正 + 存量清理

策略只在准入时生效,对**已经存在**的对象一律不追溯:

```bash
# 存量 namespace 的 PSA 标签(策略装上之前建的不会被自动钉回)
kubectl label ns -l kubezoo.io/tenant pod-security.kubernetes.io/enforce=restricted --overwrite
# 存量违规 Pod —— 实测:补完 namespace 标签后,已在跑的 privileged Pod 仍然 Running,
# 只发了一条 warning。不主动删就等于没封。
```


- `failurePolicy` 用 `Fail`,并配多副本 + PDB。`Ignore` 的失效是**静默的**,直接击穿隔离前提
- ⛔ **`forceFailurePolicyIgnore` 环境变量能一次性把所有策略变成 `Ignore`**
  (`pkg/toggle/toggle.go`)。必须锁死并纳入巡检,否则配置成 `Fail` 只是纸面上的
- 能用 CEL 表达的,也可以考虑 `MutatingAdmissionPolicy`(1.36 已 GA 且默认开,
  跑在 apiserver 进程内、无单点),代价是**没有 autogen**,那 8~9 个 kind 要逐个手写路径
