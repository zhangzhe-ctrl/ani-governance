// 跨包（service 层）测试装配：导出免 bootstrap.Context 的 repo 构造器，
// 字段初始化必须与生产 NewXxxRepo 逐字段一致，改生产构造器须同步此处。
//
// 说明：
//   - 本文件覆盖 repo_testkit.go / repo_testkit2.go 之外批次的 repo
//     （task）。构造器与对应 *_repo.go 的生产构造器逐字段对齐（含
//     mapper/converter 初始化与 init() 调用），唯一差异是 log 一律
//     bLogger.NewHelper(bLogger.NopLogger())；entClient 由调用方传入
//     （测试场景为 enttest.NewEntClientForTest 的 SQLite 内存库）。
//   - 生产构造器中经由 *bootstrap.Context 注入的仅是日志助手，无其他隐藏依赖；
//     依赖 redis/minio 等外部件的构造器不在此导出。
package data

import (
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	entCrud "go-wind-admin/pkg/localdeps/go-crud/entgo"

	"github.com/tx7do/go-utils/mapper"

	"go-wind-admin/app/admin/service/internal/data/ent"
	entTask "go-wind-admin/app/admin/service/internal/data/ent/task"

	taskV1 "go-wind-admin/api/gen/go/task/service/v1"
)

// NewTaskRepoForTest 与生产 NewTaskRepo 逐字段一致（log 换 NopLogger），并调用 init()。
func NewTaskRepoForTest(entClient *entCrud.EntClient[*ent.Client]) *TaskRepo {
	repo := &TaskRepo{
		log:           bLogger.NewHelper(bLogger.NopLogger()),
		entClient:     entClient,
		mapper:        mapper.NewCopierMapper[taskV1.Task, ent.Task](),
		typeConverter: mapper.NewEnumTypeConverter[taskV1.Task_Type, entTask.Type](taskV1.Task_Type_name, taskV1.Task_Type_value),
	}

	repo.init()

	return repo
}
