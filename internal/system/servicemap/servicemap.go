// Package servicemap discovers listening services, their metadata, and the
// connections they make — building a topology map of what talks to what.
// Everything is read from /proc; no external tools are required.
package servicemap

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// ServiceType is a normalised service category label.
type ServiceType string

const (
	TypeNginx      ServiceType = "nginx"
	TypeApache     ServiceType = "apache"
	TypeCaddy      ServiceType = "caddy"
	TypeHAProxy    ServiceType = "haproxy"
	TypeMySQL      ServiceType = "mysql"
	TypePostgres   ServiceType = "postgresql"
	TypeRedis      ServiceType = "redis"
	TypeMongoDB    ServiceType = "mongodb"
	TypeRabbitMQ   ServiceType = "rabbitmq"
	TypeElastic    ServiceType = "elasticsearch"
	TypeMemcached  ServiceType = "memcached"
	TypeJavaApp    ServiceType = "java-app"
	TypeNodeApp    ServiceType = "node-app"
	TypePythonApp  ServiceType = "python-app"
	TypeRubyApp    ServiceType = "ruby-app"
	TypeGoApp      ServiceType = "go-app"
	TypeSSH        ServiceType = "ssh"
	TypeDNS        ServiceType = "dns"
	TypeSMTP       ServiceType = "smtp"
	TypeEtcd       ServiceType = "etcd"
	TypeDocker     ServiceType = "docker"
	TypeKubernetes ServiceType = "kubernetes"
	TypeVarnish    ServiceType = "varnish"
	TypeGeneric    ServiceType = "generic"
)

// Service is a process listening on at least one port, with enriched metadata.
type Service struct {
	PID         int               `json:"pid"`
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	ServiceType ServiceType       `json:"service_type"`
	Exe         string            `json:"exe"`
	HomeDir     string            `json:"home_dir"`
	User        string            `json:"user"`
	Protocol    string            `json:"protocol"`
	Port        int               `json:"port"`
	Address     string            `json:"address"`
	ConfigPaths []string          `json:"config_paths"`
	EnvVars     map[string]string `json:"env_vars,omitempty"`
	Outbound    []OutboundConn    `json:"outbound"`
	ClientCount int               `json:"client_count"`
}

// OutboundConn is a connection this service is making to a remote endpoint.
type OutboundConn struct {
	DstIP       string `json:"dst_ip"`
	DstPort     int    `json:"dst_port"`
	Protocol    string `json:"protocol"`
	ServiceHint string `json:"service_hint"`
	State       string `json:"state"`
}

// Snapshot is a point-in-time capture of all services and connections.
type Snapshot struct {
	Services    []Service `json:"services"`
	CapturedAt  time.Time `json:"captured_at"`
	ConntrackOK bool      `json:"conntrack_ok"`
}

// ── Entry point ───────────────────────────────────────────────────────────────

// Capture takes a full point-in-time snapshot.
func Capture() (*Snapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build inode→pid map once; used for both listening and established lookups.
	inodePID, _ := buildInodePIDMap(ctx)

	// All TCP/UDP entries from /proc/net/*
	all := readAllSockets()

	// Separate listeners from established connections
	listeners := map[string]socketEntry{} // key = "proto:port"
	established := []socketEntry{}
	for _, e := range all {
		if e.state == "0A" /* LISTEN */ || (e.state == "07" && (e.proto == "udp" || e.proto == "udp6")) {
			listeners[fmt.Sprintf("%s:%d", e.proto, e.localPort)] = e
		} else if e.state == "01" /* ESTABLISHED */ {
			established = append(established, e)
		}
	}

	// Assign PIDs to all sockets
	for key, e := range listeners {
		if pid, ok := inodePID[e.inode]; ok {
			e.pid = pid
			listeners[key] = e
		}
	}
	for i, e := range established {
		if pid, ok := inodePID[e.inode]; ok {
			established[i].pid = pid
		}
	}

	// Deduplicate listeners by PID — one service can listen on multiple protos.
	// Primary key is pid:port (prefer tcp4 > tcp6 > udp).
	type svcKey struct{ pid, port int }
	seen := map[svcKey]bool{}
	var svcs []Service

	for _, e := range listeners {
		if e.pid == 0 {
			continue
		}
		k := svcKey{e.pid, int(e.localPort)}
		if seen[k] {
			continue
		}
		seen[k] = true

		svc := buildService(ctx, e)

		// Count inbound clients and collect outbound connections.
		remoteIPs := map[string]bool{}
		outboundSeen := map[string]bool{}

		for _, conn := range established {
			if conn.pid != e.pid {
				continue
			}
			// Inbound: local port == listening port
			if conn.localPort == e.localPort {
				remoteIPs[conn.remoteIP] = true
			} else {
				// Outbound: local port is ephemeral, remote is the actual service
				obKey := fmt.Sprintf("%s:%d", conn.remoteIP, conn.remotePort)
				if !outboundSeen[obKey] {
					outboundSeen[obKey] = true
					svc.Outbound = append(svc.Outbound, OutboundConn{
						DstIP:       conn.remoteIP,
						DstPort:     int(conn.remotePort),
						Protocol:    normaliseProto(conn.proto),
						ServiceHint: portServiceHint(int(conn.remotePort)),
						State:       "ESTABLISHED",
					})
				}
			}
		}
		svc.ClientCount = len(remoteIPs)
		svcs = append(svcs, svc)
	}

	_, conntrackOK := readConntrack()

	return &Snapshot{
		Services:    svcs,
		CapturedAt:  time.Now().UTC(),
		ConntrackOK: conntrackOK,
	}, nil
}

// ── Service builder ───────────────────────────────────────────────────────────

func buildService(ctx context.Context, e socketEntry) Service {
	svc := Service{
		PID:      e.pid,
		Protocol: normaliseProto(e.proto),
		Port:     int(e.localPort),
		Address:  e.localIP,
		Outbound: []OutboundConn{},
	}

	// Process name
	svc.Name = readComm(e.pid)

	// Executable path
	if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", e.pid)); err == nil {
		svc.Exe = exe
	}

	// Owner (UID → username)
	svc.User = readProcUser(e.pid)

	// Working directory (useful for apps started from their home dir)
	if cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", e.pid)); err == nil {
		svc.HomeDir = cwd
	}

	// Detect service type
	cmdline := readCmdline(e.pid)
	svc.ServiceType = detectServiceType(svc.Name, cmdline, int(e.localPort), svc.Exe)
	svc.DisplayName = displayName(svc.ServiceType, svc.Name)

	// Config paths based on service type
	svc.ConfigPaths = knownConfigs(svc.ServiceType)

	// Better home dir based on service type
	if home := serviceHomeDir(svc.ServiceType, svc.Exe, cmdline); home != "" {
		svc.HomeDir = home
	}

	// Environment vars — read with timeout, extract useful keys only
	select {
	case <-ctx.Done():
	default:
		svc.EnvVars = readUsefulEnv(e.pid)
	}

	return svc
}

// ── Service detection ─────────────────────────────────────────────────────────

func detectServiceType(name, cmdline string, port int, exe string) ServiceType {
	n := strings.ToLower(name)
	c := strings.ToLower(cmdline)
	e := strings.ToLower(exe)

	switch {
	case n == "nginx" || strings.Contains(e, "/nginx"):
		return TypeNginx
	case n == "apache2" || n == "httpd" || strings.Contains(e, "/httpd") || strings.Contains(e, "/apache2"):
		return TypeApache
	case n == "caddy":
		return TypeCaddy
	case n == "haproxy":
		return TypeHAProxy
	case n == "mysqld" || n == "mysqld_safe" || n == "mariadbd" || strings.Contains(e, "mysqld"):
		return TypeMySQL
	case n == "postgres" || strings.Contains(e, "/postgres"):
		return TypePostgres
	case n == "redis-server" || strings.Contains(e, "redis-server"):
		return TypeRedis
	case n == "mongod":
		return TypeMongoDB
	case n == "beam.smp" || strings.Contains(c, "rabbitmq"):
		return TypeRabbitMQ
	case strings.Contains(e, "elasticsearch") || strings.Contains(c, "elasticsearch"):
		return TypeElastic
	case n == "memcached":
		return TypeMemcached
	case n == "varnishd":
		return TypeVarnish
	case n == "sshd":
		return TypeSSH
	case n == "named" || n == "unbound" || n == "coredns" || port == 53:
		return TypeDNS
	case n == "postfix" || n == "sendmail" || n == "dovecot" || n == "exim":
		return TypeSMTP
	case n == "etcd":
		return TypeEtcd
	case n == "dockerd" || n == "docker-proxy":
		return TypeDocker
	case n == "kube-apiserver" || n == "kubelet":
		return TypeKubernetes
	case n == "java" || strings.Contains(e, "/java"):
		return TypeJavaApp
	case n == "node" || n == "nodejs" || strings.Contains(e, "/node"):
		return TypeNodeApp
	case n == "python" || n == "python3" || n == "gunicorn" || n == "uvicorn" || n == "uwsgi":
		return TypePythonApp
	case n == "ruby" || n == "puma" || n == "unicorn" || n == "passenger":
		return TypeRubyApp
	}
	return TypeGeneric
}

func displayName(t ServiceType, name string) string {
	m := map[ServiceType]string{
		TypeNginx:      "Nginx",
		TypeApache:     "Apache",
		TypeCaddy:      "Caddy",
		TypeHAProxy:    "HAProxy",
		TypeMySQL:      "MySQL / MariaDB",
		TypePostgres:   "PostgreSQL",
		TypeRedis:      "Redis",
		TypeMongoDB:    "MongoDB",
		TypeRabbitMQ:   "RabbitMQ",
		TypeElastic:    "Elasticsearch",
		TypeMemcached:  "Memcached",
		TypeVarnish:    "Varnish",
		TypeSSH:        "SSH",
		TypeDNS:        "DNS",
		TypeSMTP:       "Mail (SMTP/IMAP)",
		TypeEtcd:       "etcd",
		TypeDocker:     "Docker",
		TypeKubernetes: "Kubernetes",
		TypeJavaApp:    "Java Application",
		TypeNodeApp:    "Node.js Application",
		TypePythonApp:  "Python Application",
		TypeRubyApp:    "Ruby Application",
		TypeGoApp:      "Go Application",
	}
	if d, ok := m[t]; ok {
		return d
	}
	return name
}

func knownConfigs(t ServiceType) []string {
	m := map[ServiceType][]string{
		TypeNginx:    {"/etc/nginx/nginx.conf", "/etc/nginx/"},
		TypeApache:   {"/etc/apache2/", "/etc/httpd/"},
		TypeCaddy:    {"/etc/caddy/Caddyfile", "/etc/caddy/"},
		TypeHAProxy:  {"/etc/haproxy/haproxy.cfg"},
		TypeMySQL:    {"/etc/mysql/my.cnf", "/etc/mysql/", "/etc/my.cnf"},
		TypePostgres: {"/etc/postgresql/"},
		TypeRedis:    {"/etc/redis/redis.conf", "/etc/redis/"},
		TypeMongoDB:  {"/etc/mongod.conf"},
		TypeRabbitMQ: {"/etc/rabbitmq/"},
		TypeElastic:  {"/etc/elasticsearch/elasticsearch.yml"},
		TypeVarnish:  {"/etc/varnish/"},
		TypeSSH:      {"/etc/ssh/sshd_config"},
		TypeDNS:      {"/etc/named.conf", "/etc/unbound/unbound.conf"},
		TypeEtcd:     {"/etc/etcd/"},
	}
	if paths, ok := m[t]; ok {
		return paths
	}
	return []string{}
}

func serviceHomeDir(t ServiceType, exe, cmdline string) string {
	switch t {
	case TypeMySQL:
		return "/var/lib/mysql"
	case TypePostgres:
		return "/var/lib/postgresql"
	case TypeRedis:
		return "/var/lib/redis"
	case TypeMongoDB:
		return "/var/lib/mongodb"
	case TypeElastic:
		return "/var/lib/elasticsearch"
	case TypeNginx:
		return "/var/www"
	case TypeApache:
		for _, p := range []string{"/var/www/html", "/var/www"} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	case TypeJavaApp:
		// Check for CATALINA_HOME, common Tomcat installs
		for _, candidate := range []string{"/opt/tomcat", "/usr/share/tomcat", "/opt/app"} {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return ""
}

// ── Port service hints ────────────────────────────────────────────────────────

func portServiceHint(port int) string {
	hints := map[int]string{
		21: "FTP", 22: "SSH", 23: "Telnet", 25: "SMTP",
		53: "DNS", 80: "HTTP", 110: "POP3", 143: "IMAP",
		389: "LDAP", 443: "HTTPS", 465: "SMTPS", 514: "Syslog",
		587: "SMTP/TLS", 636: "LDAPS", 993: "IMAPS", 995: "POP3S",
		1433: "MSSQL", 1521: "Oracle DB", 2181: "ZooKeeper",
		2375: "Docker", 2376: "Docker TLS", 2379: "etcd", 2380: "etcd peer",
		3000: "HTTP-alt", 3306: "MySQL", 3307: "MySQL-alt",
		4567: "App Server", 5432: "PostgreSQL", 5601: "Kibana",
		5671: "AMQP/TLS", 5672: "AMQP (RabbitMQ)", 6379: "Redis",
		6443: "K8s API", 7473: "Neo4j HTTPS", 7474: "Neo4j HTTP",
		8080: "HTTP-alt", 8443: "HTTPS-alt", 8983: "Solr",
		9000: "HTTP-alt", 9042: "Cassandra", 9092: "Kafka",
		9200: "Elasticsearch HTTP", 9300: "Elasticsearch cluster",
		11211: "Memcached", 15672: "RabbitMQ mgmt", 27017: "MongoDB",
		28017: "MongoDB HTTP",
	}
	if h, ok := hints[port]; ok {
		return h
	}
	return ""
}

// ── Proc helpers ──────────────────────────────────────────────────────────────

func readComm(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readCmdline(pid int) string {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return ""
	}
	for i, c := range b {
		if c == 0 {
			b[i] = ' '
		}
	}
	return strings.TrimSpace(string(b))
}

func readProcUser(pid int) string {
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	f, err := os.Open(statusPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				uid := fields[1]
				if u := uidToName(uid); u != "" {
					return u
				}
				return "uid:" + uid
			}
		}
	}
	return ""
}

func uidToName(uid string) string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), ":", 4)
		if len(parts) >= 3 && parts[2] == uid {
			return parts[0]
		}
	}
	return ""
}

// interestingEnvKeys are the env var keys worth surfacing in the UI.
var interestingEnvKeys = map[string]bool{
	"JAVA_HOME": true, "CATALINA_HOME": true, "CATALINA_BASE": true,
	"NODE_ENV": true, "NODE_PATH": true,
	"RAILS_ENV": true, "RACK_ENV": true, "GEM_HOME": true,
	"VIRTUAL_ENV": true, "PYTHONPATH": true,
	"APP_HOME": true, "APP_ROOT": true, "APP_ENV": true,
	"DATABASE_URL": true, "DB_HOST": true, "DB_PORT": true, "DB_NAME": true,
	"REDIS_URL": true, "MONGODB_URI": true, "MONGO_URL": true,
	"SPRING_PROFILES_ACTIVE": true, "SPRING_CONFIG_LOCATION": true,
}

func readUsefulEnv(pid int) map[string]string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, kv := range strings.Split(string(data), "\x00") {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		key := kv[:idx]
		val := kv[idx+1:]
		if !interestingEnvKeys[key] {
			continue
		}
		// Partially redact secrets in URLs
		if strings.Contains(strings.ToLower(key), "url") || strings.Contains(strings.ToLower(key), "uri") {
			val = redactURL(val)
		}
		out[key] = val
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func redactURL(raw string) string {
	// Replace password in scheme://user:pass@host with ***
	if idx := strings.Index(raw, "@"); idx > 0 {
		prefix := raw[:idx]
		suffix := raw[idx:]
		if colonIdx := strings.LastIndex(prefix, ":"); colonIdx > 0 {
			schemeEnd := strings.Index(prefix, "://")
			if schemeEnd >= 0 && colonIdx > schemeEnd+3 {
				return prefix[:colonIdx] + ":***" + suffix
			}
		}
	}
	return raw
}

// ── /proc/net socket parsing ──────────────────────────────────────────────────

type socketEntry struct {
	proto      string
	localIP    string
	localPort  uint16
	remoteIP   string
	remotePort uint16
	state      string // hex state code e.g. "01" = ESTABLISHED
	inode      uint64
	pid        int
}

func readAllSockets() []socketEntry {
	var all []socketEntry
	for _, f := range []struct{ path, proto string }{
		{"/proc/net/tcp", "tcp"}, {"/proc/net/tcp6", "tcp6"},
		{"/proc/net/udp", "udp"}, {"/proc/net/udp6", "udp6"},
	} {
		entries, err := parseProcNetFile(f.path, f.proto)
		if err == nil {
			all = append(all, entries...)
		}
	}
	return all
}

func parseProcNetFile(path, proto string) ([]socketEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	ipv6 := strings.HasSuffix(proto, "6")
	var out []socketEntry
	sc := bufio.NewScanner(file)
	sc.Scan() // skip header

	for sc.Scan() {
		fields := strings.Fields(strings.TrimSpace(sc.Text()))
		if len(fields) < 10 {
			continue
		}
		localIP, localPort, err := parseHexAddr(fields[1], ipv6)
		if err != nil {
			continue
		}
		remoteIP, remotePort, err := parseHexAddr(fields[2], ipv6)
		if err != nil {
			continue
		}
		stateHex := strings.ToUpper(fields[3])
		inode, _ := strconv.ParseUint(fields[9], 10, 64)

		out = append(out, socketEntry{
			proto:      proto,
			localIP:    localIP,
			localPort:  localPort,
			remoteIP:   remoteIP,
			remotePort: remotePort,
			state:      stateHex,
			inode:      inode,
		})
	}
	return out, sc.Err()
}

func parseHexAddr(hexAddr string, ipv6 bool) (ip string, port uint16, err error) {
	parts := strings.SplitN(hexAddr, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid addr: %q", hexAddr)
	}
	portVal, err := strconv.ParseUint(parts[1], 16, 16)
	if err != nil {
		return "", 0, err
	}
	port = uint16(portVal)

	rawIP, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", 0, err
	}
	if ipv6 {
		if len(rawIP) != 16 {
			return "", 0, fmt.Errorf("bad ipv6 len")
		}
		for i := 0; i < 16; i += 4 {
			rawIP[i], rawIP[i+3] = rawIP[i+3], rawIP[i]
			rawIP[i+1], rawIP[i+2] = rawIP[i+2], rawIP[i+1]
		}
		ip = net.IP(rawIP).String()
	} else {
		if len(rawIP) != 4 {
			return "", 0, fmt.Errorf("bad ipv4 len")
		}
		ip = fmt.Sprintf("%d.%d.%d.%d", rawIP[3], rawIP[2], rawIP[1], rawIP[0])
	}
	return ip, port, nil
}

func normaliseProto(p string) string {
	if strings.HasPrefix(p, "tcp") {
		return "tcp"
	}
	return "udp"
}

// ── Inode → PID map (reused from ports scanner pattern) ──────────────────────

func buildInodePIDMap(ctx context.Context) (map[uint64]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	type result struct {
		inode uint64
		pid   int
	}
	results := make(chan result, 1024)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 32)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		wg.Add(1)
		go func(pid int, name string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			fdDir := filepath.Join("/proc", name, "fd")
			fds, err := os.ReadDir(fdDir)
			if err != nil {
				return
			}
			for _, fd := range fds {
				select {
				case <-ctx.Done():
					return
				default:
				}
				link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
				if err != nil {
					continue
				}
				if !strings.HasPrefix(link, "socket:[") {
					continue
				}
				inodeStr := strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")
				inode, err := strconv.ParseUint(inodeStr, 10, 64)
				if err != nil {
					continue
				}
				select {
				case results <- result{inode: inode, pid: pid}:
				case <-ctx.Done():
					return
				}
			}
		}(pid, e.Name())
	}

	go func() { wg.Wait(); close(results) }()

	m := make(map[uint64]int)
	for r := range results {
		m[r.inode] = r.pid
	}
	return m, nil
}
