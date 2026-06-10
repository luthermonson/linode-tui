package views

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/linode/linodego/v2"

	"github.com/linode/tui/linode"
	"github.com/linode/tui/tools"
)

func init() {
	Register("databases", []string{"db", "dbs", "dbaas"}, newDatabases)
}

func newDatabases(d Deps) View {
	return newListView(listOpts[linodego.Database]{
		Deps:  d,
		Title: "Databases",
		Columns: []table.Column{
			{Title: "ID", Width: 10},
			{Title: "LABEL", Width: 28},
			{Title: "ENGINE", Width: 12},
			{Title: "VERSION", Width: 10},
			{Title: "REGION", Width: 14},
			{Title: "STATUS", Width: 12},
			{Title: "NODES", Width: 6},
			{Title: "PORT", Width: 6},
			{Title: "HOST", Width: 40},
		},
		Lister: func(ctx context.Context, c *linode.Client) ([]linodego.Database, error) {
			return c.Raw().ListDatabases(ctx, nil)
		},
		Rower: func(db linodego.Database) table.Row {
			return table.Row{
				strconv.Itoa(db.ID),
				db.Label,
				db.Engine,
				db.Version,
				db.Region,
				string(db.Status),
				strconv.Itoa(db.ClusterSize),
				strconv.Itoa(db.Port),
				db.Hosts.Primary,
			}
		},
		Matcher: func(db linodego.Database, needle string) bool {
			return containsAny(needle, db.Label, db.Engine, db.Region, string(db.Status), db.Hosts.Primary)
		},
		IDFn:         func(db linodego.Database) string { return strconv.Itoa(db.ID) },
		BookmarkKind: "databases",
		OnEnter: openDatabase,
		Actions: []Action[linodego.Database]{
			{
				Key:    "d",
				Label:  "delete",
				Prompt: func(db linodego.Database) string { return fmt.Sprintf("DELETE %s database %s (id %d)?", db.Engine, db.Label, db.ID) },
				Run: func(ctx context.Context, c *linode.Client, db linodego.Database) error {
					switch {
					case strings.HasPrefix(db.Engine, "mysql"):
						return c.Raw().DeleteMySQLDatabase(ctx, db.ID)
					case strings.HasPrefix(db.Engine, "postgres"):
						return c.Raw().DeletePostgresDatabase(ctx, db.ID)
					default:
						return fmt.Errorf("unsupported engine %q", db.Engine)
					}
				},
			},
		},
	})
}

func openDatabase(db linodego.Database, d Deps) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var (
			user, pass string
			kind       tools.Kind
			scheme     string
			err        error
		)
		switch {
		case strings.HasPrefix(db.Engine, "postgres"):
			creds, e := d.Linode.Raw().GetPostgresDatabaseCredentials(ctx, db.ID)
			err = e
			if creds != nil {
				user, pass = creds.Username, creds.Password
			}
			kind = tools.KindPostgreSQL
			scheme = "postgres"
		case strings.HasPrefix(db.Engine, "mysql"):
			creds, e := d.Linode.Raw().GetMySQLDatabaseCredentials(ctx, db.ID)
			err = e
			if creds != nil {
				user, pass = creds.Username, creds.Password
			}
			kind = tools.KindMySQL
			scheme = "mysql"
		default:
			return ErrorMsg{Err: fmt.Errorf("unsupported db engine: %q", db.Engine)}
		}
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("fetch credentials for %q: %w", db.Label, err)}
		}
		if db.Hosts.Primary == "" {
			return ErrorMsg{Err: fmt.Errorf("%q has no primary host yet (status=%s)", db.Label, db.Status)}
		}

		host := db.Hosts.Primary
		port := db.Port
		if port == 0 {
			if kind == tools.KindMySQL {
				port = 3306
			} else {
				port = 5432
			}
		}
		dsn := fmt.Sprintf("%s://%s:%s@%s:%d/",
			scheme,
			url.QueryEscape(user),
			url.QueryEscape(pass),
			host,
			port,
		)
		return DrillInMsg{
			Tool: kind,
			Vars: struct{ DSN string }{DSN: dsn},
		}
	}
}
