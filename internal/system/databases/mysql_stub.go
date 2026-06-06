//go:build !mysql

package databases

import "context"

func connectMySQL(_ context.Context, inst Instance, _ string) (DB, error) {
	return nil, ErrDriverNotCompiled{Driver: DriverMySQL}
}

func mysqlCompiled() bool { return false }
