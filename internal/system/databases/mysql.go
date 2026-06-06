//go:build mysql

package databases

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

type mysqlDB struct {
	db *sql.DB
}

func connectMySQL(ctx context.Context, inst Instance, password string) (DB, error) {
	var dsn string
	if inst.Socket != "" {
		dsn = fmt.Sprintf("root:%s@unix(%s)/", password, inst.Socket)
	} else {
		dsn = fmt.Sprintf("root:%s@tcp(%s:%d)/", password, inst.Host, inst.Port)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("mysql connect: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("mysql ping: %w", err)
	}
	return &mysqlDB{db: db}, nil
}

func (m *mysqlDB) Driver() Driver { return DriverMySQL }
func (m *mysqlDB) Close() error   { return m.db.Close() }

func (m *mysqlDB) ListDatabases(ctx context.Context) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, "SHOW DATABASES")
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

func (m *mysqlDB) ListTables(ctx context.Context, database string) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, "SHOW TABLES IN `"+database+"`")
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

func (m *mysqlDB) Query(ctx context.Context, database, query string) (*QueryResult, error) {
	// Safety: only allow SELECT/SHOW/DESCRIBE/EXPLAIN
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	for _, allowed := range []string{"SELECT", "SHOW", "DESCRIBE", "DESC", "EXPLAIN"} {
		if strings.HasPrefix(trimmed, allowed) {
			goto allowed
		}
	}
	return &QueryResult{Error: "only SELECT, SHOW, DESCRIBE and EXPLAIN are permitted"}, nil

allowed:
	if database != "" {
		if _, err := m.db.ExecContext(ctx, "USE `"+database+"`"); err != nil {
			return nil, err
		}
	}
	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return &QueryResult{Error: err.Error()}, nil
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	result := &QueryResult{Columns: cols}

	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		// Convert []byte to string for JSON serialisation
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		result.Rows = append(result.Rows, vals)
	}
	result.RowCount = len(result.Rows)
	return result, nil
}
