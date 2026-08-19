package datasource

import (
	"crypto/ed25519"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

var testAddr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2220}

// 契约：accept-new——未知主机首连放行并补录，再连一致放行，指纹变了拒绝。
func TestHostKeyVerifierAcceptNew(t *testing.T) {
	// 文件与父目录都不存在，验证首连路径会把它们建出来。
	path := filepath.Join(t.TempDir(), "ssh", "known_hosts")
	target := "example.com:2220"
	key1, key2 := testKey(t), testKey(t)

	cb, algos, err := hostKeyVerifier(path, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(algos) != 0 {
		t.Fatalf("未知主机不应有算法偏好，得到 %v", algos)
	}
	if err := cb(target, testAddr, key1); err != nil {
		t.Fatalf("首连应放行并补录: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("首连后应已写入 known_hosts: %v", err)
	}
	if !strings.Contains(string(raw), "[example.com]:2220") {
		t.Fatalf("补录行应带端口形式的主机名，实际内容: %q", raw)
	}

	// 重建 verifier 模拟下一次拨号：记录已在，一致的 key 放行。
	cb, algos, err = hostKeyVerifier(path, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(algos) != 1 || algos[0] != ssh.KeyAlgoED25519 {
		t.Fatalf("算法偏好应收窄到已存的 ed25519，得到 %v", algos)
	}
	if err := cb(target, testAddr, key1); err != nil {
		t.Fatalf("指纹一致应放行: %v", err)
	}

	// 同一主机换了把 key：拒绝，且不能被「再补录一条」洗白。
	err = cb(target, testAddr, key2)
	if err == nil {
		t.Fatal("指纹变化必须拒绝")
	}
	if !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("错误信息应说明指纹不一致: %v", err)
	}
	if raw, _ := os.ReadFile(path); strings.Count(string(raw), "[example.com]:2220") != 1 {
		t.Fatalf("拒绝路径不得追加新记录: %q", raw)
	}
}

// 契约：算法偏好来自 known_hosts 已有条目——服务器有多把 host key 时，
// 协商必须挑我们存过的类型，否则就是「手工 ssh 连得上、这里报 mismatch」。
func TestHostKeyVerifierNarrowsAlgorithms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	target := "example.com:2220"

	cb, _, err := hostKeyVerifier(path, target)
	if err != nil {
		t.Fatal(err)
	}
	if err := cb(target, testAddr, testKey(t)); err != nil {
		t.Fatal(err)
	}

	_, algos, err := hostKeyVerifier(path, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range algos {
		if strings.Contains(a, "ecdsa") || a == ssh.KeyAlgoRSA {
			t.Fatalf("偏好里不该出现没存过的类型: %v", algos)
		}
	}
	// 别的主机不受影响，仍走默认偏好。
	if _, algos, _ := hostKeyVerifier(path, "other.example.com:22"); len(algos) != 0 {
		t.Fatalf("未知主机不应有算法偏好，得到 %v", algos)
	}
}
