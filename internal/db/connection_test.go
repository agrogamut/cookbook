package db

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestMigrationConnectionPreservesSessionLocks(t *testing.T) {
	for _, tc := range []struct {
		name, url, host string
		port            uint16
	}{
		{"hosted transaction pooler", "postgres://postgres.project:example@aws-0-region.pooler.supabase.com:6543/postgres?sslmode=require", "aws-0-region.pooler.supabase.com", 5432},
		{"hosted session pooler", "postgres://postgres.project:example@aws-0-region.pooler.supabase.com:5432/postgres?sslmode=require", "aws-0-region.pooler.supabase.com", 5432},
		{"local database", "postgres://test:example@127.0.0.1:55434/test?sslmode=disable", "127.0.0.1", 55434},
		{"other server", "postgres://test:example@pooler.example.com:6543/test", "pooler.example.com", 6543},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, err := migrationConfig(tc.url)
			if err != nil {
				t.Fatal(err)
			}
			if config.Host != tc.host || config.Port != tc.port || config.Password != "example" {
				t.Fatal("migration connection changed unexpected fields")
			}
			if config.DefaultQueryExecMode != pgx.QueryExecModeSimpleProtocol {
				t.Fatal("migration connection uses prepared statements")
			}
			for _, fallback := range config.Fallbacks {
				if fallback.Port != tc.port {
					t.Fatal("fallback connection has a different port")
				}
			}
		})
	}
}
