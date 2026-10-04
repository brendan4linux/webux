<script lang="ts">
  import { onMount } from 'svelte';
  import HealthChecks from '$components/HealthChecks.svelte';
  import HardeningScore from '$components/HardeningScore.svelte';
  import PerformanceScore from '$components/PerformanceScore.svelte';

  let hostname    = $state('');
  let distro      = $state('');
  let kernel      = $state('');
  let uptime      = $state('');
  let cpuPct      = $state(0);
  let memUsed     = $state('');
  let memTotal    = $state('');
  let memPct      = $state(0);
  let diskUsed    = $state('');
  let diskTotal   = $state('');
  let diskPct     = $state(0);
  let load        = $state<number[]>([]);
  let loading     = $state(true);
  let info: any   = $state(null);

  // VM summary panel
  interface VM { id: string; name: string; state: string; memory_mb: number; vcpus: number; }
  interface ClusterVM { vmid: number; name: string; node: string; state: string; local: boolean; }
  let vms          = $state<VM[]>([]);
  let clusterVMs   = $state<ClusterVM[]>([]);
  let vmsLoaded    = $state(false);  // prevents duplicate fetches
  let vmsLoading   = $state(false);  // controls the loading spinner

  const quickLinks = [
    { label: 'Open Ports',  href: '#/ports',      desc: 'TCP/UDP listeners' },
    { label: 'Processes',   href: '#/processes',   desc: 'Running processes' },
    { label: 'Migration',   href: '#/migration',   desc: 'Host snapshot export' },
    { label: 'Services',    href: '#/services',    desc: 'systemd units' },
    { label: 'Containers',  href: '#/containers',  desc: 'Docker / Podman' },
  ];

  function fmtMB(mb: number): string {
    if (mb >= 1024) return (mb / 1024).toFixed(1) + ' GB';
    return mb.toFixed(0) + ' MB';
  }

  function fmtGB(gb: number): string {
    if (gb >= 1024) return (gb / 1024).toFixed(1) + ' TB';
    return gb.toFixed(1) + ' GB';
  }

  function fmtUptime(seconds: number): string {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d}d ${h}h ${m}m`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }

  async function loadStats() {
    try {
      const res = await fetch('/api/system/stats');
      if (!res.ok) return;
      const d = await res.json();

      hostname = d.hostname ?? '';
      distro   = d.distro  ?? '';
      kernel   = d.kernel  ?? '';

      // Handle both human-readable and numeric field formats
      cpuPct = d.cpu_percent ?? 0;
      load   = [d.load_avg_1 ?? 0, d.load_avg_5 ?? 0, d.load_avg_15 ?? 0].filter(Boolean);
      if (!load.length && d.load_avg) load = d.load_avg;

      // Memory — prefer human fields, fall back to numeric
      if (d.mem_used_human) {
        memUsed  = d.mem_used_human;
        memTotal = d.mem_total_human;
        memPct   = d.mem_percent ?? 0;
      } else if (d.mem_used_mb != null) {
        memUsed  = fmtMB(d.mem_used_mb);
        memTotal = fmtMB(d.mem_total_mb);
        memPct   = d.mem_total_mb > 0 ? (d.mem_used_mb / d.mem_total_mb) * 100 : 0;
      }

      // Disk — prefer human fields, fall back to numeric
      if (d.disk_used_human) {
        diskUsed  = d.disk_used_human;
        diskTotal = d.disk_total_human;
        diskPct   = d.disk_percent ?? 0;
      } else if (d.disk_used_gb != null) {
        diskUsed  = fmtGB(d.disk_used_gb);
        diskTotal = fmtGB(d.disk_total_gb);
        diskPct   = d.disk_total_gb > 0 ? (d.disk_used_gb / d.disk_total_gb) * 100 : 0;
      }

      // Uptime — prefer human field, fall back to seconds
      if (d.uptime_human) {
        uptime = d.uptime_human;
      } else if (d.uptime_seconds != null) {
        uptime = fmtUptime(d.uptime_seconds);
      }

    } catch {}
    finally { loading = false; }
  }

  async function loadInfo() {
    try {
      const res = await fetch('/api/system/info');
      if (res.ok) info = await res.json();
    } catch {}
  }

  function barColor(pct: number) {
    if (pct >= 90) return 'var(--red)';
    if (pct >= 75) return 'var(--yellow)';
    return 'var(--accent)';
  }

  function capabilities(): {label: string; active: boolean}[] {
    if (!info) return [];
    return [
      { label: 'Docker',           active: info.has_docker   },
      { label: 'Podman',           active: info.has_podman   },
      { label: 'Ansible',          active: info.has_ansible  },
      { label: 'Puppet',           active: info.has_puppet   },
      { label: 'UFW',              active: info.has_ufw      },
      { label: 'nftables',         active: info.has_nftables },
      { label: 'iptables',         active: info.has_iptables },
      { label: 'Virtual Machines', active: info.has_proxmox || info.has_kvm },
    ].filter(c => c.active !== undefined);
  }

  async function loadVMs(hasHypervisor: boolean) {
    if (!hasHypervisor || vmsLoaded) return;
    vmsLoaded  = true;
    vmsLoading = true;
    const [localRes, clusterRes] = await Promise.allSettled([
      fetch('/api/vms'),
      fetch('/api/vms/cluster'),
    ]);
    if (localRes.status === 'fulfilled' && localRes.value.ok)
      vms = await localRes.value.json();
    if (clusterRes.status === 'fulfilled' && clusterRes.value.ok)
      clusterVMs = await clusterRes.value.json();
    vmsLoading = false;
  }

  let hasHypervisor = $derived(info?.has_proxmox || info?.has_kvm);

  $effect(() => {
    if (hasHypervisor) loadVMs(true);
  });

  function vmStateCounts(list: VM[] | ClusterVM[]) {
    let running = 0, stopped = 0;
    for (const v of list) {
      if (v.state === 'running') running++;
      else stopped++;
    }
    return { running, stopped, total: list.length };
  }

  onMount(() => {
    loadStats();
    loadInfo();
    const iv = setInterval(loadStats, 5000);
    return () => clearInterval(iv);
  });
</script>

<div class="dash">
  <div class="dash-header">
    <h1>{hostname || 'Dashboard'}</h1>
    <p class="subtitle">
      {#if distro}{distro}{/if}
      {#if kernel} · {kernel}{/if}
    </p>
  </div>

  <!-- Row 1: Stat cards -->
  <div class="stat-row">
    <div class="stat-card">
      <div class="stat-top">
        <span class="stat-label">CPU</span>
        {#if loading}
          <div class="skeleton" style="width:60px;height:26px;display:inline-block"></div>
        {:else}
          <span class="stat-val mono" style="color:{barColor(cpuPct)}">{cpuPct.toFixed(1)}<span class="stat-unit">%</span></span>
        {/if}
      </div>
      <div class="stat-bar"><div class="stat-bar-fill" style="width:{Math.min(cpuPct,100)}%;background:{barColor(cpuPct)}"></div></div>
      {#if load.length >= 3}
        <div class="stat-sub mono">Load {load.map(l => l.toFixed(2)).join(' · ')}</div>
      {/if}
    </div>

    <div class="stat-card">
      <div class="stat-top">
        <span class="stat-label">MEMORY</span>
        {#if loading}
          <div class="skeleton" style="width:60px;height:26px;display:inline-block"></div>
        {:else}
          <span class="stat-val mono" style="color:{barColor(memPct)}">{memPct.toFixed(0)}<span class="stat-unit">%</span></span>
        {/if}
      </div>
      <div class="stat-bar"><div class="stat-bar-fill" style="width:{Math.min(memPct,100)}%;background:{barColor(memPct)}"></div></div>
      {#if memUsed}<div class="stat-sub mono">{memUsed} / {memTotal}</div>{/if}
    </div>

    <div class="stat-card">
      <div class="stat-top">
        <span class="stat-label">DISK (/)</span>
        {#if loading}
          <div class="skeleton" style="width:60px;height:26px;display:inline-block"></div>
        {:else}
          <span class="stat-val mono" style="color:{barColor(diskPct)}">{diskPct.toFixed(0)}<span class="stat-unit">%</span></span>
        {/if}
      </div>
      <div class="stat-bar"><div class="stat-bar-fill" style="width:{Math.min(diskPct,100)}%;background:{barColor(diskPct)}"></div></div>
      {#if diskUsed}<div class="stat-sub mono">{diskUsed} / {diskTotal}</div>{/if}
    </div>

    <div class="stat-card">
      <div class="stat-top">
        <span class="stat-label">UPTIME</span>
      </div>
      {#if loading}
        <div class="skeleton" style="height:26px;width:70%;margin:0.25rem 0"></div>
      {:else}
        <div class="uptime-val mono">{uptime || '—'}</div>
      {/if}
      <div class="stat-sub">since last boot</div>
    </div>
  </div>

  <!-- Row 2: Quick links -->
  <div class="quick-row">
    {#each quickLinks as link}
      <a href={link.href} class="quick-card">
        <span class="quick-label">{link.label}</span>
        <span class="quick-desc">{link.desc}</span>
      </a>
    {/each}
  </div>

  <!-- Row 3: Detected capabilities -->
  {#if info}
    {@const caps = capabilities()}
    {#if caps.length > 0}
      <div class="caps-row">
        <span class="caps-heading">DETECTED CAPABILITIES</span>
        <div class="caps-pills">
          {#each caps as cap}
            <span class="cap-pill" class:cap-active={cap.active} class:cap-inactive={!cap.active}>
              <span class="cap-dot"></span>{cap.label}
            </span>
          {/each}
        </div>
      </div>
    {/if}
  {/if}

  <!-- Row 4: VM summary (only on hypervisor hosts) -->
  {#if hasHypervisor}
    {@const local = vmStateCounts(vms)}
    {@const isProxmox = info?.has_proxmox}
    {@const hasCluster = clusterVMs.length > 0}
    <div class="vm-summary-row">
      <!-- Local VMs panel -->
      <a class="vm-panel" href="#/vms">
        <div class="vm-panel-header">
          <span class="vm-panel-title">{isProxmox ? 'Proxmox VMs (this node)' : 'Virtual Machines'}</span>
          <span class="vm-panel-link">View all →</span>
        </div>
        {#if vmsLoading}
          <div class="vm-loading"><span class="vm-spinner"></span>Scanning VMs…</div>
        {:else if !vms.length}
          <div class="vm-empty">No VMs found</div>
        {:else}
          <div class="vm-counts">
            <span class="vm-count running"><span class="vm-dot running"></span>{local.running} running</span>
            <span class="vm-count stopped"><span class="vm-dot stopped"></span>{local.stopped} stopped</span>
          </div>
          <div class="vm-list">
            {#each vms.slice(0, 8) as vm}
              <div class="vm-row">
                <span class="vm-dot {vm.state}"></span>
                <span class="vm-name">{vm.name}</span>
                {#if vm.memory_mb}
                  <span class="vm-mem">{vm.memory_mb >= 1024 ? (vm.memory_mb/1024).toFixed(0)+'G' : vm.memory_mb+'M'}</span>
                {/if}
              </div>
            {/each}
            {#if vms.length > 8}
              <div class="vm-more">+{vms.length - 8} more</div>
            {/if}
          </div>
        {/if}
      </a>

      <!-- Proxmox cluster panel (only when pvesh returns data) -->
      {#if isProxmox && hasCluster}
        {@const clusterLocal  = clusterVMs.filter(v => v.local)}
        {@const clusterRemote = clusterVMs.filter(v => !v.local)}
        {@const clusterCounts = vmStateCounts(clusterVMs)}
        <div class="vm-panel">
          <div class="vm-panel-header">
            <span class="vm-panel-title">Proxmox Cluster (all nodes)</span>
            <span class="vm-counts-inline">
              <span class="vm-count running"><span class="vm-dot running"></span>{clusterCounts.running}</span>
              <span class="vm-count stopped"><span class="vm-dot stopped"></span>{clusterCounts.stopped}</span>
            </span>
          </div>
          <!-- Group by node -->
          {#each [...new Set(clusterVMs.map(v => v.node))] as node}
            {@const nodeVMs = clusterVMs.filter(v => v.node === node)}
            <div class="cluster-node-label">{node}{clusterLocal.length && node === clusterLocal[0]?.node ? ' (this node)' : ''}</div>
            <div class="vm-list">
              {#each nodeVMs.slice(0, 6) as vm}
                <div class="vm-row">
                  <span class="vm-dot {vm.state}"></span>
                  <span class="vm-name">{vm.name || 'VM ' + vm.vmid}</span>
                  <span class="vm-mem" style="color:var(--text-tertiary)">#{vm.vmid}</span>
                </div>
              {/each}
              {#if nodeVMs.length > 6}
                <div class="vm-more">+{nodeVMs.length - 6} more on this node</div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  <!-- Row 5: Health checks + Security hardening -->
  <div class="health-row">
    <HealthChecks />
    <HardeningScore />
  </div>

  <!-- Row 5: Performance tuning (full width) -->
  <div class="perf-row">
    <PerformanceScore />
  </div>
</div>

<style>
.dash { max-width: 1200px; padding-bottom: 220px; }
.dash-header { margin-bottom: 1rem; }
.dash-header h1 { font-size: 1.3rem; font-weight: 700; margin: 0 0 0.2rem; }
.subtitle { font-size: 0.8rem; color: var(--text-secondary); margin: 0; }

.stat-row { display: grid; grid-template-columns: repeat(4, 1fr); gap: 0.625rem; margin-bottom: 0.75rem; }
.stat-card { background: var(--bg-panel); border: 1px solid var(--border-subtle); border-radius: var(--r-lg); padding: 0.875rem 1rem; }
.stat-top { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 0.5rem; }
.stat-label { font-size: 0.65rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.1em; color: var(--text-tertiary); }
.stat-val { font-size: 1.6rem; font-weight: 700; color: var(--text-primary); line-height: 1; }
.stat-unit { font-size: 0.9rem; color: var(--text-tertiary); }
.stat-bar { height: 4px; background: var(--bg-raised); border-radius: 2px; overflow: hidden; margin-bottom: 0.4rem; }
.stat-bar-fill { height: 100%; border-radius: 2px; transition: width 0.4s ease; }
.stat-sub { font-size: 0.7rem; color: var(--text-tertiary); }
.uptime-val { font-size: 1.35rem; font-weight: 700; color: var(--accent); margin: 0.2rem 0 0.4rem; line-height: 1.1; }

.quick-row { display: grid; grid-template-columns: repeat(5, 1fr); gap: 0.5rem; margin-bottom: 0.75rem; }
.quick-card { display: flex; flex-direction: column; gap: 3px; padding: 0.625rem 0.875rem; background: var(--bg-panel); border: 1px solid var(--border-subtle); border-radius: var(--r-lg); text-decoration: none; transition: border-color 0.12s, background 0.12s; }
.quick-card:hover { border-color: var(--accent); background: var(--accent-dim); }
.quick-label { font-size: 0.82rem; font-weight: 500; color: var(--text-primary); }
.quick-desc  { font-size: 0.7rem; color: var(--text-tertiary); }

.caps-row { background: var(--bg-panel); border: 1px solid var(--border-subtle); border-radius: var(--r-lg); padding: 0.75rem 1rem; margin-bottom: 0.75rem; display: flex; align-items: center; gap: 1rem; flex-wrap: wrap; }
.caps-heading { font-size: 0.6rem; font-weight: 600; text-transform: uppercase; letter-spacing: 0.12em; color: var(--text-tertiary); flex-shrink: 0; }
.caps-pills { display: flex; flex-wrap: wrap; gap: 0.75rem; }
.cap-pill { display: flex; align-items: center; gap: 0.35rem; font-size: 0.78rem; font-family: var(--font-mono); }
.cap-active  { color: var(--text-secondary); }
.cap-inactive { color: var(--text-tertiary); opacity: 0.4; }
.cap-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--accent); box-shadow: 0 0 4px var(--accent); flex-shrink: 0; }
.cap-inactive .cap-dot { background: var(--text-tertiary); box-shadow: none; }

.health-row { width: 100%; display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; }
.perf-row { width: 100%; margin-top: 0.75rem; }

/* VM summary row */
.vm-summary-row {
  display: flex;
  gap: 0.75rem;
  margin-bottom: 0.75rem;
  flex-wrap: wrap;
}
.vm-panel {
  flex: 1;
  min-width: 220px;
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-lg);
  padding: 0.75rem 1rem;
  text-decoration: none;
  color: inherit;
  display: block;
  transition: border-color 0.12s;
}
a.vm-panel:hover { border-color: var(--accent); }
.vm-panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.6rem;
}
.vm-panel-title {
  font-size: 0.65rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--text-tertiary);
}
.vm-panel-link {
  font-size: 0.7rem;
  color: var(--accent);
}
.vm-loading, .vm-empty {
  font-size: 0.78rem;
  color: var(--text-tertiary);
  display: flex;
  align-items: center;
  gap: 0.4rem;
}
.vm-spinner {
  width: 12px;
  height: 12px;
  border: 2px solid var(--border-subtle);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: vm-spin 0.7s linear infinite;
  flex-shrink: 0;
}
@keyframes vm-spin {
  to { transform: rotate(360deg); }
}
.vm-counts {
  display: flex;
  gap: 1rem;
  margin-bottom: 0.5rem;
}
.vm-counts-inline {
  display: flex;
  gap: 0.6rem;
}
.vm-count {
  display: flex;
  align-items: center;
  gap: 0.3rem;
  font-size: 0.78rem;
}
.vm-count.running { color: #4ade80; }
.vm-count.stopped { color: var(--text-tertiary); }
.vm-list {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}
.vm-row {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.78rem;
}
.vm-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}
.vm-dot.running   { background: #4ade80; box-shadow: 0 0 4px #4ade8088; }
.vm-dot.stopped   { background: var(--text-tertiary); }
.vm-dot.paused,
.vm-dot.suspended { background: #facc15; }
.vm-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-secondary);
}
.vm-mem {
  font-size: 0.7rem;
  color: var(--text-tertiary);
  flex-shrink: 0;
  font-family: var(--font-mono);
}
.vm-more {
  font-size: 0.7rem;
  color: var(--text-tertiary);
  padding-top: 0.1rem;
}
.cluster-node-label {
  font-size: 0.65rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--accent);
  margin: 0.5rem 0 0.2rem;
}
</style>
