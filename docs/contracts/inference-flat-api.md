# Inference 管理接口状态

Governance 当前通过 BFF 提供 Inference 管理接口。普通和 GPU 推理的创建、列表、详情、操作查询、更新、启停/重启及删除均使用 `/api/v1/inference/...`；BFF 对外遵循 `page`/`pageSize`、`items`/`total` 的 HTTP 合同，按租户与权限过滤后分页。Inference Go API 仅作为 Gov 的受信服务间依赖，不改变 Model 目录/版本/导入或 ACC、Resource 的职责。Higress 仍只承载 `/v1/completions` 等推理数据面，不承载这些管理路由。
