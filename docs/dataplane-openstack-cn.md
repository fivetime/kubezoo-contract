# 数据面契约:OpenStack 租户档案与网络

**一句话**:kubezoo 为每个租户发布一份「档案」Secret(Keystone application
credential + 网络事实),数据面组件(kubezun、kubetron、以及未来任何实现)
**只**从档案取凭据与网络,并履行本文的核对义务;pod 级的端口交接走
`dataplane.kubezoo.io/*` 注解,与具体数据面无关。

已实现方:无(本文先行)。
待接入方:kubezoo-controller(写者)、kubezun、kubetron(读者)。

## 两条边界:谁定什么

- **风格与命名由 kubezoo 定,数据面遵守。** 与 `dataplane-cluster-ip-cn.md`
  的既有原则同源:kubezoo 不该知道这套部署的数据面是谁。两个下游各有各的
  风格(kubezun 用 `knaas.io/*` 注解,kubetron 用 `kubetron.network.kubevirt.io/*`),
  上游为每家适配一次是不可维护的;契约面收敛到 kubezoo 的域,下游改常量。
- **可行性由 OpenStack API 定,谁也改不了。** 契约里每一个键都必须对得上
  gophercloud 的某个 CreateOpts 字段或某个真实消费点;凡是底层 API 不收的
  参数,契约不许发明(见「非目标」——`subnet-v4-id`/`subnet-v6-id` 就是
  这样被否掉的)。

## 命名规则(三层,写死)

| 域 | 谁可见/可写 | 用途 |
|---|---|---|
| `kubezoo.io/<key>`(裸域) | 租户**不可见不可写**(网关 HiddenMetadata 双向执行) | 平台↔平台的事实:归属标签、档案注解、cluster-ip |
| `dataplane.kubezoo.io/<key>` | 租户**可见可写**,网关透传 | 租户↔数据面的契约面:端口交接注解 |
| `OS_*`(Secret data 键) | 平台侧对象,不经租户视角 | OpenStack 自己的概念用 OpenStack 的标准名,**永不发明同义词** |

⛔ **子域可见性是承重行为,必须钉住**:网关隐藏元数据的模式是锚定正则
`^kubezoo\.io/`(kubezoo-gateway `pkg/convert/hiddenmeta.go`),
`dataplane.kubezoo.io/...` **不**匹配它,所以租户看得见、写得进——这不是巧合,
是本契约依赖的行为。谁把模式改宽(比如去掉 `^` 或写成 `kubezoo\.io/`),
第二层契约面整层静默死亡且零报错。网关侧必须有守卫测试断言
`dataplane.kubezoo.io/x` 不被剥。

## 归属键(已有事实,升格为条款)

namespace 标签 `kubezoo.io/tenant=<tid>` 是**唯一合法的租户归属依据**。

- kubezoo 承诺:网关创建 namespace 时写入、拒绝任何变更、对租户不可见;
- 数据面承诺:只用它归属;标签缺失 ⇒ **拒绝服务该 namespace,不得回退**
  到任何默认租户(kubezun resolver 现行语义:"no fallback",正确,保持)。

## 租户档案 Secret

放在**不带租户前缀的平台 namespace**(本契约定为 `kubezoo-tenants`)。
租户经网关只能到达 `<tid>-*`,无前缀 namespace 结构性不可达——这层安全性
是免费的,但反过来:⛔ **档案永远不进 `<tid>-kube-system`**,那是租户可见的
(计费可见是特性,凭据可见是事故)。

```yaml
apiVersion: v1
kind: Secret
metadata:
  namespace: kubezoo-tenants
  name: <tid>-kubezun-<region>   # 作用域 = (租户, 组件, region),见下节
  labels:
    kubezoo.io/tenant: "<tid>"
    kubezoo.io/component: kubezun
    kubezoo.io/region: "<region>"
  annotations:
    kubezoo.io/contract-version: "1"
    kubezoo.io/project-id: "<keystone project uuid>"   # 声明,写者预填
    kubezoo.io/region: "<region>"                      # 声明,消费者实连核对
stringData:
  # —— 凭据段:键名 = OpenStack 标准 env 名,逐字照抄 ——
  OS_AUTH_URL: "https://keystone.example:5000/v3"
  OS_REGION_NAME: "RegionOne"
  OS_APPLICATION_CREDENTIAL_ID: "..."
  OS_APPLICATION_CREDENTIAL_SECRET: "..."
  # (按名引用的变体:OS_APPLICATION_CREDENTIAL_NAME + OS_USERNAME +
  #  OS_USER_DOMAIN_NAME。按 ID 引用是首选,少两个键少一类错配。)
  # —— 网络段:仅计算数据面(kubezun)消费;kubetron 份的网络段留空 ——
  network-id: "<capsule 无端口交接时的缺省落点网络>"
  vip-network-id: "<LB VIP 端口所在网络>"
  vip-subnet-id: "<LB VIP 地址子网>"
```

每个网络键的存在理由(可行性背书):

| 键 | 消费点 | 为什么必须显式 |
|---|---|---|
| `network-id` | Zun capsule `nets=[{network:…}]` | 不指定时 Zun 取它找到的第一张网络——租户有第二张网络起就是掷硬币 |
| `vip-subnet-id` | Octavia `CreateOpts.VipSubnetID` | VIP 子网**不是** pod 子网:VIP 落在 pod 子网会让东西向流量带错目的 MAC |
| `vip-network-id` | 预创建 VIP 端口 | 建 VIP 地址端口需要网络 ID(gophercloud `VipNetworkID` 同样真实存在) |

### Region 分片:档案的作用域是 (租户, 组件, region)

数据面编排受 OVN 规模上限约束,按 Keystone Region 分片:**每个 region 是
一套独立的 Neutron/OVN 控制面,network/subnet UUID 只在本 region 内有
意义**。所以网络键不可能是租户级属性,region 必须进档案的身份:

- **每份档案区域内自包含**:`OS_REGION_NAME` 与档案身份中的 region 一致,
  网络三键都是该 region 内的 UUID。消费者(分片进程本就按 region 落)
  只取**自己 region** 的档案,⛔ 禁止跨档案借任何值——"region A 的凭据 +
  region B 的网络 ID"这种拼装没有一个 OpenStack API 会接受,但只有契约
  禁止它,错误才发生在启动时而不是第一次建 capsule 时。
- **存在性语义按 region 生效**:`<tid>-kubezun-<regionX>` 存在 = 该租户
  在 regionX 就绪;不存在 = 该 region 不服务该租户。区域灰度、按域开通、
  region 退役都用同一个开关表达,不另设状态字段。
- **凭据**:Keystone 为各 region 共享,project 与 application credential
  本身不分区——跨 region 的同租户同组件档案**允许**装同一份凭据值,也
  允许每 region 独立发一份(吊销粒度更细,推荐)。无论哪种,消费者一律
  当作独立档案对待,核对义务(project/region 实连核对)逐份执行。
- ⭐ **两套 region 命名必须钉成同一套**:节点标签
  `topology.kubernetes.io/region` 的值 == Keystone region 名 == 档案身份
  中的 `<region>`。这三处一旦漂移(比如节点侧写 `cn-east` 而 Keystone 叫
  `RegionEast`),分片进程会找不到自己的档案或找错——契约把"三处同值"
  定为条款,一致性验收里加对照。

### 写者与原子性

- **唯一写者**:kubezoo-controller(或其驱动的 onboarding 流程)。
- **顺序**:OpenStack 侧全部就绪(project、application credential、网络、
  VIP 子网)之后,**最后一步一次性写 Secret**。
- **Secret 存在即就绪**。没有 ready 注解、没有两段式——部分写入的档案比
  缺失更糟。
- `kubezoo.io/project-id` / `kubezoo.io/region` 注解**必须预填**:写者知道
  意图中的 project。这把消费侧的核对从 TOFU(首连记录,之后核对——首连
  窗口内错值会被记成真值)扳成纯声明式核对,窗口消失。

### 消费者义务(接入即承诺,逐条可测)

1. **只用 application credential。** 数据面进程内禁止出现 admin 凭据:
   Zun 的 admin 上下文强制 `all_projects=True`,一个 bug 就是跨租户事故。
   (每组件独立一份 app-cred:吊销与爆炸半径按组件切分。)
2. **实连核对声明。** 拿凭据建会话后,核对会话实际认证到的
   project/region 与档案注解一致;不符 ⇒ **拒绝整租户并告警**,不是降级。
   错串 project 的后果不可逆:旧 project 里的 capsule 从新凭据下不可见,
   会被判为丢失而全量重建,同时旧 capsule 持续运行、持续计费、无人能回收。
   改绑定是一次迁移,不是一次编辑。
3. **凭据与 auth-url 只能源自档案。** 来自租户对象(注解、标签、spec 字段、
   ConfigMap)的任何"覆盖凭据/覆盖端点"一律无效。这是对
   「租户注解覆盖 OpenStack 凭据 → 平台控制器 SSRF」一类攻击的封条。
4. **轮换必须可拾取。** 会话/凭据缓存必须有失效机制(TTL 或 watch),
   上限 5 分钟。平台轮换流程:造新 app-cred → 更新 Secret → 宽限期后删旧。
5. **无档案 = 拒绝服务。** 找不到档案的租户不服务、不回退、错误信息里
   说清是边界不是故障。

## 网络:三层

### 第一层 · 租户缺省网络(档案,见上)

无端口交接的 pod 落 `network-id`;LB 的 VIP 落 `vip-*` 两键。

### 第二层 · 端口交接(pod 注解,数据面无关)

端口权威(kubetron 或任何实现)预创建/托管 Neutron 端口,把交接结果
写在 **pod 注解**上;capsule 平面(kubezun)**只读 pod**,不读任何一家的 CRD。
⚠️ 端口权威同时服务它自己平面的 full pod(见「并存条款」),两条路径在
port-created 之前完全同构,之后按持有 pod 的平面分叉:

```yaml
# 租户(或网络数据面的 webhook)写:
metadata:
  annotations:
    dataplane.kubezoo.io/port-claims: "<claim 名,同 namespace>"
# 网络数据面解析后回填:
    dataplane.kubezoo.io/port-id: "<neutron port uuid>"
```

| 条款 | 内容 |
|---|---|
| 引用 | `port-claims` 由租户或数据面 webhook 写;值的解析(claim 对象长什么样、哪个 CRD)是网络数据面内部事务 |
| 回填 | 网络数据面负责把就绪端口的 UUID 写进 `port-id`;端口未就绪则不写 |
| 消费 | 计算数据面:`port-id` 存在 ⇒ capsule 用 `nets=[{port:…}]`,**优先于** `network-id`;`port-claims` 存在而 `port-id` 未回填 ⇒ **等待**(pod 不建、事件说明在等端口),不回退缺省网络 |
| 生命周期 | 端口归网络数据面/claim 所有,capsule 只借用,**capsule 删除永不删端口**(IP 存续正是端口交接的目的) |
| 完整性 | 挂端口前,计算数据面核对端口的 project == 档案 `kubezoo.io/project-id`,不符拒绝 |

⚠️ **`port-id` 是租户可写域,这是特性不是漏洞**:租户在自己 project 里
伪造 `port-id` 等价于"自带端口"(合法能力,Neutron RBAC 管辖);跨 project
伪造被上一条完整性核对拦下。不需要网关保护这个键。

⛔ **这两个键永远不迁入裸域 `kubezoo.io/`**:它们恰恰需要租户可写。谁做这个
"规范化",网关会把它们剥掉/丢弃,功能静默死亡且零报错。

### 第三层 · namespace 级(明确不设)

"某 namespace 整体走专属网络"由网络数据面用自己的注入机制实现
(webhook 自动给该 namespace 的 pod 注 `port-claims`)。契约不提供第三种
寻址;语义收敛为一句话:**无交接的 pod 落租户缺省网络,有交接的 pod 落
交接的端口。**

## 并存条款:两个计算平面互不打架

kubezun 与 kubetron **不是上下游,是两个平行的计算平面**:kubetron 让租户
pod 以完整 pod 落**真实 worker 节点**(OVN 端口经 NAD/ovs-cni 进 pod,
chassis 绑定);kubezun 让租户 pod 以 capsule 落 **Zun 计算节点**(虚拟节点,
Zun attach 完成绑定)。kubetron 额外兼任两个平面共用的端口权威。
并存的每个接触面都要有互斥规则:

### 1. Pod 分区键 = 节点,互斥由 k8s 自己保证

一个 pod 绑定且只绑定一个节点(k8s 单绑定不变量);**绑定在虚拟节点 ⇒
capsule 平面,绑定在真实节点 ⇒ full-pod 平面**,不存在第三态。落点由
kubezoo 的 placement 注入决定(池标签),所以平面选择归 kubezoo,不归
任何数据面。双方义务:**不得对绑定在对方节点上的 pod 做任何动作**
(建 capsule、绑 chassis、生成 NAD、写 pod status)。预绑定阶段
(webhook 时点 nodeName 未定)以 kubezoo 注入的池标签预判平面。

### 2. 端口权威唯一,绑定行为按平面分叉

claim/端口的生命周期只有 kubetron 一个权威(两个平面共用);分叉点在
port-created 之后:

| 持有 pod 的平面 | port-created 之后 |
|---|---|
| full pod(真实节点) | kubetron 继续:chassis 绑定(`binding:host_id`)+ 生成 NAD |
| capsule(虚拟节点) | kubetron 停手:回填 `dataplane.kubezoo.io/port-id`,**不设 host 绑定**;Zun attach 完成其余 |

claim 的单持有者 CEL 不变量天然防跨平面双绑(一个端口不可能同时被
full pod 和 capsule 持有)。

⚠️ **lab 验证项**:Zun attach 预创建端口时是否改写 `device_owner`/
`device_id`——若改写,kubetron 的所有权标记会被数据面自己的挂载动作抹掉,
其孤儿 GC 随即失去判据。接入前必须实测。

### 3. Service → Octavia 单主

两个平面的 pod 都落在租户的 Neutron 网络上,同一个 LB 池**可以**同时装
两个平面的后端——所以打架点不在成员,在**驱动者**:一个 Service 只能有
一个 LB 驱动者,`kubezoo.io/cluster-ip` 也只有该驱动者可写。

- 默认归属:后端全为 capsule ⇒ kubezun;含 full pod 或
  `type: LoadBalancer` ⇒ kubetron(与既有决定「LoadBalancer 归 kubetron」
  一致);
- 仲裁记录:驱动者把自己写进上游 Service 注解 `kubezoo.io/lb-owner`
  (平台域,租户不可见),另一平面见到非己方 owner 即完全不动该 Service;
- 驱动者义务:成员集必须覆盖 EndpointSlice 全集,**含对方平面的后端**,
  不得只装自己平面的 pod;
- ⚠️ 混合后端(一个 Service 同时选中 capsule 与 full pod)是 lab 验证项,
  v1 允许但未实证。

### 4. OpenStack 资源打 owner 标,GC 只收自己标记的

双方现状各有半套:kubetron 给自建端口打 `device_owner=kubetron` +
per-cluster tag(其注释自己就记录过教训:不带集群标记时,另一个集群的
活端口"look exactly like orphans");kubezun 用租户前缀命名 Octavia 对象
("so a garbage collector can recognise its own")。升格为条款:

- 任何平面创建的 OpenStack 资源必须带**可判别的 owner 标记**
  (tag 或命名前缀,写进各自文档);
- 任何 GC 遍历必须**按自己的标记过滤**:"不带我的标记" = "不是我的",
  **永远不等于**"孤儿";
- ⛔ 已知具体风险:kubetron 的孤儿 LB GC 用 admin 凭据做跨项目 list,
  **会看见 kubezun 建的 LB**。无标记过滤,这就是一台定时误删机。接入
  本契约前该过滤必须先落地。

### 5. 凭据天然隔离(重申)

档案按组件分份(`<tid>-kubezun-<region>` / `<tid>-kubetron-<region>`、各自独立 app-cred)
已经把凭据面切开:吊销一个平面不影响另一个,一个平面的 bug 拿不到
另一个平面的多余权限。

### 6. NetworkPolicy:谁的 pod 谁执行(方言见下节)

k8s 上游模型原样适用:NP 由**提供该 pod 网络的平面**执行,apiserver 只
存储。capsule 平面 → Zun 侧 NP→SG(已实现);full-pod 平面 → claim 端口
的 NP→SG(**决策记录 2026-08-14:kubetron 实现语义翻译**,机制其文档已给:
`port set --security-group`,port 从 `claim.status.portID` 拿;其原先
"SG 划出 scope、租户 Horizon 自服务"的决定收回——该前提在本平台不成立,
租户只有 k8s API)。跨平面 peer 在 v1 结构性不存在(档位按租户二元),
每个执行者只在本平面内解析 selector。

⛔ **Cilium 不是任何租户流量的 NP 执行者**:租户流量在 OVN 接口(net1)上,
Cilium 只覆盖 eth0(kubetron DESIGN-refactor §507-508 自己的记载)。
"B1 走 Cilium 所以不用管"是错的,不许再出现在任何一方的文档里。

## NetworkPolicy 执行契约(平台方言:kubezoo 定义,两平面执行)

两个平面翻译到**同一个 Neutron SG 原语**,所以只有**一种方言**,不存在
按平面分叉的能力清单;租户在两个档位看到完全一致的 NP 行为。

### namespace 的对应关系(先于方言,两家不许各凭直觉)

| 轴 | 关系 |
|---|---|
| 租户 : Keystone project | 1:1(凭据/计费/配额边界) |
| 租户 : 缺省 Neutron 网络 | 1:1(档案 `network-id`) |
| namespace : 网络 | 缺省 N:1(租户所有 ns 共享缺省网络);full-pod 平面可经 claim 分化到租户其他网络(≈VPC) |
| namespace : OpenStack | **无对应物**,唯一的桥 = `kubezoo.io/tenant` 标签 |

推论两条:

- **网络层默认扁平,与 k8s 默认语义自洽**(无 NP 时跨 ns 本就互通)。
  跨 ns 隔离**只能由 SG 规则表达**(ns→pod IP 集合),不得指望网络拓扑;
  执行者的主要翻译工作就是维护 ns→pods→IPs 映射并随 pod 增删刷新 SG。
- ⛔ **selector 解析域 = 本租户的 namespaces,先按 `kubezoo.io/tenant`
  过滤,再匹配租户的 selector**。namespace 标签是租户自定的,跨租户可以
  相同;不先限定租户边界,A 的 `namespaceSelector: {env: prod}` 会匹配到
  B 的同标签 ns,把 B 的 pod IP 写进 A 的 SG 放行规则——即便流量因项目
  隔离结构性不通,把别家 IP 写进任何 SG 规则本身就是越界(信息泄漏,
  且网络拓扑一变即成真窟窿)。

### 方言

- **基线语义 = 上游 k8s NP,四条一条不许走样**:pod 未被任何策略选中 =
  default-allow;被选中即该方向 default-deny;规则只做加法放行(NP 没有
  deny 规则);多条策略取并集。SG 翻译必须保持这四条。
- **不可表达清单(v1)**:`ipBlock.except`、命名端口、SCTP。清单由平台
  维护(网关侧管理员可配置,与 hidden-metadata pattern 同款机制,不写死
  在代码里);**任一平面不再满足清单,即不得服务其档位**。
- **准入是主闸,fail-closed 是纵深**:网关按方言统一拒绝清单内字段
  (档位无关、同一错误信息,说明"省略它的后果是相应流量保持拒绝");
  任何漏过准入的不可表达结构,执行侧一律保持拒绝,**永不 fail-open**。

### 平台基础设施白名单(两平面必须同样实现,并写进租户文档)

Octavia health monitor 探测源、租户 resolver 的 DNS、元数据服务。
否则租户一条 default-deny 就把自己的 LB 打成全员 down
(HM 被 SG 拒 ⇒ member 全摘),且无从自查。

### 一致性验收(契约的牙齿)

kubezoo-contract 提供**黑盒一致性用例**(租户视角 apply NP + 探通断,
不看任何一方的实现):default-deny 生效 / 同 ns selector 放行 /
namespaceSelector 跨 ns 放行 / ipBlock 放行 / 端口限定 /
清单三项在准入被拒且错误点名字段 / **阴性对照:另一租户的同标签
namespace 不被本租户的 namespaceSelector 匹配**(解析域条款的牙齿)。
两平面必须交出**相同的通过矩阵**。

⛔ **平面通过一致性验收之前,其档位的 NP 一律网关拒绝(fail-loud)**——
被接受而无人执行的 NP 是伪装成隔离的裸奔,比报错坏得多。
⚠️ 代价要认:过渡期自带 NP 的 chart 在该档位装不上,这是诚实的价格。

## 轮换与吊销

- **轮换**:见消费者义务 4。
- **吊销(租户完全停机/取证)**:**Keystone 侧 revoke application credential
  是权威开关**——它在数据面失联、进程被攻破、Secret 陈旧时照样生效,这正是
  强制 app-cred 的红利。删 Secret 只是清理,顺序在 revoke 之后。
- **read-only 停机(欠费)**:完全不动数据面,由网关拦写。

## RBAC

- 每租户的计算数据面进程(一租户一进程):SA 以 `resourceNames` 限定
  **只能 Get 自己那份** Secret(消费用 Get,不需要 List/Watch 权限);
- 多租户单进程的组件(kubetron manager):label selector 范围的 List/Watch。

## 非目标(v1 明确不做,含否决理由)

- **子网选择键(`subnet-v4-id`/`subnet-v6-id`)**:Zun capsule 的网络参数
  只有 `{network, port}`,**不存在子网参数**;LB member 的子网从 capsule
  反查获得;kubetron 的按子网选址在 claim/端口粒度自理(Neutron
  `FixedIPs[{SubnetID,…}]`)。三个消费者没有一个会读这两个键——曾被提议,
  按「可行性由 OpenStack API 定」否决。
- **双栈 LB**:gophercloud 已备好 `AdditionalVips`,但单 LB 单 VIP 族是
  现行运作方式。留作 v2 扩展,v1 不占坑。
- **claim CRD 的组名/形状**:运行时契约面已收敛到 pod 注解,CRD 是网络
  数据面的内部实现,契约不管辖(也因此换数据面不动租户清单)。
- **配额/AZ 目录**:另行立约,不塞进档案。

## 迁移注记(现状 → 契约)

| 项目 | 现状 | 改动 |
|---|---|---|
| kubezun | Secret data 已是 `OS_*` 键(兼容,零改);binding 注解用 `knaas.io/project-id`/`knaas.io/region`;`--network` 与 VIP 两参数是进程 flag;不读 `port-id`;Service 无单主判定 | 注解常量改 `kubezoo.io/*`;核对逻辑从 TOFU 改为"缺注解即拒绝";网络三键改读档案;实现 `port-id` 消费(含 project 核对);会话缓存加 TTL;LB 驱动前检查/认领 `kubezoo.io/lb-owner` |
| kubetron | pod 注解用 `kubetron.network.kubevirt.io/port-claims`;凭据走 env/stand-in ConfigMap;孤儿 LB GC 跨项目 list 无对方标记概念 | 注解常量改 `dataplane.kubezoo.io/port-claims`;claim 绑定按持有 pod 平面分叉(capsule 不设 host 绑定,回填 `port-id`);凭据源改档案;补实连核对;⛔ 孤儿 GC 加 owner 标记过滤(否则误删 kubezun 的 LB);LB 单主认领 |
| kubezoo-gateway | HiddenMetadata 已锚定 `^kubezoo\.io/` | 加子域守卫测试(断言 `dataplane.kubezoo.io/x` 不被剥) |
| kubezoo-controller | 无 | 新增档案分发(先校验+摆放,OpenStack 侧资源可仍由脚本造) |

## 相关

- 姊妹契约:`docs/dataplane-cluster-ip-cn.md`(同一命名原则的先例)
- 部署基线:`config/deployment-example/`(⛔ 那里不放凭据,示例只在本文)
