package svccheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// RunMySQL runs OS-level security checks for MySQL/MariaDB.
// No database credentials are required.
func RunMySQL() []Check {
	cfg := loadMySQLConfig()
	return []Check{
		checkMySQLNotRoot(),
		checkMySQLBindAddress(cfg),
		checkMySQLLocalInfile(cfg),
		checkMySQLDataDirPerms(),
		checkMySQLErrorLog(cfg),
	}
}

func loadMySQLConfig() string {
	candidates := []string{
		"/etc/mysql/mysql.conf.d/mysqld.cnf",
		"/etc/mysql/mariadb.conf.d/50-server.cnf",
		"/etc/mysql/my.cnf",
		"/etc/my.cnf",
		"/etc/my.cnf.d/server.cnf",
		"/etc/mariadb/my.cnf",
	}
	var content strings.Builder
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			content.WriteString(string(b))
			content.WriteByte('\n')
		}
	}
	// Also read conf.d directories
	for _, dir := range []string{"/etc/mysql/conf.d", "/etc/my.cnf.d", "/etc/mysql/mariadb.conf.d"} {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.cnf"))
		for _, f := range matches {
			if b, err := os.ReadFile(f); err == nil {
				content.WriteString(string(b))
				content.WriteByte('\n')
			}
		}
	}
	return content.String()
}

func checkMySQLNotRoot() Check {
	c := Check{
		ID:       "mysql_not_root",
		Label:    "MySQL/MariaDB not running as root",
		Severity: "high",
		Fix:      "Ensure MySQL/MariaDB runs as the `mysql` user. Check your systemd unit or init script.",
	}
	out, err := exec.Command("ps", "-eo", "user,comm").Output()
	if err != nil {
		c.Detail = "Could not inspect running processes"
		return c
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		comm := strings.ToLower(fields[1])
		if comm == "mysqld" || comm == "mariadbd" || comm == "mariadb" {
			user := fields[0]
			if user == "root" {
				c.Detail = "MySQL/MariaDB process (" + fields[1] + ") is running as root"
				return c
			}
			c.Pass = true
			c.Detail = "MySQL/MariaDB is running as user: " + user
			return c
		}
	}
	c.Pass = true
	c.Detail = "MySQL/MariaDB does not appear to be running"
	return c
}

func checkMySQLBindAddress(cfg string) Check {
	c := Check{
		ID:       "mysql_bind_address",
		Label:    "MySQL not listening on all interfaces (bind-address)",
		Severity: "high",
		Fix:      "Set `bind-address = 127.0.0.1` in your MySQL/MariaDB config to restrict network access",
	}
	if cfg == "" {
		c.Detail = "Could not read MySQL/MariaDB config"
		return c
	}
	lower := strings.ToLower(cfg)
	// If not set at all, mysqld defaults to listening on all interfaces in older versions
	bindIdx := strings.Index(lower, "bind-address")
	if bindIdx == -1 {
		bindIdx = strings.Index(lower, "bind_address")
	}
	if bindIdx == -1 {
		c.Detail = "bind-address not set in config — MySQL may be listening on all interfaces (default varies by version)"
		return c
	}
	// Find the value
	line := cfg[bindIdx:]
	if nl := strings.IndexByte(line, '\n'); nl > 0 {
		line = line[:nl]
	}
	if strings.Contains(line, "0.0.0.0") || strings.Contains(line, "::") {
		c.Detail = "bind-address is set to all interfaces (" + strings.TrimSpace(strings.SplitN(line, "=", 2)[1]) + ")"
		return c
	}
	c.Pass = true
	c.Detail = "bind-address is restricted: " + strings.TrimSpace(line)
	return c
}

func checkMySQLLocalInfile(cfg string) Check {
	c := Check{
		ID:       "mysql_local_infile",
		Label:    "LOAD DATA LOCAL INFILE disabled",
		Severity: "medium",
		Fix:      "Set `local-infile=0` in the [mysqld] section of your MySQL/MariaDB config",
	}
	if cfg == "" {
		c.Detail = "Could not read MySQL/MariaDB config"
		return c
	}
	lower := strings.ToLower(cfg)
	if strings.Contains(lower, "local-infile=0") || strings.Contains(lower, "local_infile=0") ||
		strings.Contains(lower, "local-infile = 0") || strings.Contains(lower, "local_infile = 0") {
		c.Pass = true
		c.Detail = "local-infile is disabled"
	} else if strings.Contains(lower, "local-infile") || strings.Contains(lower, "local_infile") {
		c.Detail = "local-infile is set but not to 0 — LOAD DATA LOCAL INFILE may be enabled"
	} else {
		c.Detail = "local-infile not explicitly set — may be enabled by default in some versions"
	}
	return c
}

func checkMySQLDataDirPerms() Check {
	c := Check{
		ID:       "mysql_datadir_perms",
		Label:    "MySQL data directory has secure permissions",
		Severity: "medium",
		Fix:      "Set data directory permissions to 750 and ownership to the mysql user: `chmod 750 /var/lib/mysql && chown mysql:mysql /var/lib/mysql`",
	}
	candidates := []string{"/var/lib/mysql", "/var/lib/mariadb", "/data/mysql"}
	for _, dir := range candidates {
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}
		mode := info.Mode().Perm()
		stat, ok := info.Sys().(*syscall.Stat_t)
		ownerUID := uint32(0)
		if ok {
			ownerUID = stat.Uid
		}
		// Should not be world-readable or world-writable
		if mode&0o007 != 0 {
			c.Detail = dir + " is world-accessible (permissions: " + mode.String() + ")"
			return c
		}
		if ownerUID == 0 {
			c.Detail = dir + " is owned by root — should be owned by the mysql user"
			return c
		}
		c.Pass = true
		c.Detail = dir + " permissions: " + mode.String()
		return c
	}
	c.Pass = true
	c.Detail = "MySQL data directory not found at standard paths — may not be installed"
	return c
}

func checkMySQLErrorLog(cfg string) Check {
	c := Check{
		ID:       "mysql_error_log",
		Label:    "Error logging configured",
		Severity: "low",
		Fix:      "Set `log_error = /var/log/mysql/error.log` in your MySQL/MariaDB config",
	}
	if cfg == "" {
		c.Detail = "Could not read MySQL/MariaDB config"
		return c
	}
	lower := strings.ToLower(cfg)
	if strings.Contains(lower, "log_error") || strings.Contains(lower, "log-error") {
		c.Pass = true
		c.Detail = "Error logging is configured"
	} else {
		c.Detail = "log_error not configured — errors may not be persisted to disk"
	}
	return c
}
