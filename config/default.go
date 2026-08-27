package config

import "time"

func Default() *Config {
	return &Config{
		ActiveTheme: "dark",
		Refresh:     2 * time.Second,
		Accounts:    map[string]Account{},
		// 0 means "keep forever" (documented in docs/USAGE.md and on the
		// struct field), so the 90-day retention has to be seeded here rather
		// than inferred at startup — otherwise an explicit `0` and an absent
		// key are indistinguishable. Load() starts from Default() and lets the
		// file overwrite it, so a config that says 0 really does mean forever.
		AuditRetentionDays: 90,
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
			SSH: Tool{
				Exec:        "ssh",
				Args:        []string{"root@{{.IP}}"},
				Mode:        ModeTUI,
				AutoInstall: false,
			},
		},
	}
}
