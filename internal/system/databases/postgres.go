//go:build postgres

package databases

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type postgresDB struct {
	conn *pgx.Conn
}

func postgresCompiled() bool { return true }

func connectPostgres(ctx context.Context, inst Instance, password string) (DB, error) {
	var connStr string
	if inst.Socket != "" {
		connStr = fmt.Sprintf("host=%s user=postgres password=%s sslmode=disable", inst.Socket, password)
	} else {
		connStr = fmt.Sprintf("host=%s port=%d user=postgres password=%s sslmode=disable",
			inst.Host, inst.Port, password)
	}
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}
	return &postgresDB{conn: conn}, nil
}

func (p *postgresDB) Driver() Driver { return DriverPostgres }
func (p *postgresDB) Close() error   { return p.conn.Close(context.Background()) }

func (p *postgresDB) ListDatabases(ctx context.Context) ([]string, error) {
	rows, err := p.conn.Query(ctx,
		"SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dbs []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			dbs = append(dbs, name)
		}
	}
	return dbs, nil
}

func (p *postgresDB) ListTables(ctx context.Context, database string) ([]string, error) {
	rows, err := p.conn.Query(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_schema='public' ORDER BY table_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			tables = append(tables, name)
		}
	}
	return tables, nil
}

func (p *postgresDB) Query(ctx context.Context, _ string, query string) (*QueryResult, error) {
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	for _, allowed := range []string{"SELECT", "SHOW", "EXPLAIN", "\\D"} {
		if strings.HasPrefix(trimmed, allowed) {
			goto allowed
		}
	}
	return &QueryResult{Error: "only SELECT, SHOW and EXPLAIN are permitted"}, nil

allowed:
	rows, err := p.conn.Query(ctx, query)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	defer rows.Close()

	var cols []string
	for _, fd := range rows.FieldDescriptions() {
		cols = append(cols, string(fd.Name))
	}
	result := &QueryResult{Columns: cols}

	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		result.Rows = append(result.Rows, vals)
	}
	result.RowCount = len(result.Rows)
	return result, nil
}
