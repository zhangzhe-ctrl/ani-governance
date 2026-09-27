# GPU 软件合同实验

`run.sh` 只运行显式提供的跨仓软件合同目录，使用真实 PostgreSQL、mTLS 与测试 owner；不证明 GPU 硬件或生产部署。运行前准备独占数据库、证书、Accelerator 端点和 `ready.json`，并核对所有输入归本 run。脚本不创建或清理别人的容器和数据库。

必填环境：`GOV_ACC_JOINT_DIR`、`GOV_ACC_EVIDENCE_DIR`、私有 `GOMODCACHE`、`GOCACHE`，以及 `GOV_ACC_GOV_ADDR`、`GOV_ACC_GOV2_ADDR`、`GOV_ACC_RELEASE_ADDR`、`GOV_ACC_RELEASE2_ADDR`、`GOV_ACC_OWNER_ADDR`。`run.sh --help` 可只读查看入口。`prepare.py --help` 说明目录参数。测试失败保留日志、退出码与完整源码 SHA；`GOV_ACC_JOINT_DIR` 的凭据文件不入 Git。

已退役的模拟器、固定旧容器/PID 故障脚本可从 Git 提交 `63849fc4cde38b879184a8fea4a6f539e60063e1` 找回作历史阅读，不能在当前资源上直接执行。
