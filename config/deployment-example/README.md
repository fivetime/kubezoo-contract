# 真实部署样例(control1)

这是 **2026-08-08 实际在跑的那一套清单**,从 `10.32.16.2:/root/kubezoo-deploy/` 原样取回,
只删掉了凭据文件。上游是 Kamaji 托管的控制面(`kamaji/kubezoo-upstream`)。

⛔ **不是"推荐配置",也不是模板** —— 是一份**已知能跑起来**的存档。
`config/setup/proxy.yaml`(在 kubezoo-gateway 里)才是基底;这里记的是那份基底
**在一套真集群上必须改成什么样**,以及每一处为什么。

## 为什么进这个仓库

因为在此之前它只存在于**一台机器的磁盘上**,不在任何版本控制里。
那台机器没了,当前生产集群唯一的真相来源就没了。

⚠️ 这些**地址和上游端点是这套部署的真值**,不是占位符。抄之前先改。
用占位符换掉它们正是 kubezoo-gateway `config/setup/controller.yaml` 踩过的坑
(`KUBEZOO_EXTERNAL_ADDRESS_PLACEHOLDER` 从来没人替换,那份清单**按构造就不可用**)。

## ⛔ 这里没有凭据,也不要往里加

原目录还有 `pki/`(CA 私钥、admin 私钥)、`upstream/`(`sa.key`、client 私钥)和四个
带内嵌证书的 kubeconfig。**一个都没进来,而且这个仓库是公开的。**

清单里出现的 `secretName` 只是**名字引用**,Secret 本身要在集群里另行创建。

## 与基底(`kubezoo-gateway/config/setup/proxy.yaml`)的差异

| 改动 | 为什么 |
|---|---|
| 上游 → `https://kubezoo-upstream.kamaji.svc:6443` | 集群内用 svc 名。⚠️ 反过来也成立:拿 svc 名的 kubeconfig 在宿主机上用会报错,且报错长得像证书问题 |
| 镜像 → `ghcr.io/fivetime/*:latest` + `imagePullPolicy: Always` | CI 推 `latest`,杀 pod 即拉新版 |
| ⛔ **kubezoo-etcd 加 PVC** | 基底里 kubezoo-etcd **没有卷**(demo 形态),而 Tenant 对象就存在那里 —— **丢了等于所有租户没了** |
| etcd 镜像 → `quay.io/coreos/etcd:v3.6.12` | 基底的 v3.3.27 已 EOL |
| Service → LoadBalancer + Cilium 注解,钉 `10.224.18.51` | 本集群的 LB-IPAM 约定 |
| **443 端口(443→6443)** | 租户的 `kubernetes` Service 是 headless,不做转发;客户端解析后拨 `https://` 用的就是 443,所以 kubezoo 必须自己听 443 |
| `--service-account-issuer` **两个值** | ⛔ kubezoo 把 TokenRequest **转发给上游**,拿回来的 token 是**上游签的**。只配自己那个 ⇒ 租户 pod 内的 SA token **全部 401**,而 kubezoo 侧毫无异常。上游 issuer 取自 `kubectl get --raw /.well-known/openid-configuration` |
| `--api-audiences` 同时含上游值 | 同上 |
| `--tenant-dns=true` + `--tenant-dns-cluster-domain` | 每租户 CoreDNS。⚠️ 网关和控制器**两边都要开**,且 cluster domain 必须一致 —— 不一致不报错,只是短名字不再解析 |

## 控制器侧(`controller.yaml`)

基底那份 `config/setup/controller.yaml` 带着从没被替换的占位符,所以这里是改写过的版本:
外部地址已填实、上游 kubeconfig 从 Kamaji 的 `-admin-kubeconfig` Secret 取。

`--tenant-dns-image` 钉的是 `registry.k8s.io/coredns/coredns:v1.13.1`,
`--tenant-dns-replicas=2`。

## ⚠️ 这套部署里**没有**的东西

- **配额组件(clusterresourcequota)未部署**。它是 ValidatingWebhook,要注册在**上游**,
  且上游 apiserver 要能回连它的 Service —— 跨控制面,和单集群不是一回事。
  没有它 = 租户级配额完全不生效,**且没有任何异常现象**。
- **上游的 LB 仍对外可达** ⇒ "所有流量经 kubezoo" 目前只是约定、未强制。
  要封死应把上游那个 Service 收成 ClusterIP(kubezoo 走 svc 名不受影响)。

## 相关

- 策略层:`config/policy/`(⚠️ 本部署跑的是 knaas 自己那套 `knaas-tenant-*`,
  和这里交付的 `tenant-*` **不是同一份**,选的标签键也不同)
- 数据面契约:`docs/dataplane-cluster-ip-cn.md`
