# ANI Governance Redis

Gov 的专用 Redis Chart。为兼容已有部署，默认资源名为 `redis`，默认存储类为
`local-path`；原有部署在 `ani-infra` 时，Gov 默认连接
`redis.ani-infra.svc.cluster.local:6379`。离线示例显式选择 `ani-system`、
资源名 `ani-governance-redis` 和 Ceph `ani-block`，需同时使用 Gov 的离线示例。
密码由同命名空间 `Secret/ani-governance-redis` 的 `password` 键提供。

默认镜像使用原有镜像仓库和固定的 linux/amd64 manifest
`sha256:ca0acbb137c1dc3339c8b147a58fd6f42775d4599327b50e7b116c23de501af2`
（Redis 7.4.11）。离线交付把同一 manifest 导入目标仓库并覆盖 `image.repository`；
交付时须校验目标仓库的摘要，不要仅修改 tag 而保留不匹配的 digest。需要仓库认证时设置
`imagePullSecrets`，对应 Secret 必须在 `ani-system`。

`persistence.storageClass` 默认为 `local-path`；设为空可使用集群默认 StorageClass，
[离线示例](values-offline.example.yaml) 显式使用 installer 的 Ceph `ani-block`，
申请 2Gi RWO PVC。Chart 不创建 StorageClass，也不会安装 Redis 的替代实现。

先创建 `ani-system` 和 Redis 密码 Secret，再安装；密码文件由部署方在仓库外安全生成。
Gov 配置 Secret 中的 Redis 原始密码必须相同，Asynq URI 中则需要 URL 编码。

```bash
# 当前 context 必须由部署方先核对。
kubectl create namespace ani-system --dry-run=client -o yaml | kubectl apply -f -
kubectl -n ani-system create secret generic ani-governance-redis \
  --from-file=password=/absolute/path/outside/repository/redis-password \
  --dry-run=client -o yaml | kubectl apply -f -

# 先复制并编辑示例，填入已导入离线仓库的真实 repository。
helm lint charts/ani-governance-redis --strict -f /absolute/path/redis.values.yaml
helm upgrade --install ani-governance-redis charts/ani-governance-redis \
  --namespace ani-system -f /absolute/path/redis.values.yaml \
  --wait --timeout 10m
```

`service.port` 同时控制 Redis 监听、Service 和探针端口；改端口时同步修改
Gov 的 `redis.address` 与 `server.asynq.uri`。PVC 保留策略由 StatefulSet 和集群控制，
升级或重新安装前先检查现有数据及备份，不删除 PVC 来处理认证或启动错误。
