package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hirochachacha/go-smb2"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// target — минимальный файловый интерфейс архива. Пути относительны корня
// target; разделитель — '/'.
type target interface {
	Stat(rel string) (os.FileInfo, error)
	ReadDir(rel string) ([]os.FileInfo, error)
	MkdirAll(rel string) error
	Open(rel string) (io.ReadCloser, error)
	Create(rel string) (io.WriteCloser, error)
	WriteAt(rel string, p []byte, off int64) error
	Remove(rel string) error
	Rename(oldRel, newRel string) error
	Close() error
}

func joinRel(root, rel string) string {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "/")
	if root == "" {
		return path.Clean(rel)
	}
	return path.Join(root, rel)
}

// --- локальная флэшка ---

type localTarget struct {
	root    string
	mounted bool
}

func (t *localTarget) p(rel string) string {
	return filepath.Join(t.root, filepath.FromSlash(strings.TrimPrefix(rel, "/")))
}
func (t *localTarget) Stat(rel string) (os.FileInfo, error) { return os.Stat(t.p(rel)) }
func (t *localTarget) ReadDir(rel string) ([]os.FileInfo, error) {
	es, err := os.ReadDir(t.p(rel))
	if err != nil {
		return nil, err
	}
	out := make([]os.FileInfo, 0, len(es))
	for _, e := range es {
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}
func (t *localTarget) MkdirAll(rel string) error              { return os.MkdirAll(t.p(rel), 0o755) }
func (t *localTarget) Open(rel string) (io.ReadCloser, error) { return os.Open(t.p(rel)) }
func (t *localTarget) Create(rel string) (io.WriteCloser, error) {
	if err := os.MkdirAll(filepath.Dir(t.p(rel)), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(t.p(rel), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
}
func (t *localTarget) WriteAt(rel string, p []byte, off int64) error {
	if err := os.MkdirAll(filepath.Dir(t.p(rel)), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(t.p(rel), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := f.WriteAt(p, off)
	if err == nil && n != len(p) {
		return io.ErrShortWrite
	}
	return err
}
func (t *localTarget) Remove(rel string) error {
	err := os.Remove(t.p(rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (t *localTarget) Rename(a, b string) error {
	if err := os.MkdirAll(filepath.Dir(t.p(b)), 0o755); err != nil {
		return err
	}
	_ = os.Remove(t.p(b))
	return os.Rename(t.p(a), t.p(b))
}
func (t *localTarget) Close() error {
	if t.mounted && os.Getenv("MALINA_ARCH_FLASH_ROOT") == "" {
		t.mounted = false
		if err := exec.Command("umount", t.root).Run(); err != nil {
			return err
		}
	}
	return nil
}

// --- SFTP ---

type sftpTarget struct {
	cli  *ssh.Client
	sftp *sftp.Client
	root string
}

func dialSFTP(c networkConfig, password string) (*sftpTarget, error) {
	port := c.Port
	if port == 0 {
		port = 22
	}
	hostKey, err := hostKeyCallback(c.HostKey)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            c.User,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: hostKey,
		Timeout:         timeout(c.AccessSec),
	}
	cli, err := ssh.Dial("tcp", net.JoinHostPort(c.Host, fmt.Sprint(port)), cfg)
	if err != nil {
		return nil, err
	}
	sc, err := sftp.NewClient(cli)
	if err != nil {
		cli.Close()
		return nil, err
	}
	lx.Debugf("SFTP: подключено к %s:%d", c.Host, port)
	return &sftpTarget{cli: cli, sftp: sc, root: strings.TrimPrefix(c.Path, "/")}, nil
}

// hostKeyCallback принимает либо fingerprint (SHA256:.../MD5:...), либо полный
// публичный ключ в формате authorized_keys.
func hostKeyCallback(s string) (ssh.HostKeyCallback, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("SFTP: не задан host key fingerprint")
	}
	if strings.HasPrefix(s, "SHA256:") {
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintSHA256(key) != s {
				return fmt.Errorf("host key: ожидался %s", s)
			}
			return nil
		}, nil
	}
	if strings.HasPrefix(s, "MD5:") {
		want := strings.TrimPrefix(s, "MD5:")
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			if ssh.FingerprintLegacyMD5(key) != want {
				return fmt.Errorf("host key: ожидался MD5:%s", want)
			}
			return nil
		}, nil
	}
	k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(s))
	if err != nil {
		return nil, fmt.Errorf("host key: %w", err)
	}
	return ssh.FixedHostKey(k), nil
}

func (t *sftpTarget) p(rel string) string                       { return joinRel(t.root, rel) }
func (t *sftpTarget) Stat(rel string) (os.FileInfo, error)      { return t.sftp.Stat(t.p(rel)) }
func (t *sftpTarget) ReadDir(rel string) ([]os.FileInfo, error) { return t.sftp.ReadDir(t.p(rel)) }
func (t *sftpTarget) MkdirAll(rel string) error                 { return t.sftp.MkdirAll(t.p(rel)) }
func (t *sftpTarget) Open(rel string) (io.ReadCloser, error)    { return t.sftp.Open(t.p(rel)) }
func (t *sftpTarget) Create(rel string) (io.WriteCloser, error) {
	p := t.p(rel)
	if err := t.sftp.MkdirAll(path.Dir(p)); err != nil {
		return nil, err
	}
	return t.sftp.Create(p)
}
func (t *sftpTarget) WriteAt(rel string, b []byte, off int64) error {
	p := t.p(rel)
	if err := t.sftp.MkdirAll(path.Dir(p)); err != nil {
		return err
	}
	f, err := t.sftp.OpenFile(p, os.O_CREATE|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteAt(b, off)
	return err
}
func (t *sftpTarget) Remove(rel string) error {
	err := t.sftp.Remove(t.p(rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (t *sftpTarget) Rename(a, b string) error {
	if err := t.sftp.MkdirAll(path.Dir(t.p(b))); err != nil {
		return err
	}
	_ = t.sftp.Remove(t.p(b))
	if err := t.sftp.PosixRename(t.p(a), t.p(b)); err == nil {
		return nil
	}
	return t.sftp.Rename(t.p(a), t.p(b))
}
func (t *sftpTarget) Close() error { _ = t.sftp.Close(); return t.cli.Close() }

// --- SMB2/3 ---

type smbTarget struct {
	conn    net.Conn
	session *smb2.Session
	share   *smb2.Share
	root    string
}

func dialSMB(c networkConfig, password string) (*smbTarget, error) {
	port := c.Port
	if port == 0 {
		port = 445
	}
	shareName, root := splitShare(c.Path)
	if shareName == "" {
		return nil, errors.New("SMB: путь должен быть share[/path]")
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(c.Host, fmt.Sprint(port)), timeout(c.AccessSec))
	if err != nil {
		return nil, err
	}
	d := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: c.User, Password: password, Domain: c.Domain}}
	ctx, cancel := context.WithTimeout(context.Background(), timeout(c.AccessSec))
	defer cancel()
	sess, err := d.DialContext(ctx, conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	share, err := sess.Mount(shareName)
	if err != nil {
		sess.Logoff()
		conn.Close()
		return nil, err
	}
	lx.Debugf("SMB: подключено к %s:%d, share=%s", c.Host, port, shareName)
	return &smbTarget{conn: conn, session: sess, share: share, root: strings.TrimPrefix(root, "/")}, nil
}

func splitShare(p string) (string, string) {
	p = strings.TrimPrefix(strings.TrimSpace(p), "/")
	if p == "" {
		return "", ""
	}
	parts := strings.SplitN(p, "/", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}
func (t *smbTarget) p(rel string) string                       { return joinRel(t.root, rel) }
func (t *smbTarget) Stat(rel string) (os.FileInfo, error)      { return t.share.Stat(t.p(rel)) }
func (t *smbTarget) ReadDir(rel string) ([]os.FileInfo, error) { return t.share.ReadDir(t.p(rel)) }
func (t *smbTarget) MkdirAll(rel string) error                 { return t.share.MkdirAll(t.p(rel), 0o755) }
func (t *smbTarget) Open(rel string) (io.ReadCloser, error)    { return t.share.Open(t.p(rel)) }
func (t *smbTarget) Create(rel string) (io.WriteCloser, error) {
	p := t.p(rel)
	if err := t.share.MkdirAll(path.Dir(p), 0o755); err != nil {
		return nil, err
	}
	return t.share.Create(p)
}
func (t *smbTarget) WriteAt(rel string, b []byte, off int64) error {
	p := t.p(rel)
	if err := t.share.MkdirAll(path.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := t.share.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteAt(b, off)
	return err
}
func (t *smbTarget) Remove(rel string) error {
	err := t.share.Remove(t.p(rel))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
func (t *smbTarget) Rename(a, b string) error {
	if err := t.share.MkdirAll(path.Dir(t.p(b)), 0o755); err != nil {
		return err
	}
	_ = t.share.Remove(t.p(b))
	return t.share.Rename(t.p(a), t.p(b))
}
func (t *smbTarget) Close() error {
	if t.share != nil {
		_ = t.share.Umount()
	}
	if t.session != nil {
		_ = t.session.Logoff()
	}
	return t.conn.Close()
}

func timeout(sec int) time.Duration {
	if sec <= 0 {
		return 10 * time.Second
	}
	return time.Duration(sec) * time.Second
}

// blockParent возвращает родительское блочное устройство для раздела.
func blockParent(dev string) string {
	p, err := filepath.EvalSymlinks(filepath.Join("/sys/class/block", dev))
	if err != nil {
		return dev
	}
	if _, err := os.Stat(filepath.Join(p, "partition")); err == nil {
		return filepath.Base(filepath.Dir(p))
	}
	return dev
}

// rootBlockParents — родительские устройства разделов, на которых смонтированы
// "/" и "/malina". Их нельзя использовать как внешний target.
func rootBlockParents() (map[string]bool, error) {
	out := map[string]bool{}
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return out, err
	}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) < 2 || (f[1] != "/" && f[1] != "/malina") {
			continue
		}
		link, err := filepath.EvalSymlinks(f[0])
		if err != nil {
			continue
		}
		out[blockParent(filepath.Base(link))] = true
	}
	return out, nil
}

// externalBlock проверяет, что носитель внешний: removable=1 или USB в
// цепочке устройств (USB-HDD/SSD часто имеют removable=0).
func externalBlock(parent string) bool {
	if b, err := os.ReadFile(filepath.Join("/sys/block", parent, "removable")); err == nil {
		if strings.TrimSpace(string(b)) == "1" {
			return true
		}
	}
	p, err := filepath.EvalSymlinks(filepath.Join("/sys/block", parent, "device"))
	if err != nil {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, "usb") {
			return true
		}
	}
	return false
}

func checkFlashAllowed(uuid string) error {
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return errors.New("flash: не задан UUID раздела")
	}
	if len(uuid) > 64 || strings.ContainsAny(uuid, "/\\") || strings.Contains(uuid, "..") {
		return errors.New("flash: неверный UUID раздела")
	}
	link, err := filepath.EvalSymlinks("/dev/disk/by-uuid/" + uuid)
	if err != nil {
		return fmt.Errorf("flash: раздел %s не найден", uuid)
	}
	parent := blockParent(filepath.Base(link))
	roots, err := rootBlockParents()
	if err != nil {
		return errors.New("flash: не удалось определить системные диски")
	}
	if roots[parent] {
		return errors.New("flash: системный диск/раздел использовать нельзя")
	}
	if !externalBlock(parent) {
		return errors.New("flash: носитель не является внешним")
	}
	return nil
}
