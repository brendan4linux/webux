// Package databases detects and connects to running database instances.
// Detection is zero-dependency (cross-references /proc open ports).
// Driver support is opt-in via build tags: -tags mysql -tags postgres
package databases

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Driver identifies the database engine.
type Driver string

const (
	DriverMySQL      Driver = "mysql"
	DriverPostgres   Driver = "postgres"
	DriverRedis      Driver = "redis"
	DriverMongoDB    Driver = "mongodb"
	DriverSQLite     Driver = "sqlite"
)

// Instance is a detected database instance on this host.
type Instance struct {
	Driver   Driver `json:"driver"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DataDir  string `json:"data_dir"`
	Version  string `json:"version"`
	Socket   string `json:"socket"`     // unix socket path if applicable
	Running  bool   `json:"running"`
	CanQuery bool   `json:"can_query"`  // driver compiled in and connectable
}

// QueryResult is the result of a read-only SQL query.
type QueryResult struct {
	Columns []string        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
	RowCount int            `json:"row_count"`
	Error   string          `json:"error,omitempty"`
}

// DB is a live database connection.
type DB interface {
	Driver() Driver
	ListDatabases(ctx context.Context) ([]string, error)
	ListTables(ctx context.Context, database string) ([]string, error)
	Query(ctx context.Context, database, sql string) (*QueryResult, error)
	Close() error
}

// Connect opens a connection to a database instance.
// Returns ErrDriverNotCompiled if the relevant build tag wasn't set.
func Connect(ctx context.Context, inst Instance, password string) (DB, error) {
	switch inst.Driver {
	case DriverMySQL:
		return connectMySQL(ctx, inst, password)
	case DriverPostgres:
		return connectPostgres(ctx, inst, password)
	default:
		return nil, fmt.Errorf("driver %q not supported", inst.Driver)
	}
}

// ErrDriverNotCompiled is returned when a driver isn't compiled in.
type ErrDriverNotCompiled struct{ Driver Driver }

func (e ErrDriverNotCompiled) Error() string {
	return fmt.Sprintf("%s driver not compiled — rebuild with -tags %s", e.Driver, e.Driver)
}

// Detect finds running database instances by probing well-known ports
// and unix sockets. Pure /proc inspection — no external tools needed.
func Detect() []Instance {
	var instances []Instance

	// Known port → driver mappings
	knownPorts := map[int]Driver{
		3306:  DriverMySQL,
		5432:  DriverPostgres,
		6379:  DriverRedis,
		27017: DriverMongoDB,
	}

	// Check which ports are in use via /proc/net/tcp
	openPorts := openTCPPorts()

	for port, driver := range knownPorts {
		if !openPorts[port] {
			continue
		}
		inst := Instance{
			Driver:  driver,
			Host:    "127.0.0.1",
			Port:    port,
			Running: true,
			DataDir: defaultDataDir(driver),
			Version: detectVersion(driver),
		}
		instances = append(instances, inst)
	}

	// Also check well-known unix sockets
	sockets := map[string]Driver{
		"/var/run/mysqld/mysqld.sock": DriverMySQL,
		"/tmp/mysql.sock":             DriverMySQL,
		"/var/run/postgresql/.s.PGSQL.5432": DriverPostgres,
	}
	seen := make(map[Driver]bool)
	for _, inst := range instances {
		seen[inst.Driver] = true
	}
	for socket, driver := range sockets {
		if seen[driver] {
			continue
		}
		if _, err := os.Stat(socket); err == nil {
			instances = append(instances, Instance{
				Driver:  driver,
				Socket:  socket,
				Running: true,
				DataDir: defaultDataDir(driver),
				Version: detectVersion(driver),
			})
		}
	}

	// Mark which drivers are queryable (compiled in)
	for i := range instances {
		instances[i].CanQuery = driverCompiled(instances[i].Driver)
	}

	return instances
}

func openTCPPorts() map[int]bool {
	m := make(map[int]bool)
	// Read /proc/net/tcp for listening ports (state 0A = LISTEN)
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n")[1:] {
			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}
			if strings.ToUpper(fields[3]) != "0A" {
				continue // not LISTEN
			}
			// local_address is "hex_ip:hex_port"
			addrParts := strings.Split(fields[1], ":")
			if len(addrParts) < 2 {
				continue
			}
			var port int
			fmt.Sscanf(addrParts[len(addrParts)-1], "%x", &port)
			m[port] = true
		}
	}
	return m
}

func defaultDataDir(d Driver) string {
	switch d {
	case DriverMySQL:
		return "/var/lib/mysql"
	case DriverPostgres:
		return "/var/lib/postgresql"
	case DriverMongoDB:
		return "/var/lib/mongodb"
	case DriverRedis:
		return "/var/lib/redis"
	default:
		return ""
	}
}

func driverCompiled(d Driver) bool {
	switch d {
	case DriverMySQL:
		return mysqlCompiled()
	case DriverPostgres:
		return postgresCompiled()
	default:
		return false
	}
}

func detectVersion(d Driver) string {
	cmds := map[Driver][]string{
		DriverMySQL:    {"mysql", "--version"},
		DriverPostgres: {"psql", "--version"},
		DriverRedis:    {"redis-server", "--version"},
		DriverMongoDB:  {"mongod", "--version"},
	}
	args, ok := cmds[d]
	if !ok {
		return "unknown"
	}
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return "unknown"
	}
	// Return first line, trimmed
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if len(line) > 60 {
		line = line[:60]
	}
	return line
}
