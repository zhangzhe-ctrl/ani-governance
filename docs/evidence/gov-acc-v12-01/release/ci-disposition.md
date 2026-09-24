# CI 收尾决定

2026-09-24，用户明确指示：“ci没过就算了，直接走后面流程吧”。据此不再等待或要求 Governance CI 成功，继续最终备份、任务资源清理与证据发布；这是本轮交付条件的明确调整，不改变软件行为验收结果。

已观察事实：

- Accelerator `1d32dd9a9173b8869fa0ef2ae64e20b88f2ca0a3` 的 [真实 Actions](https://github.com/zhangzhe-ctrl/ani-accelerator-service/actions/runs/35949990504) 已成功。
- Governance 实现 `bd9ad1a33bbe8ce28f4faeb19bfc9ee494ae8a84` 的 [真实 Actions](https://github.com/zhangzhe-ctrl/ani-governance/actions/runs/35952056513) 已启动；2026-09-24T03:40:16Z 实际公开页面仍是 `In progress`，精确提交链接一致。尚无成功或失败结论，不把“用户接受 CI 未通过”写成“CI 实际失败”。
- GitHub 匿名 API 后续受共享 IP 限流；公开 Actions 页面仍可查询。这不是源码门禁失败，也不是把 Fedora PASS 改称 GitHub PASS 的依据。

SHIP-03 保留 `not_verified` 并按用户指示不再阻塞交付。Fedora 完整 make verify-gpu、最终模块 normal/race、A/B/C 及82项软件验收仍保留各自真实证据。最终纯证据提交可能触发新的 CI；收尾不等待它，实际状态可从任务分支 Actions 查看。
