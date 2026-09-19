// InternalMessageService 的 SQLite 内存库集成测试（白盒，包内测试）。
//
// 覆盖目标：
//   - ListMessage / GetMessage 的 enrichment：CategoryName 经
//     internalMessageCategoryRepo.ListCategoriesByIds 从分类表回填；
//     未挂分类的消息不回填。
//   - RegisterInternalMessagePublisher / RegisterTaskEnqueuer 注册缝：
//     默认值（noop publisher / nil enqueuer）被注册实例替换。
//
// 跳过项：SendMessage/广播 fan-out（涉 SSE 推送与 asynq 投递，只验证注册缝本身）、
// HandleAuthorize（依赖 authenticator，构造时置 nil）。
package service

import (
	"context"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/trans"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"github.com/tx7do/kratos-transport/transport/sse"

	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/app/admin/service/internal/data/enttest"

	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	internalMessageV1 "go-wind-admin/api/gen/go/internal_message/service/v1"
)

// internalMessageServiceUserRepoStub：executeBroadcast 分页拉取用户时才会用到
// data.UserRepo，本批次测试路径不触发，仅以空桩占位（嵌入接口获得默认方法集，
// 未覆写方法被调用即 panic，测试即失败）。
type internalMessageServiceUserRepoStub struct {
	data.UserRepo
}

// newInternalMessageServiceForTest 白盒复刻 NewInternalMessageService 的字段初始化：
// log 换 NopLogger，repo 用 testkit 构造器，authenticator 置 nil（HandleAuthorize 专用），
// 默认 publisher 为 noop、taskEnqueuer 为 nil（与生产构造器一致，供注册缝测试断言）。
func newInternalMessageServiceForTest(t *testing.T) *InternalMessageService {
	t.Helper()
	entClient := enttest.NewEntClientForTest(t)
	return &InternalMessageService{
		log:                          bLogger.NewHelper(bLogger.NopLogger()),
		internalMessageRepo:          data.NewInternalMessageRepoForTest(entClient),
		internalMessageCategoryRepo:  data.NewInternalMessageCategoryRepoForTest(entClient),
		internalMessageRecipientRepo: data.NewInternalMessageRecipientRepoForTest(entClient),
		userRepo:                     &internalMessageServiceUserRepoStub{},
		authenticator:                nil,
		clientType:                   authenticationV1.ClientType_admin,
		internalMessagePublisher:     noopInternalMessagePublisher{},
		taskEnqueuer:                 nil,
	}
}

// TestInternalMessageServiceSqlite_ListMessageEnrichment 验证 ListMessage 的
// CategoryName 回填：挂了分类的消息回填分类名，未挂分类的保持空。
func TestInternalMessageServiceSqlite_ListMessageEnrichment(t *testing.T) {
	svc := newInternalMessageServiceForTest(t)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	err := svc.internalMessageCategoryRepo.Create(ctx, &internalMessageV1.CreateInternalMessageCategoryRequest{
		Data: &internalMessageV1.InternalMessageCategory{
			Name:      trans.Ptr("站内信分类甲"),
			Code:      trans.Ptr("IMCAT_SVC_A"),
			IsEnabled: trans.Ptr(true),
		},
	})
	require.NoError(t, err)

	// 分类仓储 Create 只返回 error，分类 ID 从列表反查（按唯一 code 定位）。
	catList, err := svc.internalMessageCategoryRepo.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	var categoryID uint32
	var catFound bool
	for _, cat := range catList.GetItems() {
		if cat.GetCode() == "IMCAT_SVC_A" {
			categoryID = cat.GetId()
			catFound = true
		}
	}
	require.True(t, catFound, "创建后分类应出现在列表中")

	withoutCategory, err := svc.internalMessageRepo.Create(ctx, &internalMessageV1.CreateInternalMessageRequest{
		Data: &internalMessageV1.InternalMessage{
			Title:   trans.Ptr("站内信Svc无分类消息"),
			Content: trans.Ptr("内容"),
			Status:  internalMessageV1.InternalMessage_PUBLISHED.Enum(),
			Type:    internalMessageV1.InternalMessage_NOTIFICATION.Enum(),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, withoutCategory)

	// 挂分类的消息：CategoryId 指向已建分类。
	msg, err := svc.internalMessageRepo.Create(ctx, &internalMessageV1.CreateInternalMessageRequest{
		Data: &internalMessageV1.InternalMessage{
			Title:      trans.Ptr("站内信Svc带分类消息"),
			Content:    trans.Ptr("内容"),
			Status:     internalMessageV1.InternalMessage_PUBLISHED.Enum(),
			Type:       internalMessageV1.InternalMessage_NOTIFICATION.Enum(),
			CategoryId: trans.Ptr(categoryID),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, msg)

	resp, err := svc.ListMessage(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(2), resp.GetTotal(), "两条消息都应出现在列表中")
	require.Len(t, resp.GetItems(), 2)

	for _, item := range resp.GetItems() {
		switch item.GetTitle() {
		case "站内信Svc带分类消息":
			require.Equal(t, "站内信分类甲", item.GetCategoryName(),
				"挂分类的消息应回填分类名")
		case "站内信Svc无分类消息":
			require.Empty(t, item.GetCategoryName(),
				"未挂分类的消息不应回填分类名")
		default:
			t.Fatalf("列表中出现未创建的消息 title=%q", item.GetTitle())
		}
	}
}

// TestInternalMessageServiceSqlite_GetMessageEnrichment 验证 GetMessage 单条查询的
// CategoryName 回填。
func TestInternalMessageServiceSqlite_GetMessageEnrichment(t *testing.T) {
	svc := newInternalMessageServiceForTest(t)
	ctx := enttest.NewSystemViewerCtx(context.Background())

	err := svc.internalMessageCategoryRepo.Create(ctx, &internalMessageV1.CreateInternalMessageCategoryRequest{
		Data: &internalMessageV1.InternalMessageCategory{
			Name:      trans.Ptr("站内信分类乙"),
			Code:      trans.Ptr("IMCAT_SVC_B"),
			IsEnabled: trans.Ptr(true),
		},
	})
	require.NoError(t, err)

	// 分类仓储 Create 只返回 error，分类 ID 从列表反查（按唯一 code 定位）。
	catList, err := svc.internalMessageCategoryRepo.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	var categoryID uint32
	var catFound bool
	for _, cat := range catList.GetItems() {
		if cat.GetCode() == "IMCAT_SVC_B" {
			categoryID = cat.GetId()
			catFound = true
		}
	}
	require.True(t, catFound, "创建后分类应出现在列表中")

	msg, err := svc.internalMessageRepo.Create(ctx, &internalMessageV1.CreateInternalMessageRequest{
		Data: &internalMessageV1.InternalMessage{
			Title:      trans.Ptr("站内信Svc单条富集消息"),
			Content:    trans.Ptr("内容"),
			Status:     internalMessageV1.InternalMessage_PUBLISHED.Enum(),
			Type:       internalMessageV1.InternalMessage_NOTIFICATION.Enum(),
			CategoryId: trans.Ptr(categoryID),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, msg.Id)

	resp, err := svc.GetMessage(ctx, &internalMessageV1.GetInternalMessageRequest{
		QueryBy: &internalMessageV1.GetInternalMessageRequest_Id{Id: msg.GetId()},
	})
	require.NoError(t, err)
	require.Equal(t, "站内信Svc单条富集消息", resp.GetTitle())
	require.Equal(t, "站内信分类乙", resp.GetCategoryName(),
		"GetMessage 应对挂分类的消息回填分类名")
}

// recordingInternalMessagePublisher：注册缝测试用 publisher 替身。
type recordingInternalMessagePublisher struct {
	publishCalls   int
	tryPublishOK   bool
	tryPublishHits int
}

func (r *recordingInternalMessagePublisher) Publish(context.Context, sse.StreamID, *sse.Event) {
	r.publishCalls++
}

func (r *recordingInternalMessagePublisher) TryPublish(context.Context, sse.StreamID, *sse.Event) bool {
	r.tryPublishHits++
	return r.tryPublishOK
}

// recordingTaskEnqueuer：注册缝测试用 TaskEnqueuer 替身。
type recordingTaskEnqueuer struct {
	newTaskCalls int
}

func (r *recordingTaskEnqueuer) NewTask(string, any, ...asynq.Option) error {
	r.newTaskCalls++
	return nil
}

// TestInternalMessageServiceSqlite_RegisterSeams 验证两个注册缝：
// 构造时为默认值（noop publisher / nil enqueuer），注册后被替换为传入实例。
func TestInternalMessageServiceSqlite_RegisterSeams(t *testing.T) {
	svc := newInternalMessageServiceForTest(t)

	// 生产构造器注入的默认值。
	require.IsType(t, noopInternalMessagePublisher{}, svc.internalMessagePublisher,
		"未注册前 publisher 应为 noop 默认实现")
	require.Nil(t, svc.taskEnqueuer,
		"未注册前 taskEnqueuer 应为 nil")

	publisher := &recordingInternalMessagePublisher{}
	svc.RegisterInternalMessagePublisher(publisher)
	gotPublisher, ok := svc.internalMessagePublisher.(*recordingInternalMessagePublisher)
	require.True(t, ok, "注册后 publisher 应被替换为注册实例")
	require.Same(t, publisher, gotPublisher)

	enqueuer := &recordingTaskEnqueuer{}
	svc.RegisterTaskEnqueuer(enqueuer)
	gotEnqueuer, ok := svc.taskEnqueuer.(*recordingTaskEnqueuer)
	require.True(t, ok, "注册后 taskEnqueuer 应被替换为注册实例")
	require.Same(t, enqueuer, gotEnqueuer)
}
