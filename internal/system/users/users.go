// Package users manages Linux user and group accounts.
// Reads are done directly from /etc/passwd, /etc/shadow, /etc/group.
// Writes wrap useradd/usermod/userdel/groupadd/groupdel, emitting CLI echoes.
package users

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// User represents a Linux user account.
type User struct {
	Username    string   `json:"username"`
	UID         int      `json:"uid"`
	GID         int      `json:"gid"`
	GECOS       string   `json:"gecos"`       // Full name / comment field
	Home        string   `json:"home"`
	Shell       string   `json:"shell"`
	Groups      []string `json:"groups"`      // supplementary groups
	PrimaryGroup string  `json:"primary_group"`
	Locked      bool     `json:"locked"`      // true if shadow entry starts with !
	IsSystem    bool     `json:"is_system"`   // UID < 1000
	HasPassword bool     `json:"has_password"` // false if shadow is * or !
}

// Group represents a Linux group.
type Group struct {
	Name    string   `json:"name"`
	GID     int      `json:"gid"`
	Members []string `json:"members"`
}

// CreateUserOpts are the options for creating a new user.
type CreateUserOpts struct {
	Username string `json:"username"`
	Password string `json:"password"` // plaintext — hashed before passing to passwd
	FullName string `json:"full_name"`
	Home     string `json:"home"`     // empty = default /home/<username>
	Shell    string `json:"shell"`    // empty = /bin/bash
	Groups   []string `json:"groups"` // supplementary groups
	System   bool   `json:"system"`   // create as system user (UID < 1000)
	UID      int    `json:"uid"`      // 0 = auto-assign
}

// Manager handles user and group operations.
type Manager struct{}

// NewManager returns a Manager.
func NewManager() *Manager { return &Manager{} }

// ListUsers returns all users from /etc/passwd, enriched with group and shadow info.
func (m *Manager) ListUsers(includeSystem bool) ([]User, error) {
	passwd, err := parsePasswd()
	if err != nil {
		return nil, err
	}

	shadowMap := parseShadow() // best-effort; empty map if not root
	groupMap := parseGroupMembership()
	gidToName := parseGIDToName()

	var users []User
	for _, u := range passwd {
		if !includeSystem && u.IsSystem {
			continue
		}
		u.Groups = groupMap[u.Username]
		u.PrimaryGroup = gidToName[u.GID]

		if shadow, ok := shadowMap[u.Username]; ok {
			u.Locked = strings.HasPrefix(shadow, "!")
			u.HasPassword = shadow != "" && shadow != "*" && shadow != "!" && shadow != "!!"
		}

		users = append(users, u)
	}
	return users, nil
}

// ListGroups returns all groups from /etc/group.
func (m *Manager) ListGroups() ([]Group, error) {
	return parseGroups()
}

// GetUser returns a single user by username.
func (m *Manager) GetUser(username string) (*User, error) {
	users, err := m.ListUsers(true)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.Username == username {
			return &u, nil
		}
	}
	return nil, fmt.Errorf("user %q not found", username)
}

// CreateUser creates a new user. Returns the CLI equivalent.
func (m *Manager) CreateUser(opts CreateUserOpts) (string, error) {
	args := []string{}

	if opts.FullName != "" {
		args = append(args, "--comment", opts.FullName)
	}
	if opts.Home != "" {
		args = append(args, "--home-dir", opts.Home)
	} else {
		args = append(args, "--create-home")
	}
	if opts.Shell != "" {
		args = append(args, "--shell", opts.Shell)
	} else {
		args = append(args, "--shell", "/bin/bash")
	}
	if len(opts.Groups) > 0 {
		args = append(args, "--groups", strings.Join(opts.Groups, ","))
	}
	if opts.System {
		args = append(args, "--system")
	}
	if opts.UID > 0 {
		args = append(args, "--uid", strconv.Itoa(opts.UID))
	}
	args = append(args, opts.Username)

	cliCmd := "useradd " + strings.Join(args, " ")

	if err := exec.Command("useradd", args...).Run(); err != nil {
		return "", fmt.Errorf("useradd: %w", err)
	}

	// Set password if provided
	if opts.Password != "" {
		if err := setPassword(opts.Username, opts.Password); err != nil {
			return cliCmd, fmt.Errorf("password set failed: %w", err)
		}
		cliCmd += fmt.Sprintf(" && echo '%s:PASSWORD' | chpasswd", opts.Username)
	}

	return cliCmd, nil
}

// LockUser locks a user account. Returns the CLI equivalent.
func (m *Manager) LockUser(username string) (string, error) {
	cmd := fmt.Sprintf("usermod --lock %s", username)
	if err := exec.Command("usermod", "--lock", username).Run(); err != nil {
		return "", fmt.Errorf("lock %s: %w", username, err)
	}
	return cmd, nil
}

// UnlockUser unlocks a user account. Returns the CLI equivalent.
func (m *Manager) UnlockUser(username string) (string, error) {
	cmd := fmt.Sprintf("usermod --unlock %s", username)
	if err := exec.Command("usermod", "--unlock", username).Run(); err != nil {
		return "", fmt.Errorf("unlock %s: %w", username, err)
	}
	return cmd, nil
}

// DeleteUser deletes a user. If removeHome is true, also removes the home directory.
func (m *Manager) DeleteUser(username string, removeHome bool) (string, error) {
	args := []string{}
	if removeHome {
		args = append(args, "--remove")
	}
	args = append(args, username)

	cmd := "userdel " + strings.Join(args, " ")
	if err := exec.Command("userdel", args...).Run(); err != nil {
		return "", fmt.Errorf("userdel: %w", err)
	}
	return cmd, nil
}

// ChangeShell changes a user's login shell. Returns the CLI equivalent.
func (m *Manager) ChangeShell(username, shell string) (string, error) {
	cmd := fmt.Sprintf("chsh --shell %s %s", shell, username)
	if err := exec.Command("chsh", "--shell", shell, username).Run(); err != nil {
		return "", fmt.Errorf("chsh: %w", err)
	}
	return cmd, nil
}

// AddToGroup adds a user to a supplementary group. Returns the CLI equivalent.
func (m *Manager) AddToGroup(username, group string) (string, error) {
	cmd := fmt.Sprintf("usermod --append --groups %s %s", group, username)
	if err := exec.Command("usermod", "--append", "--groups", group, username).Run(); err != nil {
		return "", fmt.Errorf("add to group: %w", err)
	}
	return cmd, nil
}

// CreateGroup creates a new group. Returns the CLI equivalent.
func (m *Manager) CreateGroup(name string, gid int) (string, error) {
	args := []string{}
	if gid > 0 {
		args = append(args, "--gid", strconv.Itoa(gid))
	}
	args = append(args, name)
	cmd := "groupadd " + strings.Join(args, " ")
	if err := exec.Command("groupadd", args...).Run(); err != nil {
		return "", fmt.Errorf("groupadd: %w", err)
	}
	return cmd, nil
}

// DeleteGroup removes a group. Returns the CLI equivalent.
func (m *Manager) DeleteGroup(name string) (string, error) {
	cmd := fmt.Sprintf("groupdel %s", name)
	if err := exec.Command("groupdel", name).Run(); err != nil {
		return "", fmt.Errorf("groupdel: %w", err)
	}
	return cmd, nil
}

// AvailableShells returns the list of valid login shells from /etc/shells.
func AvailableShells() []string {
	f, err := os.Open("/etc/shells")
	if err != nil {
		return []string{"/bin/bash", "/bin/sh", "/bin/zsh", "/bin/fish"}
	}
	defer f.Close()
	var shells []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			shells = append(shells, line)
		}
	}
	return shells
}

// --- internal parsers -------------------------------------------------------

func parsePasswd() ([]User, error) {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var users []User
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) < 7 {
			continue
		}
		uid, _ := strconv.Atoi(parts[2])
		gid, _ := strconv.Atoi(parts[3])
		users = append(users, User{
			Username: parts[0],
			UID:      uid,
			GID:      gid,
			GECOS:    parts[4],
			Home:     parts[5],
			Shell:    parts[6],
			IsSystem: uid < 1000 && uid != 0, // root (0) shown; system 1-999 hidden by default
		})
	}
	return users, scanner.Err()
}

func parseShadow() map[string]string {
	m := make(map[string]string)
	f, err := os.Open("/etc/shadow")
	if err != nil {
		return m // not root — expected
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) >= 2 {
			m[parts[0]] = parts[1] // username → password hash
		}
	}
	return m
}

func parseGroups() ([]Group, error) {
	f, err := os.Open("/etc/group")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var groups []Group
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) < 4 {
			continue
		}
		gid, _ := strconv.Atoi(parts[2])
		var members []string
		if parts[3] != "" {
			members = strings.Split(parts[3], ",")
		}
		groups = append(groups, Group{
			Name:    parts[0],
			GID:     gid,
			Members: members,
		})
	}
	return groups, scanner.Err()
}

// parseGroupMembership returns a map of username → []groupName for supplementary groups.
func parseGroupMembership() map[string][]string {
	m := make(map[string][]string)
	f, err := os.Open("/etc/group")
	if err != nil {
		return m
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) < 4 || parts[3] == "" {
			continue
		}
		groupName := parts[0]
		for _, member := range strings.Split(parts[3], ",") {
			member = strings.TrimSpace(member)
			if member != "" {
				m[member] = append(m[member], groupName)
			}
		}
	}
	return m
}

func parseGIDToName() map[int]string {
	m := make(map[int]string)
	f, err := os.Open("/etc/group")
	if err != nil {
		return m
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) < 3 {
			continue
		}
		gid, err := strconv.Atoi(parts[2])
		if err == nil {
			m[gid] = parts[0]
		}
	}
	return m
}

func setPassword(username, password string) error {
	// echo "username:password" | chpasswd
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", username, password))
	return cmd.Run()
}
