package password

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestECDSACrypto_EncryptAndVerify(t *testing.T) {
	crypto, err := NewECDSACrypto()
	if err != nil {
		t.Fatalf("创建 ECDSACrypto 实例失败: %v", err)
	}

	message := "test message"

	// 签名消息
	encrypted, err := crypto.Encrypt(message)
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}

	// 验证签名
	isValid, err := crypto.Verify(message, encrypted)
	if err != nil {
		t.Fatalf("验证失败: %v", err)
	}

	if !isValid {
		t.Fatal("签名验证未通过")
	}
}

// ecdhPublicKeyForPeer 取出 Encrypt 输出中裸的 base64 公钥：
// 当前 DeriveSharedSecret 只接受不带 "ecdh$" 前缀的公钥。
func ecdhPublicKeyForPeer(t *testing.T, encrypted string) string {
	t.Helper()
	const prefix = "ecdh$"
	if !strings.HasPrefix(encrypted, prefix) {
		t.Fatalf("Encrypt 应返回 %s 前缀的公钥，实际为 %q", prefix, encrypted)
	}
	return strings.TrimPrefix(encrypted, prefix)
}

// TestECDHCrypto_EncryptAndVerify 是当前 ECDH 实现的合同测试，不是口令散列用例：
// Encrypt 输出本方公钥（"ecdh$" + base64(未压缩曲线点)），Verify 比较
// 本方由对端公钥推导出的共享 X 坐标字节与传入内容的 SHA-256。
// 因此任意字符串（例如 "test message"）不是共享秘密，用它验证必然失败。
// 这里保留两个相互独立的实例，各自持有自己的密钥对，完成一次真实的密钥协商。
func TestECDHCrypto_EncryptAndVerify(t *testing.T) {
	crypto1, err := NewECDHCrypto()
	if err != nil {
		t.Fatalf("创建 ECDHCrypto 实例1失败: %v", err)
	}

	crypto2, err := NewECDHCrypto()
	if err != nil {
		t.Fatalf("创建 ECDHCrypto 实例2失败: %v", err)
	}

	message := "test message"

	// Encrypt 返回各自的公钥，message 只用于触发非空校验分支。
	encrypted1, err := crypto1.Encrypt(message)
	if err != nil {
		t.Fatalf("实例1加密失败: %v", err)
	}
	encrypted2, err := crypto2.Encrypt(message)
	if err != nil {
		t.Fatalf("实例2加密失败: %v", err)
	}

	// 带前缀的输出不能直接喂给 DeriveSharedSecret，这是当前方法的既定要求。
	if _, err := crypto1.DeriveSharedSecret(encrypted2); err == nil {
		t.Fatal("DeriveSharedSecret 不应接受带 ecdh$ 前缀的公钥")
	}

	publicKey1 := ecdhPublicKeyForPeer(t, encrypted1)
	publicKey2 := ecdhPublicKeyForPeer(t, encrypted2)

	// 交叉推导：实例1 用实例2 的公钥，实例2 用实例1 的公钥。
	shared1, err := crypto1.DeriveSharedSecret(publicKey2)
	if err != nil {
		t.Fatalf("实例1推导共享秘密失败: %v", err)
	}
	shared2, err := crypto2.DeriveSharedSecret(publicKey1)
	if err != nil {
		t.Fatalf("实例2推导共享秘密失败: %v", err)
	}

	if len(shared1) == 0 {
		t.Fatal("实例1推导的共享秘密为空")
	}
	if !bytes.Equal(shared1, shared2) {
		t.Fatal("两个独立实例推导的共享秘密不一致")
	}

	// 按当前 Verify 合同，用原始共享字节的字符串形式验证对端公钥。
	isValid, err := crypto1.Verify(string(shared1), encrypted2)
	if err != nil {
		t.Fatalf("验证共享秘密失败: %v", err)
	}
	if !isValid {
		t.Fatal("共享密钥验证未通过")
	}

	isValid, err = crypto2.Verify(string(shared2), encrypted1)
	if err != nil {
		t.Fatalf("反向验证共享秘密失败: %v", err)
	}
	if !isValid {
		t.Fatal("反向共享密钥验证未通过")
	}

	// 负向：篡改共享秘密的一个字节必须验证不通过，且按合同返回结果而非错误。
	tampered := make([]byte, len(shared1))
	copy(tampered, shared1)
	tampered[len(tampered)-1] ^= 0x01
	isValid, err = crypto1.Verify(string(tampered), encrypted2)
	if err != nil {
		t.Fatalf("篡改共享秘密应返回验证结果而不是错误: %v", err)
	}
	if isValid {
		t.Fatal("篡改后的共享秘密不应通过验证")
	}

	// 负向：无法解析的公钥必须报错。
	badKey := "ecdh$" + base64.StdEncoding.EncodeToString([]byte{0x04, 0x01, 0x02})
	if _, err := crypto1.Verify(string(shared1), badKey); err == nil {
		t.Fatal("非法公钥应当返回错误")
	}

	// 负向：任意字符串不是共享秘密。
	isValid, err = crypto1.Verify(message, encrypted2)
	if err != nil {
		t.Fatalf("验证任意字符串失败: %v", err)
	}
	if isValid {
		t.Fatal("任意字符串不应作为共享秘密通过验证")
	}
}
