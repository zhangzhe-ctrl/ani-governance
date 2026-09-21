package server

import (
	"crypto/sha256"
	"encoding/base64"
	"io"

	"github.com/go-kratos/kratos/v2/transport/http"
	"go-wind-admin/app/admin/service/internal/service"
)

const invitationScript = `
const token = location.hash.slice(1);
history.replaceState(null, '', location.pathname);
const form = document.querySelector('form');
const message = document.querySelector('#message');
const button = document.querySelector('button');
if (!token) { message.textContent = '邀请链接缺少令牌，请从邮件打开完整链接。'; button.disabled = true; }
form.addEventListener('submit', async (event) => {
  event.preventDefault();
  const password = form.elements.password.value;
  if (password !== form.elements.confirm.value) { message.textContent = '两次密码不一致。'; return; }
  button.disabled = true;
  message.textContent = '正在激活…';
  try {
    const response = await fetch(location.pathname, {
      method: 'POST', credentials: 'omit', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({token, password})
    });
    if (!response.ok) {
      const error = await response.json().catch(() => ({}));
      throw new Error(error.message || '激活失败，请检查链接是否过期。');
    }
    form.hidden = true;
    form.reset();
    message.textContent = '账号已激活。请返回原登录入口，使用新密码登录。';
  } catch (error) { message.textContent = error.message; button.disabled = false; }
});
`

// GET only serves the form; mail scanners cannot consume an invitation.
func registerInvitationPage(srv *http.Server) {
	srv.Route("/").GET(service.InvitationPath, func(ctx http.Context) error {
		header := ctx.Response().Header()
		header.Set("Content-Type", "text/html; charset=utf-8")
		header.Set("Cache-Control", "no-store")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("X-Content-Type-Options", "nosniff")
		hash := sha256.Sum256([]byte(invitationScript))
		header.Set("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		_, err := io.WriteString(ctx.Response(), `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>激活 ANI 账号</title><main><h1>激活 ANI 账号</h1><p>设置登录密码后即可激活账号。</p><form><p><label>新密码 <input name="password" type="password" autocomplete="new-password" required></label></p><p><label>确认密码 <input name="confirm" type="password" autocomplete="new-password" required></label></p><button type="submit">设置密码并激活</button></form><p id="message" role="status" aria-live="polite"></p></main><script>`+invitationScript+`</script></html>`)
		return err
	})
}
