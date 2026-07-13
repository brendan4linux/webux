<script lang="ts">
  let { currentHash }: { currentHash: string } = $props();

  const navGroups = [
    {
      label: 'System',
      items: [
        { href: '#/', icon: '⬛', label: 'Dashboard' },
        { href: '#/services', icon: '⚙', label: 'Services' },
        { href: '#/processes', icon: '◈', label: 'Processes' },
        { href: '#/disks',     icon: '◫', label: 'Disks' },
        { href: '#/users',     icon: '◉', label: 'Users' },
        { href: '#/logs',      icon: '≡', label: 'Logs' },
      ]
    },
    {
      label: 'Network',
      items: [
        { href: '#/ports', icon: '◎', label: 'Ports & Sockets' },
        { href: '#/network', icon: '⟁', label: 'Interfaces' },
        { href: '#/firewall', icon: '⬡', label: 'Firewall' },
      ]
    },
    {
      label: 'Applications',
      items: [
        { href: '#/containers', icon: '▣', label: 'Containers' },
        { href: '#/databases', icon: '⬠', label: 'Databases' },
        { href: '#/webservers', icon: '◫', label: 'Webservers' },
        { href: '#/packages', icon: '◫', label: 'Packages' },
        { href: '#/files', icon: '◱', label: 'Files' },
        { href: '#/cron', icon: '◷', label: 'Cron' },
      ]
    },
    {
      label: 'Automation',
      items: [
        { href: '#/ansible', icon: '▶', label: 'Ansible' },
        { href: '#/puppet', icon: '◆', label: 'Puppet' },
      ]
    },
    {
      label: 'Tools',
      items: [
        { href: '#/migration', icon: '⇨', label: 'Migration' },
        { href: '#/terminal', icon: '▮', label: 'Terminal' },
        { href: '#/ai', icon: '◍', label: 'AI Assistant' },
      ]
    },
  ];
</script>

<nav class="sidebar">
  <!-- Wordmark — monospace, matches logo font -->
  <div class="logo">
    <span class="logo-word">web</span><span class="logo-accent">ux</span>
  </div>

  <!-- Nav groups -->
  {#each navGroups as group}
    <div class="nav-group">
      <span class="nav-group-label">{group.label}</span>
      {#each group.items as item}
        <a
          href={item.href}
          class="nav-item"
          class:active={currentHash === item.href}
        >
          <span class="nav-icon" aria-hidden="true">{item.icon}</span>
          <span class="nav-label">{item.label}</span>
        </a>
      {/each}
    </div>
  {/each}

  <!-- Footer -->
  <div class="sidebar-footer">
    <a href="#/settings" class="nav-item" class:active={currentHash === '#/settings'}>
      <span class="nav-icon" aria-hidden="true">◈</span>
      <span class="nav-label">Settings</span>
    </a>
    <div class="version-tag">webux dev</div>
  </div>
</nav>

<style>
.sidebar {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow-y: auto;
  overflow-x: hidden;
}

/* ── Wordmark ── */
.logo {
  padding: 0 1rem;
  height: 52px;
  display: flex;
  align-items: center;
  gap: 0;
  border-bottom: 1px solid var(--border-subtle);
  flex-shrink: 0;
}
.logo-word {
  font-family: var(--font-mono);
  font-size: 1.45rem;
  font-weight: 700;
  color: var(--text-primary);
  letter-spacing: -0.03em;
}
.logo-accent {
  font-family: var(--font-mono);
  font-size: 1.45rem;
  font-weight: 700;
  color: var(--accent);
  letter-spacing: -0.03em;
}

/* ── Nav groups ── */
.nav-group {
  padding: 0.75rem 0 0.25rem;
  flex-shrink: 0;
}

.nav-group-label {
  display: block;
  font-size: 0.65rem;
  font-weight: 500;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.1em;
  padding: 0 0.875rem 0.375rem;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.45rem 0.875rem;
  color: var(--nav-item);
  font-size: 0.9rem;
  font-weight: 400;
  font-family: var(--font-mono);
  text-decoration: none;
  border-radius: 0;
  transition: color 0.12s, background 0.12s;
  position: relative;
}
.nav-item:hover {
  color: var(--nav-item-hover);    /* emerald on hover */
  background: var(--accent-dim);
}
.nav-item.active {
  color: var(--accent);
  background: var(--accent-dim);
  font-weight: 500;
}
.nav-item.active::before {
  content: '';
  position: absolute;
  left: 0; top: 0; bottom: 0;
  width: 2px;
  background: var(--accent);
  border-radius: 0 2px 2px 0;
}

.nav-icon {
  font-size: 0.75rem;
  width: 14px;
  text-align: center;
  flex-shrink: 0;
  opacity: 0.6;
}
.nav-item:hover .nav-icon,
.nav-item.active .nav-icon { opacity: 1; }

.nav-label { flex: 1; }

/* ── Footer ── */
.sidebar-footer {
  margin-top: auto;
  padding: 0.5rem 0;
  border-top: 1px solid var(--border-subtle);
  flex-shrink: 0;
}
.version-tag {
  font-family: var(--font-mono);
  font-size: 0.6rem;
  color: var(--text-tertiary);
  padding: 0.375rem 0.875rem;
  letter-spacing: 0.08em;
}
</style>
