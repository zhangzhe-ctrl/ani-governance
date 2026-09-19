package service

import (
	"context"
	"strconv"

	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"github.com/tx7do/go-utils/trans"
	"google.golang.org/protobuf/types/known/emptypb"
	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	notificationChannelV1 "go-wind-admin/api/gen/go/notification_channel/service/v1"
	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"

	"go-wind-admin/pkg/middleware/auth"
	"go-wind-admin/pkg/mailer"

	"go-wind-admin/app/admin/service/internal/data"
)

// NotificationChannelService 通知渠道管理（平台级配置）。
// 一期实现 EMAIL（SMTP）渠道：CRUD + 测试发送；找回密码/联系方式验证
// 等下游能力在渠道可用后接入。
type NotificationChannelService struct {
	adminV1.NotificationChannelServiceHTTPServer

	log  *bLogger.Helper
	repo *data.NotificationChannelRepo
}

func NewNotificationChannelService(
	ctx *bootstrap.Context,
	repo *data.NotificationChannelRepo,
) *NotificationChannelService {
	return &NotificationChannelService{
		log:  ctx.NewLoggerHelper("notification-channel/service/admin-service"),
		repo: repo,
	}
}

func (s *NotificationChannelService) ListNotificationChannel(ctx context.Context, req *paginationV1.PagingRequest) (*notificationChannelV1.ListNotificationChannelResponse, error) {
	return s.repo.List(ctx, req)
}

func (s *NotificationChannelService) GetNotificationChannel(ctx context.Context, req *notificationChannelV1.GetNotificationChannelRequest) (*notificationChannelV1.NotificationChannel, error) {
	if req == nil || req.GetId() == 0 {
		return nil, adminV1.ErrorBadRequest("id is required")
	}
	return s.repo.Get(ctx, req.GetId())
}

func (s *NotificationChannelService) CreateNotificationChannel(ctx context.Context, req *notificationChannelV1.CreateNotificationChannelRequest) (*notificationChannelV1.NotificationChannel, error) {
	if req == nil || req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	id, err := s.repo.Create(ctx, req, operator.UserId)
	if err != nil {
		return nil, err
	}

	return s.repo.Get(ctx, id)
}

func (s *NotificationChannelService) UpdateNotificationChannel(ctx context.Context, req *notificationChannelV1.UpdateNotificationChannelRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, adminV1.ErrorBadRequest("invalid parameter")
	}

	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	req.Data.UpdatedBy = trans.Ptr(operator.UserId)

	if err = s.repo.Update(ctx, req, operator.UserId); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *NotificationChannelService) DeleteNotificationChannel(ctx context.Context, req *notificationChannelV1.DeleteNotificationChannelRequest) (*emptypb.Empty, error) {
	if err := s.repo.Delete(ctx, req.GetId()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// SendTestEmail 向指定收件人发送测试邮件，验证渠道配置是否可用。
// 渠道未启用时拒绝发送，避免误以为配置可用。
func (s *NotificationChannelService) SendTestEmail(ctx context.Context, req *notificationChannelV1.SendTestEmailRequest) (*emptypb.Empty, error) {
	if req == nil || req.GetId() == 0 {
		return nil, adminV1.ErrorBadRequest("id is required")
	}
	if req.GetRecipient() == "" {
		return nil, adminV1.ErrorBadRequest("recipient is required")
	}

	channel, err := s.repo.Get(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	if channel.GetType() != notificationChannelV1.NotificationChannel_EMAIL {
		return nil, adminV1.ErrorBadRequest("test email is only available for EMAIL channels")
	}

	account, err := s.repo.GetDecryptedSmtpAccount(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	if !account.Enabled {
		return nil, adminV1.ErrorBadRequest("notification channel is disabled")
	}

	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	subject := "GoWind Admin 通知渠道测试邮件"
	body := "这是一封来自 GoWind Admin 的测试邮件。\n" +
		"如果您收到了它，说明渠道 [" + strconv.FormatUint(uint64(req.GetId()), 10) + "] 配置可用。\n" +
		"操作人用户 ID: " + itoa(operator.UserId) + "\n"

	err = mailer.SendMail(mailer.SmtpConfig{
		Host:     account.Host,
		Port:     account.Port,
		Username: account.Username,
		Password: account.Password,
		From:     account.From,
		TlsMode:  account.TlsMode,
	}, []string{req.GetRecipient()}, subject, body)
	if err != nil {
		s.log.Errorf(ctx, "send test email via channel [%d] to [%s] failed: %v", req.GetId(), req.GetRecipient(), err)
		return nil, adminV1.ErrorBadRequest("%s", "send test email failed: "+err.Error())
	}

	s.log.Infof(ctx, "test email sent via channel [%d] to [%s] by operator [%d]", req.GetId(), req.GetRecipient(), operator.UserId)
	return &emptypb.Empty{}, nil
}

func itoa(v uint32) string {
	return strconv.FormatUint(uint64(v), 10)
}
