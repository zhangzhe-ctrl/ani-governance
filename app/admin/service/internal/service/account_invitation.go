package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strings"
	"time"

	adminV1 "go-wind-admin/api/gen/go/admin/service/v1"
	authenticationV1 "go-wind-admin/api/gen/go/authentication/service/v1"
	identityV1 "go-wind-admin/api/gen/go/identity/service/v1"
	"go-wind-admin/app/admin/service/internal/data"
	"go-wind-admin/pkg/localdeps/go-utils/trans"
	"go-wind-admin/pkg/mailer"
	"google.golang.org/protobuf/types/known/emptypb"
)

const InvitationPath = "/api/v1/auth/invitations/accept"

// accountInvitation is prepared only for EMAIL_INVITATION; the immediate path
// has no mail/config dependency. Its raw token lives only until SMTP completes.
type accountInvitation struct {
	token, email, link string
	expires            time.Time
	account            *data.SmtpAccount
}

func prepareAccountInvitation(ctx context.Context, channels *data.NotificationChannelRepo, mode identityV1.ActivationMode, user *identityV1.User, password string) (*accountInvitation, error) {
	if mode == identityV1.ActivationMode_IMMEDIATE {
		return nil, nil
	}
	if mode != identityV1.ActivationMode_EMAIL_INVITATION {
		return nil, adminV1.ErrorBadRequest("unsupported activation mode")
	}
	if user == nil || password != "" {
		return nil, adminV1.ErrorBadRequest("email invitation does not accept a preset password")
	}
	email := strings.TrimSpace(user.GetEmail())
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || strings.ContainsAny(email, "\r\n") {
		return nil, adminV1.ErrorBadRequest("email invitation requires a valid email address")
	}
	base, err := url.Parse(os.Getenv("ANI_INVITATION_BASE_URL"))
	if err != nil || base == nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, adminV1.ErrorBadRequest("ANI_INVITATION_BASE_URL must be a public origin")
	}
	host := base.Hostname()
	ip := net.ParseIP(host)
	if base.Scheme != "https" && !(base.Scheme == "http" && (host == "localhost" || (ip != nil && ip.IsLoopback()))) {
		return nil, adminV1.ErrorBadRequest("invitation origin requires HTTPS (HTTP is allowed only on loopback)")
	}
	if channels == nil {
		return nil, adminV1.ErrorBadRequest("email channel is not configured")
	}
	account, err := channels.GetFirstEnabledEmailChannel(ctx)
	if err != nil {
		return nil, err
	}
	if account.Host == "" || account.Port == 0 {
		return nil, adminV1.ErrorBadRequest("email channel host and port are required")
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return nil, adminV1.ErrorInternalServerError("generate invitation token failed")
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	base.Path, base.Fragment = InvitationPath, token
	user.Email = trans.Ptr(email)
	user.Status = identityV1.User_PENDING.Enum()
	return &accountInvitation{token: token, email: email, link: base.String(), expires: time.Now().Add(24 * time.Hour), account: account}, nil
}

func (i *accountInvitation) send(ctx context.Context, username string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := mailer.SendMailContext(ctx, mailer.SmtpConfig{
		Host: i.account.Host, Port: i.account.Port, Username: i.account.Username,
		Password: i.account.Password, From: i.account.From, TlsMode: i.account.TlsMode,
	}, []string{i.email}, "ANI 账号邀请", fmt.Sprintf("您受邀开通 ANI 账号 %s。\n\n请在 24 小时内打开以下链接，设置密码并激活：\n%s\n\n若非您预期的邀请，请忽略此邮件。", username, i.link))
	if err != nil {
		// SMTP errors may include addresses/provider text. Never log the invitation
		// body or expose provider details from this security-sensitive operation.
		return adminV1.ErrorInternalServerError("invitation email could not be sent; account creation was rolled back")
	}
	return nil
}

func (s *AuthenticationService) AcceptInvitation(ctx context.Context, req *authenticationV1.AcceptInvitationRequest) (*emptypb.Empty, error) {
	if req == nil || req.Token == "" || req.Password == "" || len(req.Password) > 72 {
		return nil, authenticationV1.ErrorBadRequest("invitation token and password (at most 72 bytes) are required")
	}
	if err := s.userCredentialRepo.AcceptInvitation(ctx, req.Token, req.Password); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
