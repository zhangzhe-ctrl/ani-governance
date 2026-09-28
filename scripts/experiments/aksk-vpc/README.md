# AK/SK 与 VPC 隔离实验

本入口保留 HMAC、VPC 查询和 mTLS 的真实合同检查。它会创建命名空间、数据库和业务 fixture，必须在已授权的独占集群和任务目录执行；本仓 CI 不调用它。运行结果只证明本次指定环境，Network 数据面与生产切换仍需另验。

运行前显式提供 `ANI_AKSK_LAB_RUN`（独占目录）、`ANI_AKSK_LAB_CONTEXT`、`ANI_AKSK_LAB_NAMESPACE`（本 run 独占且尚不存在）、`ANI_AKSK_LAB_NODEPORT`、`ANI_AKSK_LAB_FORWARD_PORT`、所需服务地址及证书。`lab.py --help` 列出阶段；先运行 `prepare`，每阶段核对任务状态再继续。`prepare` 遇到已有命名空间会拒绝接管。原始凭据只放 `${ANI_AKSK_LAB_RUN}/private`，日志和结果放 `evidence`。

离线签名回归：`python3 -m unittest scripts/tests/test_aksk_vpc_client.py`。可复用客户端：`python3 scripts/ops/aksk_vpc_client.py --self-test`。镜像构建使用 `scripts/dev/lab/build-images.sh`，显式指定 `ANI_LAB_RUN`、`ANI_ATLAS_BIN` 和导入目标 `KIND_CLUSTER`。权限登记 SQL 在 `scripts/ops/sql/bootstrap-network-access.sql`。

旧固定 Ubuntu 主机、namespace、NodePort 与历史验收日志位于 Git 提交 `63849fc4cde38b879184a8fea4a6f539e60063e1`；那些参数不作为当前默认值。
