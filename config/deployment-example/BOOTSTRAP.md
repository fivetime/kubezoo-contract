# 从零搭一套(control1 形态)

上游用 Kamaji 托管控制面,kubezoo 独立部署在管理集群里。
本文里的每一步都是 **2026-08-08 从在跑的那套里逐条核对出来的**,不是凭记忆写的。

⛔ **这份文档没有被完整跑过一遍。** 它是从一套已经跑起来的系统**反推**出来的,
每条事实单独核对过,但"照着从头走一遍"还没做。真要当交付文档用,
先在干净集群上走一遍并把踩到的坑补回来 —— 读起来齐全和真能跑是两回事,
这个仓库里已经有 `KUBEZOO_EXTERNAL_ADDRESS_PLACEHOLDER` 那样的前车之鉴。

---

## 0. 前置依赖

管理集群上必须先有:

| 依赖 | 本部署用的 | 用来干什么 |
|---|---|---|
| Kamaji operator | ns `kamaji-system` | 起上游控制面 |
| LoadBalancer 方案 | Cilium LB-IPAM,池 `private-pool` = `10.224.18.0/24` | 给上游和 kubezoo 各分一个 VIP |
| 存储类 | `nvme-rep3-rbd-pool` | ⛔ kubezoo-etcd 的 PVC。**没有它 Tenant 对象就没有持久化** |
| `cfssl` / `cfssljson` | | 生成 kubezoo 自己的 PKI |

地址规划(本部署实际值,抄之前改掉):

```
10.224.18.50   上游 Kamaji 控制面的 LB
10.224.18.51   kubezoo 的 LB —— 租户唯一入口
254.51.0.0/16  上游集群的 service CIDR
192.168.0.0/16 kubezoo 自己对租户宣称的 service CIDR
```

---

## 1. 起上游(Kamaji TenantControlPlane)

⚠️ **命名**:叫 `kubezoo-upstream`,不要叫 `tenant-xxx`。它的身份是"kubezoo 的上游控制面",
而 `tenant` 这个词在这套系统里属于 kubezoo 的租户(6 位 ID)。混用会让后面每一句话都有歧义。

```yaml
apiVersion: kamaji.clastix.io/v1alpha1
kind: TenantControlPlane
metadata:
  name: kubezoo-upstream
  namespace: kamaji
spec:
  dataStore: default
  controlPlane:
    deployment:
      replicas: 2
      extraArgs:
        controllerManager:
          - --allocate-node-cidrs=false      # ⚠️ 见下
    service:
      serviceType: LoadBalancer
      additionalMetadata:
        labels:
          io.cilium/ip-pool-private: "true"
          io.cilium/bgp-advertise-external-ip: "true"
        annotations:
          io.cilium/lb-ipam-ips: "10.224.18.50"
  kubernetes:
    version: v1.36.3
    kubelet: {cgroupfs: systemd}
    admissionControllers: [ResourceQuota, LimitRanger]
  networkProfile:
    port: 6443
    certSANs: ["10.224.18.50"]
    podCidr: ""
    serviceCidr: ""
    serviceCidrs: ["254.51.0.0/16"]
```

⚠️ **`--allocate-node-cidrs=false`**:节点是 virtual kubelet,自带网络(OVN),
让 node-ipam 去分 podCIDR 只会失败或分出没人用的段。

等到 `kubectl -n kamaji get tcp kubezoo-upstream` 显示 `Ready`。

---

## 2. 从 Kamaji 的 Secret 里取上游材料

Kamaji 把材料摊在几个 `<tcp名>-` 前缀的 Secret 里。**键名已实测**:

| kubezoo 要的文件 | 从哪个 Secret | 取哪个键 |
|---|---|---|
| `sa.pub` / `sa.key` | `kubezoo-upstream-sa-certificate` | `sa.pub` / `sa.key` |
| `ca.crt` | `kubezoo-upstream-ca` | `ca.crt` |
| `client.crt` / `client-key.crt` | `kubezoo-upstream-admin-kubeconfig` | 从 **`admin.conf`** 里解出 `client-certificate-data` / `client-key-data` |
| 上游 kubeconfig(给控制器) | `kubezoo-upstream-admin-kubeconfig` | **`admin.svc`**(svc 名版本) |

⚠️ **`admin.svc` 不是 `admin.conf`**。`admin.svc` 里的 server 是
`https://kubezoo-upstream.kamaji.svc:6443`,`admin.conf` 里是 LB 地址。
集群内的组件用前者;**在宿主机上用前者会解析失败,而且报错长得像证书问题**。

⚠️ `-ca` 这个 Secret 里同时有 `ca.crt`/`ca.key`/`tls.crt`/`tls.key`,只取 `ca.crt`。
`-front-proxy-ca-certificate` 是另一套,**这里不用它**。

```bash
KC=kubezoo-upstream-admin-kubeconfig
mkdir -p upstream
kubectl -n kamaji get secret kubezoo-upstream-sa-certificate -o jsonpath='{.data.sa\.pub}' | base64 -d > upstream/sa.pub
kubectl -n kamaji get secret kubezoo-upstream-sa-certificate -o jsonpath='{.data.sa\.key}' | base64 -d > upstream/sa.key
kubectl -n kamaji get secret kubezoo-upstream-ca -o jsonpath='{.data.ca\.crt}' | base64 -d > upstream/ca.crt
kubectl -n kamaji get secret $KC -o go-template='{{index .data "admin.conf"}}' | base64 -d > /tmp/admin.conf
grep client-certificate-data /tmp/admin.conf | awk '{print $2}' | base64 -d > upstream/client.crt
grep client-key-data         /tmp/admin.conf | awk '{print $2}' | base64 -d > upstream/client-key.crt
kubectl -n kamaji get secret $KC -o go-template='{{index .data "admin.svc"}}' | base64 -d > upstream.kubeconfig
```

核对一下身份应该是:`subject=O=kubeadm:cluster-admins, CN=kubernetes-admin`
⇒ **kubezoo 以集群管理员身份连上游**。

---

## 3. 生成 kubezoo 自己的 PKI

`kubezoo-gateway/hack/lib/gen_pki.sh` 里有 `gen_ca` / `gen_kubernetes_cert` / `gen_admin_cert`
(cfssl)。⚠️ 同文件里的 `get_upstream_pki_kind()` **假设上游是 kind**,这里用不上,别调它。

产出:`ca.pem` `ca-key.pem` `kubernetes.pem` `kubernetes-key.pem` `admin.pem` `admin-key.pem`。

⚠️ `kubernetes.pem` 的 SAN 必须含 **kubezoo 的 LB 地址(10.224.18.51)和 svc 名**
`kubezoo.kubezoo-system.svc`,否则租户或控制器有一方连不上。

---

## 4. 造 5 个 Secret

清单只按名字引用它们,**内容得自己造**。键名已实测:

```bash
N=kubezoo-system
kubectl create ns $N

# ① 网关的服务端证书
kubectl -n $N create secret generic kubezoo-pki \
  --from-file=ca.pem --from-file=kubernetes.pem --from-file=kubernetes-key.pem

# ② 上游材料(第 2 步取的)
kubectl -n $N create secret generic upstream-pki \
  --from-file=upstream/ca.crt --from-file=upstream/client.crt \
  --from-file=upstream/client-key.crt --from-file=upstream/sa.pub --from-file=upstream/sa.key

# ③ 控制器签租户证书要的 CA ⛔ 含私钥
kubectl -n $N create secret generic kubezoo-ca \
  --from-file=ca.pem --from-file=ca-key.pem

# ④ 控制器连 kubezoo 自己(用 admin.pem 生成的 kubeconfig,server 指 svc 名)
kubectl -n $N create secret generic kubezoo-controller-kubeconfig \
  --from-file=kubezoo.kubeconfig

# ⑤ 控制器连上游(第 2 步的 admin.svc)
kubectl -n $N create secret generic kubezoo-upstream-kubeconfig \
  --from-file=upstream.kubeconfig
```

两个 kubeconfig 的 server 必须是:

```
kubezoo.kubeconfig   → https://kubezoo.kubezoo-system.svc:6443
upstream.kubeconfig  → https://kubezoo-upstream.kamaji.svc:6443
```

⭐ **`kubezoo-ca` 里有 CA 私钥,而且必须有** —— 控制器用它给每个租户签客户端证书
(`contract/pkg/util/certs.go:82 NewTenantCertAndKey`)。这是这套部署里最敏感的一份材料:
拿到它等于能签出任意租户的身份。

---

## 5. apply 清单

```bash
kubectl -n kubezoo-system apply -f audit-policy.yaml   # 先,网关挂载它
kubectl -n kubezoo-system apply -f kubezoo.yaml
kubectl -n kubezoo-system apply -f controller.yaml
```

⛔ **`-n kubezoo-system` 不能省。** 这几份清单里没写 namespace,漏了会在 `default` 里
建出一整套重复的 —— 而且 ClusterRole/ClusterRoleBinding 是**集群级共享的**,
之后清理那份重复的会把真正在用的一并删掉(实际发生过)。

各处参数为什么要那样改,见同目录 `README.md`。⚠️ 尤其
**`--service-account-issuer` 必须写两个值**,少了上游那个,租户 pod 里的 SA token 全部 401
而 kubezoo 侧毫无异常。

---

## 6. 建第一个租户

```bash
cat <<EOF | kubectl --kubeconfig=kubezoo-admin.kubeconfig apply -f -
apiVersion: tenant.kubezoo.io/v1alpha1
kind: Tenant
metadata:
  name: "111111"          # ⚠️ 必须 6 位;前缀长度是整套翻译的前提
spec:
  id: 1
EOF
```

`kubezoo-admin.kubeconfig` 用第 3 步的 `admin.pem`/`admin-key.pem` 生成,server 指
`https://10.224.18.51:6443`。

控制器随后会:建 `111111-{default,kube-system,kube-public,kube-node-lease}` 四个命名空间、
用 `kubezoo-ca` 给租户签客户端证书、生成 kubeconfig。

⛔ **kubeconfig 有取用期限。** 它被 base64 塞进 Tenant 的注解
`kubezoo.io/tenant.kubeconfig.base64`,平台在 **`--credential-retention`(默认 24h)**
之后把它删掉 —— credential-retention 的设计意图是平台不长期持有租户的私钥。
实测:建完 **6 秒**注解就出现了;而一个建了两天的租户上,这个注解已经是**空的**。

```bash
kubectl --kubeconfig=kubezoo-admin.kubeconfig get tenant 111111 \
  -o jsonpath='{.metadata.annotations.kubezoo\.io/tenant\.kubeconfig\.base64}' \
  | base64 -d > tenant-111111.kubeconfig
test -s tenant-111111.kubeconfig || echo "空 —— 要么还没签好(等几秒),要么已过保留期"
```

⚠️ **过期了不能靠重启控制器补救**:重签的判据是 `credential-issued-at` 这个注解
(`controller.go:1134`),它一存在就永不重签 —— 撤走凭据时**不会**顺手删掉它。
要重新签必须**手工删掉那个注解**(控制器撤走时的日志里也这么写):

```bash
kubectl --kubeconfig=kubezoo-admin.kubeconfig annotate tenant 111111 \
  kubezoo.io/tenant.credential-issued-at-
```

⚠️ 还有个反直觉的分支(`controller.go:1139`):注解里**有 kubeconfig 但没有 issued-at**
时,控制器会**认领**而不是重签 —— 那是为了让升级不至于给每个既有租户凭空多签一份。

## 7. 验证

以下每条都在 control1 上真跑过(用一个临时租户 `999999`,验完已删除)。

```bash
T=tenant-999999.kubeconfig
kubectl --kubeconfig=$T get ns
#   default / kube-node-lease / kube-public / kube-system   ← 无前缀

kubectl --kubeconfig=$T get nodes
#   No resources found   ← 见下
```

对照上游,确认翻译真的发生了:

```bash
kubectl --kubeconfig=upstream.kubeconfig get ns | grep 999999
#   999999-default / 999999-kube-node-lease / 999999-kube-public / 999999-kube-system
```

⚠️ **`get nodes` 返回 `No resources found` 是对的**,不是权限问题:一个还没有工作节点的
租户就是没有节点可看。⛔ 别把这行输出当成一个名叫 `No` 的节点 —— 用 `wc -l` 数它会得到 1,
这个仓库的历史上因此差点误报过一次租户间泄漏。

删除也验过会收敛:

```bash
kubectl --kubeconfig=kubezoo-admin.kubeconfig delete tenant 999999
# 15s 内:上游的 4 个命名空间、ClusterRole、ClusterRoleBinding 全部清零
# ⚠️ 刚删完的十几秒里 namespace 处于 Terminating,那时去数会看到残留,不是泄漏
```

### ⛔ 控制面健康 ≠ 能用

这套系统上反复出现的失败形态是"控制面全绿、断的是另一条只有真实工作负载才走的路"。
上面那些**全过了也不能算装好**,至少还要验:

- **租户 pod 里的 SA token 能访问 kubezoo**。⚠️ 必须用 **token**,不能用 kubeconfig ——
  `--service-account-issuer` 那个坑只在 token 路径上暴露,kubeconfig 一切正常。
- **一个真实的 Deployment 跑起来并被调度**。pod 由**上游的 controller-manager** 创建,
  根本不经过 kubezoo,是完全独立的一条路径。

## ⚠️ 这份文档还缺的

- **策略层**:`config/policy/` 下的准入策略要单独装(`hack/lab/policies.sh`),
  少了它们每一条都是一条实测可用的越权
- **配额组件**(clusterresourcequota)本部署没装,装法未验证
- **数据面**(kubezun / OpenStack:Zun、Octavia、Neutron、OVN)完全是另一条线,
  但没有它租户跑不了任何东西
- **`kubezoo-etcd` 的备份** —— Tenant 对象全在那里,而本文没有任何备份步骤
