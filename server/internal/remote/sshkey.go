package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"acpp/server/internal/model"
	"acpp/server/internal/service"
	"acpp/server/internal/sshdial"
)

// 私钥库：一把钥匙开好几台机器是常态，所以私钥独立成表，服务器引用它
// （见 model.SSHKey 的注释）。私钥内容入库是为了让整套连接配置能搬到
// 另一台电脑——路径搬不走。

// KeyInput 是新建/更新私钥的入参。
type KeyInput struct {
	Name string `json:"name"`
	// PrivateKey 是粘贴进来的 PEM 私钥。更新时留空表示不动原来的。
	PrivateKey string `json:"privateKey"`
	// Passphrase 是私钥的通行短语。nil 表示不动，空串表示清掉。
	Passphrase *string `json:"passphrase"`
	Note       string  `json:"note"`
	// Generate 为真时忽略 PrivateKey，当场生成一把新的 ed25519 钥匙。
	Generate bool `json:"generate"`
	// KeyPath 是「从本机文件导入」：读那个文件的内容存进来，之后就不再
	// 依赖这个路径了。与 PrivateKey / Generate 三选一。
	KeyPath string `json:"keyPath"`
}

// ListKeys 返回全部私钥，附带「被几台服务器用着」。
func (s *Service) ListKeys(ctx context.Context, keyword string) ([]model.SSHKey, error) {
	q := s.db.WithContext(ctx).Model(&model.SSHKey{})
	if kw := strings.TrimSpace(keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("name LIKE ? OR note LIKE ? OR fingerprint LIKE ?", like, like, like)
	}
	var keys []model.SSHKey
	if err := q.Order("name").Find(&keys).Error; err != nil {
		return nil, fmt.Errorf("list ssh keys: %w", err)
	}
	s.attachKeyUsage(ctx, keys)
	for i := range keys {
		keys[i].HasPassphrase = keys[i].Passphrase != ""
	}
	return keys, nil
}

// attachKeyUsage 数一遍每把钥匙被几台服务器引用。删之前得让人看见影响谁。
func (s *Service) attachKeyUsage(ctx context.Context, keys []model.SSHKey) {
	if len(keys) == 0 {
		return
	}
	ids := make([]uint, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	var rows []struct {
		KeyID uint
		N     int64
	}
	err := s.db.WithContext(ctx).Model(&model.Server{}).
		Select("key_id, count(*) as n").
		Where("key_id IN ?", ids).Group("key_id").Scan(&rows).Error
	if err != nil {
		return // 计数是观测数据，取不到不该让列表失败
	}
	byID := make(map[uint]int64, len(rows))
	for _, r := range rows {
		byID[r.KeyID] = r.N
	}
	for i := range keys {
		keys[i].UsedBy = byID[keys[i].ID]
	}
}

// GetKey 取一把钥匙（不含私钥内容——那条走 KeySecret）。
func (s *Service) GetKey(ctx context.Context, id uint) (*model.SSHKey, error) {
	var key model.SSHKey
	if err := s.db.WithContext(ctx).First(&key, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("ssh key %d: %w", id, service.ErrNotFound)
		}
		return nil, fmt.Errorf("get ssh key: %w", err)
	}
	key.HasPassphrase = key.Passphrase != ""
	s.attachKeyUsage(ctx, []model.SSHKey{key})
	return &key, nil
}

// KeySecret 取回私钥内容与通行短语。owner 专属——与服务器密码同一条规矩：
// 存进来的凭证，本人要拿得回去（装到别的机器上、或整套搬家）。
func (s *Service) KeySecret(ctx context.Context, id uint) (privateKey, passphrase string, err error) {
	var key model.SSHKey
	if err := s.db.WithContext(ctx).First(&key, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", "", fmt.Errorf("ssh key %d: %w", id, service.ErrNotFound)
		}
		return "", "", fmt.Errorf("get ssh key: %w", err)
	}
	return key.PrivateKey, key.Passphrase, nil
}

// CreateKey 新建一把钥匙：生成、粘贴内容、从本机文件导入，三条路。
func (s *Service) CreateKey(ctx context.Context, in KeyInput) (*model.SSHKey, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: 私钥名称不能为空", service.ErrInvalid)
	}
	key := model.SSHKey{Name: name, Note: strings.TrimSpace(in.Note)}
	if in.Passphrase != nil {
		key.Passphrase = *in.Passphrase
	}
	if err := s.fillKeyMaterial(&key, in); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(&key).Error; err != nil {
		return nil, wrapKeyWrite(err, key)
	}
	key.HasPassphrase = key.Passphrase != ""
	return &key, nil
}

// UpdateKey 改名字/备注，或换一把新私钥。
func (s *Service) UpdateKey(ctx context.Context, id uint, in KeyInput) (*model.SSHKey, error) {
	key, err := s.GetKey(ctx, id)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(in.Name); name != "" {
		key.Name = name
	}
	key.Note = strings.TrimSpace(in.Note)
	if in.Passphrase != nil {
		key.Passphrase = *in.Passphrase
	}
	// 私钥留空 = 不动原来的：与密码字段同一套「留空表示不修改」的约定。
	if in.Generate || strings.TrimSpace(in.PrivateKey) != "" || strings.TrimSpace(in.KeyPath) != "" {
		if err := s.fillKeyMaterial(key, in); err != nil {
			return nil, err
		}
	} else if err := s.refreshKeyIdentity(key); err != nil {
		// 只改了通行短语时也重算一次：短语对不对，这里就能发现。
		return nil, err
	}
	if err := s.db.WithContext(ctx).Save(key).Error; err != nil {
		return nil, wrapKeyWrite(err, *key)
	}
	key.HasPassphrase = key.Passphrase != ""
	return key, nil
}

// DeleteKey 删一把钥匙。还有服务器在用就拒绝——直接删掉只会让那几台机器
// 在下次连接时才报错，而那时人早就忘了动过什么。
func (s *Service) DeleteKey(ctx context.Context, id uint) error {
	var used int64
	if err := s.db.WithContext(ctx).Model(&model.Server{}).
		Where("key_id = ?", id).Count(&used).Error; err != nil {
		return fmt.Errorf("count ssh key usage: %w", err)
	}
	if used > 0 {
		return fmt.Errorf("%w: 还有 %d 台服务器在用这把私钥，先改掉它们的配置", service.ErrInvalid, used)
	}
	if err := s.db.WithContext(ctx).Delete(&model.SSHKey{}, id).Error; err != nil {
		return fmt.Errorf("delete ssh key: %w", err)
	}
	return nil
}

// fillKeyMaterial 按入参装载私钥内容，并算出指纹与公钥。
func (s *Service) fillKeyMaterial(key *model.SSHKey, in KeyInput) error {
	switch {
	case in.Generate:
		priv, err := generateED25519(key.Name, key.Passphrase)
		if err != nil {
			return err
		}
		key.PrivateKey = priv
	case strings.TrimSpace(in.PrivateKey) != "":
		key.PrivateKey = strings.TrimSpace(in.PrivateKey) + "\n"
	case strings.TrimSpace(in.KeyPath) != "":
		raw, err := os.ReadFile(sshdial.ExpandHome(strings.TrimSpace(in.KeyPath)))
		if err != nil {
			return fmt.Errorf("%w: 读私钥文件失败: %v", service.ErrInvalid, err)
		}
		key.PrivateKey = string(raw)
	default:
		return fmt.Errorf("%w: 粘贴私钥内容、选本机私钥文件，或直接生成一把新的", service.ErrInvalid)
	}
	return s.refreshKeyIdentity(key)
}

// refreshKeyIdentity 解析私钥，算出指纹与公钥。**解析不过就拒绝保存**：
// 与其存一把连不上的钥匙、等到拨号时才报错，不如在这里就说清楚。
func (s *Service) refreshKeyIdentity(key *model.SSHKey) error {
	var (
		signer ssh.Signer
		err    error
	)
	if key.Passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(key.PrivateKey), []byte(key.Passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(key.PrivateKey))
	}
	if err != nil {
		return fmt.Errorf("%w: 解析私钥失败（带密码的私钥要填通行短语）: %v", service.ErrInvalid, err)
	}
	key.Fingerprint = ssh.FingerprintSHA256(signer.PublicKey())
	key.PublicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if key.Name != "" {
		key.PublicKey += " " + key.Name
	}
	return nil
}

// generateED25519 当场生成一把 ed25519 钥匙。选 ed25519 不选 RSA：短、快、
// 现在的 OpenSSH 默认就是它，没有选长度的心智负担。
func generateED25519(comment, passphrase string) (string, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate ed25519 key: %w", err)
	}
	var block *pem.Block
	if passphrase != "" {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, comment, []byte(passphrase))
	} else {
		block, err = ssh.MarshalPrivateKey(priv, comment)
	}
	if err != nil {
		return "", fmt.Errorf("encode private key: %w", err)
	}
	return string(pem.EncodeToMemory(block)), nil
}

// wrapKeyWrite 把唯一索引冲突翻成人话。
func wrapKeyWrite(err error, key model.SSHKey) error {
	if strings.Contains(strings.ToLower(err.Error()), "unique") {
		return fmt.Errorf("%w: 已经有一把叫 %s 的私钥了", service.ErrInvalid, key.Name)
	}
	return fmt.Errorf("save ssh key: %w", err)
}
