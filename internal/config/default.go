package config

import "time"

func Default() *Config {
	return &Config{
		ActiveTheme: "dark",
		Refresh:     2 * time.Second,
		Accounts:    map[string]Account{},
		Tools: Tools{
			// empty so first install auto-picks a writable PATH dir under $HOME
			// (falling back to UserCacheDir) and persists the choice.
			InstallDir: "",
			Kubernetes: Tool{
				Exec:        "k9s",
				Args:        []string{"--kubeconfig", "{{.Kubeconfig}}"},
				Mode:        ModeTUI,
				AutoInstall: true,
			},
			MySQL: Tool{
				Exec:        "lazysql",
				Args:        []string{"{{.DSN}}"},
				Mode:        ModeTUI,
				AutoInstall: true,
			},
			PostgreSQL: Tool{
				Exec:        "lazysql",
				Args:        []string{"{{.DSN}}"},
				Mode:        ModeTUI,
				AutoInstall: true,
			},
			Lish: Tool{
				Exec:        "ssh",
				Args:        []string{"-t", "{{.Username}}@lish-{{.Region}}.linode.com", "{{.Label}}"},
				Mode:        ModeTUI,
				AutoInstall: false,
			},
		},
	}
}
