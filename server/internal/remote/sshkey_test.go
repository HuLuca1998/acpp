package remote

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"acpp/server/internal/service"
)

// 契约：点一下就能有一把新钥匙——生成 ed25519，指纹与公钥当场算出来，
// 私钥内容存得住且解析得开。公钥那一行是拿去装进目标机器 authorized_keys 的，
// 格式必须是真的。
func TestService_CreateKey_GeneratesUsableED25519(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()

	key, err := s.CreateKey(ctx, KeyInput{Name: "deploy-key", Generate: true, Note: "部署机通用"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Fatalf("fingerprint = %q, want a SHA256 one", key.Fingerprint)
	}
	if !strings.HasPrefix(key.PublicKey, "ssh-ed25519 ") {
		t.Fatalf("public key = %q, want an ssh-ed25519 authorized_keys line", key.PublicKey)
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.PublicKey)); err != nil {
		t.Fatalf("public key is not a valid authorized_keys line: %v", err)
	}
	priv, _, err := s.KeySecret(ctx, key.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	signer, err := ssh.ParsePrivateKey([]byte(priv))
	if err != nil {
		t.Fatalf("stored private key does not parse: %v", err)
	}
	if got := ssh.FingerprintSHA256(signer.PublicKey()); got != key.Fingerprint {
		t.Fatalf("fingerprint %q does not match the stored key %q", key.Fingerprint, got)
	}
}

// 契约：粘贴进来的私钥同样算出指纹与公钥——界面靠指纹认出是哪一把，
// 名字可以起得很随意。
func TestService_CreateKey_DerivesIdentityFromPastedKey(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	src, err := s.CreateKey(ctx, KeyInput{Name: "origin", Generate: true})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	pem, _, err := s.KeySecret(ctx, src.ID)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}

	pasted, err := s.CreateKey(ctx, KeyInput{Name: "pasted", PrivateKey: pem})
	if err != nil {
		t.Fatalf("paste: %v", err)
	}

	if pasted.Fingerprint != src.Fingerprint {
		t.Fatalf("same key got different fingerprints: %q vs %q", pasted.Fingerprint, src.Fingerprint)
	}
}

// 契约：解析不开的私钥当场拒绝。存一把连不上的钥匙、等拨号时才失败，
// 那时人早就忘了自己粘过什么。
func TestService_CreateKey_RejectsGarbage(t *testing.T) {
	s, _ := testService(t)

	_, err := s.CreateKey(context.Background(), KeyInput{
		Name: "broken", PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nnot really\n-----END OPENSSH PRIVATE KEY-----",
	})

	if !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

// 契约：还有服务器在用就不给删。直接删掉只会让那几台机器在下次连接时才
// 报错，而那时没人会想到是钥匙没了。
func TestService_DeleteKey_RefusesWhileServersUseIt(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	key, err := s.CreateKey(ctx, KeyInput{Name: "deploy-key", Generate: true})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if _, err := s.Create(ctx, Input{
		Name: "shop-live", Host: "203.0.113.24", Port: 22, User: "deploy",
		Auth: "key", KeyID: &key.ID,
	}); err != nil {
		t.Fatalf("create server: %v", err)
	}

	err = s.DeleteKey(ctx, key.ID)

	if !errors.Is(err, service.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if _, err := s.GetKey(ctx, key.ID); err != nil {
		t.Fatalf("key should survive a refused delete: %v", err)
	}
}

// 契约：服务器选了钥匙，读出来的记录就带着私钥内容——拨号的两条路（服务器
// 观察与数据源隧道）都走这里取配置，填在读取路径上两边自动拿到。
func TestService_Get_AttachesSelectedKeyMaterial(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	key, err := s.CreateKey(ctx, KeyInput{Name: "deploy-key", Generate: true})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	created, err := s.Create(ctx, Input{
		Name: "shop-live", Host: "203.0.113.24", Port: 22, User: "deploy",
		Auth: "key", KeyID: &key.ID,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	srv, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if !strings.Contains(srv.KeyData, "OPENSSH PRIVATE KEY") {
		t.Fatalf("server record carries no key material: %q", srv.KeyData)
	}
	if cfg := configOf(srv); cfg.KeyData == "" {
		t.Fatal("dial config lost the key material")
	}
}

// 契约：钥匙被删了（或从没接上），读出来的记录不带私钥，也不报错——列表与
// 编辑页不该因为一把钥匙没了就打不开，拨号时自会说「没有可用的认证方式」。
func TestService_Get_SurvivesMissingKey(t *testing.T) {
	s, gdb := testService(t)
	ctx := context.Background()
	missing := uint(404)
	created, err := s.Create(ctx, Input{
		Name: "shop-live", Host: "203.0.113.24", Port: 22, User: "deploy",
		Auth: "key", KeyID: &missing,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	_ = gdb

	srv, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if srv.KeyData != "" {
		t.Fatalf("expected no key material, got %q", srv.KeyData)
	}
}

// 契约：AI 工具按名字挑服务器时同样带上私钥内容。曾经漏过：工具走列表查询，
// 拿不到钥匙就退回读 KeyPath，从别的机器同步来的旧路径在本机读不到，
// 报「读私钥失败」——即便服务器明明选了私钥库里的钥匙。
func TestService_Pick_AttachesSelectedKeyMaterial(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	key, err := s.CreateKey(ctx, KeyInput{Name: "deploy-key", Generate: true})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	for _, name := range []string{"shop-live", "shop-staging"} {
		if _, err := s.Create(ctx, Input{
			Name: name, Host: "203.0.113.24", Port: 22, User: "deploy",
			Auth: "key", KeyID: &key.ID, KeyPath: "/nonexistent/other-machine/key",
		}); err != nil {
			t.Fatalf("create server %s: %v", name, err)
		}
	}

	srv, err := s.pick(ctx, Scope{}, "shop-live")
	if err != nil {
		t.Fatalf("pick: %v", err)
	}

	if !strings.Contains(srv.KeyData, "OPENSSH PRIVATE KEY") {
		t.Fatalf("picked server carries no key material: %q", srv.KeyData)
	}
}

// 契约：选了钥匙却查不到（被删了）时，拨号配置不退回旧的 KeyPath——
// 那条路径多半属于别的机器，读它只会报一个误导人的「文件不存在」。
func TestService_Get_SelectedKeyIgnoresLegacyPath(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	missing := uint(404)
	created, err := s.Create(ctx, Input{
		Name: "shop-live", Host: "203.0.113.24", Port: 22, User: "deploy",
		Auth: "key", KeyID: &missing, KeyPath: "/nonexistent/other-machine/key",
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	srv, err := s.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if cfg := configOf(srv); cfg.KeyPath != "" {
		t.Fatalf("dial config fell back to legacy key path %q", cfg.KeyPath)
	}
}
