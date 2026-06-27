package prompt

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// AddArgs are the values collected for the `add` command.
type AddArgs struct {
	Host   string
	Target string
}

// ForAdd prompts for any missing positional values for the add command.
func ForAdd(host, target string) (AddArgs, error) {
	a := AddArgs{Host: host, Target: target}
	var forms []*huh.Form

	if host == "" {
		forms = append(forms, huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Host (FQDN to serve)").Value(&a.Host).
				Placeholder("app.home.lab").Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("required")
				}
				return nil
			}),
		)))
	}
	if target == "" {
		forms = append(forms, huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Upstream target (host:port)").Value(&a.Target).
				Placeholder("192.168.1.50:8080").Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("required")
				}
				return nil
			}),
		)))
	}
	for _, f := range forms {
		if err := f.Run(); err != nil {
			return a, err
		}
	}
	return a, nil
}

// LoginArgs are the values collected for `dns login`.
type LoginArgs struct {
	User string
	Pass string
	TOTP string
	Name string
}

// ForLogin prompts for Technitium admin credentials. The TOTP field is left
// blank if the account has no 2FA configured.
func ForLogin(user, pass, totp, name string) (LoginArgs, error) {
	a := LoginArgs{User: user, Pass: pass, TOTP: totp, Name: name}
	if name == "" {
		a.Name = "hl"
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Technitium admin user").Value(&a.User).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("required")
				}
				return nil
			}),
		huh.NewInput().Title("Password").EchoMode(huh.EchoModePassword).Value(&a.Pass).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("required")
				}
				return nil
			}),
		huh.NewInput().Title("2FA code (leave blank if not enabled)").Value(&a.TOTP),
		huh.NewInput().Title("Token name").Value(&a.Name),
	))
	if err := form.Run(); err != nil {
		return a, err
	}
	return a, nil
}
