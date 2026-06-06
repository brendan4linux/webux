//go:build !postgres

package databases

import "context"

func connectPostgres(_ context.Context, inst Instance, _ string) (DB, error) {
	return nil, ErrDriverNotCompiled{Driver: DriverPostgres}
}

func postgresCompiled() bool { return false }
