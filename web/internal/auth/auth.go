package auth

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// ErrInvalidCredentials возвращается при неудачной аутентификации.
var ErrInvalidCredentials = errors.New("invalid credentials")

// Authenticator проверяет пару пользователь/пароль.
type Authenticator interface {
	Authenticate(user, password string) error
}

// PAM аутентифицирует через Linux-аккаунт с помощью хелпера PAM.
// Хелпер читает пароль из stdin и выходит с кодом 0 при успехе.
type PAM struct {
	HelperPath string
	User       string
}

func NewPAM(helperPath, user string) *PAM {
	return &PAM{HelperPath: helperPath, User: user}
}

func (p *PAM) Authenticate(user, password string) error {
	if user != p.User {
		return ErrInvalidCredentials
	}
	cmd := exec.Command(p.HelperPath, "-u", user)
	cmd.Stdin = strings.NewReader(password)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// Dev — аутентификатор только для разработки с фиксированным паролем.
type Dev struct {
	User     string
	Password string
}

func NewDev(user, password string) *Dev {
	return &Dev{User: user, Password: password}
}

func (d *Dev) Authenticate(user, password string) error {
	if user == d.User && password == d.Password {
		return nil
	}
	return ErrInvalidCredentials
}

// ChangePassword меняет пароль Linux-аккаунта через chpasswd (нужен root).
// Корень смонтирован ro, поэтому используется хелпер malina-rw
// (короткое сериализованное окно rw).
func ChangePassword(user, password string) error {
	// Защита от инъекции строк в chpasswd: `:` и переводы строк формируют
	// дополнительные записи "пользователь:пароль" (эскалация до root).
	if user == "" || strings.ContainsAny(user, "\n\r:") ||
		strings.ContainsAny(password, "\n\r:") ||
		strings.IndexFunc(password, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return ErrInvalidCredentials
	}
	cmd := exec.Command("/malina/usr/local/sbin/malina-rw", "run", "chpasswd")
	cmd.Stdin = strings.NewReader(user + ":" + password + "\n")
	return cmd.Run()
}
