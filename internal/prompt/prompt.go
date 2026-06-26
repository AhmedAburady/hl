package prompt

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// AddArgs are the values collected for the `add` command.
type AddArgs struct {
	Host    string
	Target  string
	DNSType string
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
	Name string
}

// ForLogin prompts for Technitium admin credentials.
func ForLogin(user, pass, name string) (LoginArgs, error) {
	a := LoginArgs{User: user, Pass: pass, Name: name}
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
		huh.NewInput().Title("Token name").Value(&a.Name),
	))
	if err := form.Run(); err != nil {
		return a, err
	}
	return a, nil
}

// ForDNSAdd prompts for missing pieces of a DNS record.
func ForDNSAdd(domain, value, zone string) (string, string, string, error) {
	if domain != "" && value != "" && zone != "" {
		return domain, value, zone, nil
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Record domain (FQDN)").Value(&domain).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("required")
				}
				return nil
			}),
		huh.NewInput().Title("Zone").Value(&zone).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("required")
				}
				return nil
			}),
		huh.NewInput().Title("Value (IP for A, target for CNAME)").Value(&value).
			Validate(func(s string) error {
				if s == "" {
					return fmt.Errorf("required")
				}
				return nil
			}),
	))
	if err := form.Run(); err != nil {
		return "", "", "", err
	}
	return domain, value, zone, nil
}
