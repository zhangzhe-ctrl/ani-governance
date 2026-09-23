# Atlas 结构迁移工具镜像。
# 只在一次性迁移 Job 中使用，不承载服务流量，因此保留 busybox 的 /bin/sh 便于排查；
# 应用镜像（distroless）不包含 Atlas，也不包含 shell。
# atlas 二进制不在仓库内，由 scripts/deploy/atlas/build-image.sh 复制进构建上下文。
FROM busybox:1.37

COPY atlas /bin/atlas
COPY migrations /app/migrations/

WORKDIR /app

ENTRYPOINT ["/bin/atlas"]
