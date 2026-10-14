package pagination

import "sync"

// cursorSecret 是游标分页（cursor）的 HMAC-SHA256 签名密钥（进程级全局配置）。
//
// 它与 token 游标的签名密钥（SetTokenSecret/TokenSecret）刻意分开：
// token 游标在未配置密钥时保留"接受未签名 token"的兼容语义，而游标分页
// 未配置密钥时一律 fail-closed（既不生成也不接受游标），避免为兼容旧 token
// 而放开新游标的签名要求。
var (
	cursorSecretMu sync.RWMutex
	cursorSecret   []byte
)

// SetCursorSecret 设置游标分页的签名密钥。
//
// 建议：在进程启动时调用一次；密钥必须来自受控来源（配置中心/环境变量/密钥文件），
// 且所有副本一致，否则 A 副本签发的 next_cursor 在 B 副本上无法校验。
// 至少 32 字节高熵随机值；轮换密钥会使存量游标全部失效（客户端回到第一页）。
func SetCursorSecret(secret []byte) {
	cursorSecretMu.Lock()
	defer cursorSecretMu.Unlock()
	if secret == nil {
		cursorSecret = nil
		return
	}
	cursorSecret = make([]byte, len(secret))
	copy(cursorSecret, secret)
}

// CursorSecret 返回当前生效的游标签名密钥（未设置时为 nil）。
func CursorSecret() []byte {
	cursorSecretMu.RLock()
	defer cursorSecretMu.RUnlock()
	return cursorSecret
}
