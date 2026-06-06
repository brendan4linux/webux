<script lang="ts">
  import { onMount } from 'svelte';
  import { wsStore } from '$lib/ws';
  import { api } from '$lib/api';

  interface Stats {
    cpu_percent: number;
    mem_used_mb: number;
    mem_total_mb: number;
    load_avg_1: number;
    load_avg_5: number;
    load_avg_15: number;
    uptime_seconds: number;
    disk_used_gb: number;
    disk_total_gb: number;
  }

  let stats: Stats | null = $state(null);
  let hostInfo: any = $state(null);
  let loading = $state(true);

  function formatUptime(seconds: number): string {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    return d > 0 ? `${d}d ${h}h` : h > 0 ? `${h}h ${m}m` : `${m}m`;
  }

  function pct(used: number, total: number): number {
    if (!total) return 0;
    return Math.round((used / total) * 100);
  }

  function barColor(p: number): string {
    if (p > 85) return 'var(--red)';
    if (p > 65) return 'var(--yellow)';
    return 'var(--green)';
  }

  onMount(async () => {
    try {
      [stats, hostInfo] = await Promise.all([
        api.get<Stats>('/api/system/stats'),
        api.get<any>('/api/system/info'),
      ]);
    } catch {}
    loading = false;

    // Live metric updates from WebSocket
    wsStore.subscribe(evt => {
      if (evt?.type === 'metric') stats = evt.payload;
    });
  });

  const quickLinks = [
    { label: 'Open Ports',  href: '#/ports',      desc: 'TCP/UDP listeners' },
    { label: 'Processes',   href: '#/processes',   desc: 'Running processes' },
    { label: 'Migration',   href: '#/migration',   desc: 'Host snapshot & template' },
    { label: 'Services',    href: '#/services',    desc: 'systemd units' },
    { label: 'Containers',  href: '#/containers',  desc: 'Docker / Podman' },
  ];
</script>

<div class="dashboard">
  <!-- Host identity strip -->
  {#if hostInfo}
    <div class="host-strip">
      <span class="host-name mono">{hostInfo.hostname ?? '—'}</span>
      <span class="host-sep">·</span>
      <span class="host-detail">{hostInfo.distro ?? ''}</span>
      <span class="host-sep">·</span>
      <span class="host-detail">{hostInfo.arch ?? ''}</span>
      <span class="host-sep">·</span>
      <span class="host-detail">kernel {hostInfo.kernel ?? ''}</span>
      {#if hostInfo.init_system}
        <span class="host-sep">·</span>
        <span class="badge badge-gray">{hostInfo.init_system}</span>
      {/if}
    </div>
  {/if}

  <!-- Metric cards row -->
  <div class="metrics-grid">
    <!-- CPU -->
    <div class="metric-card">
      <div class="metric-header">
        <span class="metric-label">CPU</span>
        {#if stats}
          <span class="metric-value mono" style="color: {barColor(stats.cpu_percent)}">
            {stats.cpu_percent.toFixed(1)}%
          </span>
        {:else}
          <span class="metric-value skeleton" style="width:40px;height:18px;display:inline-block"></span>
        {/if}
      </div>
      {#if stats}
        <div class="metric-bar">
          <div class="metric-bar-fill" style="width:{stats.cpu_percent}%;background:{barColor(stats.cpu_percent)}"></div>
        </div>
        <div class="metric-sub">Load {stats.load_avg_1.toFixed(2)} · {stats.load_avg_5.toFixed(2)} · {stats.load_avg_15.toFixed(2)}</div>
      {/if}
    </div>

    <!-- Memory -->
    <div class="metric-card">
      <div class="metric-header">
        <span class="metric-label">Memory</span>
        {#if stats}
          <span class="metric-value mono" style="color:{barColor(pct(stats.mem_used_mb, stats.mem_total_mb))}">
            {pct(stats.mem_used_mb, stats.mem_total_mb)}%
          </span>
        {:else}
          <span class="metric-value skeleton" style="width:40px;height:18px;display:inline-block"></span>
        {/if}
      </div>
      {#if stats}
        <div class="metric-bar">
          <div class="metric-bar-fill" style="width:{pct(stats.mem_used_mb,stats.mem_total_mb)}%;background:{barColor(pct(stats.mem_used_mb,stats.mem_total_mb))}"></div>
        </div>
        <div class="metric-sub">{stats.mem_used_mb.toFixed(0)} / {stats.mem_total_mb.toFixed(0)} MB</div>
      {/if}
    </div>

    <!-- Disk -->
    <div class="metric-card">
      <div class="metric-header">
        <span class="metric-label">Disk (/)</span>
        {#if stats}
          <span class="metric-value mono" style="color:{barColor(pct(stats.disk_used_gb, stats.disk_total_gb))}">
            {pct(stats.disk_used_gb, stats.disk_total_gb)}%
          </span>
        {:else}
          <span class="metric-value skeleton" style="width:40px;height:18px;display:inline-block"></span>
        {/if}
      </div>
      {#if stats}
        <div class="metric-bar">
          <div class="metric-bar-fill" style="width:{pct(stats.disk_used_gb,stats.disk_total_gb)}%;background:{barColor(pct(stats.disk_used_gb,stats.disk_total_gb))}"></div>
        </div>
        <div class="metric-sub">{stats.disk_used_gb.toFixed(1)} / {stats.disk_total_gb.toFixed(1)} GB</div>
      {/if}
    </div>

    <!-- Uptime -->
    <div class="metric-card">
      <div class="metric-header">
        <span class="metric-label">Uptime</span>
        {#if stats}
          <span class="metric-value mono" style="color:var(--green)">{formatUptime(stats.uptime_seconds)}</span>
        {:else}
          <span class="metric-value skeleton" style="width:50px;height:18px;display:inline-block"></span>
        {/if}
      </div>
      <div class="metric-bar" style="background:transparent"></div>
      <div class="metric-sub" style="color:var(--text-tertiary)">since last boot</div>
    </div>
  </div>

  <!-- Quick links -->
  <div class="quick-links">
    {#each quickLinks as link}
      <a href={link.href} class="quick-link">
        <span class="quick-label">{link.label}</span>
        <span class="quick-desc">{link.desc}</span>
      </a>
    {/each}
  </div>

  <!-- Capability flags from host detection -->
  {#if hostInfo}
    <div class="capabilities">
      <h3>Detected capabilities</h3>
      <div class="cap-grid">
        {#each [
          ['Docker',   hostInfo.has_docker],
          ['Podman',   hostInfo.has_podman],
          ['Ansible',  hostInfo.has_ansible],
          ['Puppet',   hostInfo.has_puppet],
          ['UFW',      hostInfo.has_ufw],
          ['nftables', hostInfo.has_nftables],
          ['iptables', hostInfo.has_iptables],
        ] as [name, present]}
          <div class="cap-item">
            <span class="dot {present ? 'dot-green' : 'dot-gray'}"></span>
            <span class="cap-name">{name}</span>
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
.dashboard { max-width: 1100px; }

.host-strip {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.78rem;
  color: var(--text-secondary);
  margin-bottom: 1.25rem;
  padding: 0.5rem 0.75rem;
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-md);
}
.host-name { color: var(--text-primary); font-weight: 500; }
.host-sep { color: var(--text-tertiary); }
.host-detail { color: var(--text-secondary); }

.metrics-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 0.75rem;
  margin-bottom: 1rem;
}

.metric-card {
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-lg);
  padding: 1rem;
}

.metric-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 0.6rem;
}
.metric-label {
  font-size: 0.72rem;
  font-weight: 500;
  color: var(--text-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.metric-value {
  font-size: 1rem;
  font-weight: 600;
}

.metric-bar {
  height: 3px;
  background: var(--bg-active);
  border-radius: 2px;
  overflow: hidden;
  margin-bottom: 0.4rem;
}
.metric-bar-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.4s ease;
}
.metric-sub {
  font-family: var(--font-mono);
  font-size: 0.7rem;
  color: var(--text-tertiary);
}

.quick-links {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 0.5rem;
  margin-bottom: 1.25rem;
}
.quick-link {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
  padding: 0.75rem;
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-md);
  text-decoration: none;
  transition: border-color 0.15s, background 0.15s;
}
.quick-link:hover {
  border-color: var(--accent);
  background: var(--accent-dim);
}
.quick-label {
  font-size: 0.82rem;
  font-weight: 500;
  color: var(--text-primary);
}
.quick-link:hover .quick-label { color: var(--accent); }
.quick-desc {
  font-size: 0.72rem;
  color: var(--text-tertiary);
}

.capabilities {
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-lg);
  padding: 1rem;
}
.capabilities h3 { margin-bottom: 0.75rem; }

.cap-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem 1.25rem;
}
.cap-item {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.8rem;
  color: var(--text-secondary);
}
.cap-name { font-family: var(--font-mono); }
</style>
