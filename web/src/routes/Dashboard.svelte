<script lang="ts">
  import { onMount } from 'svelte';
  import HealthChecks from '$components/HealthChecks.svelte';
  import HardeningScore from '$components/HardeningScore.svelte';

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
      { label: 'Docker',   active: info.has_docker   },
      { label: 'Podman',   active: info.has_podman   },
      { label: 'Ansible',  active: info.has_ansible  },
      { label: 'Puppet',   active: info.has_puppet   },
      { label: 'UFW',      active: info.has_ufw      },
      { label: 'nftables', active: info.has_nftables },
      { label: 'iptables', active: info.has_iptables },
    ].filter(c => c.active !== undefined);
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

  <!-- Row 4: Health checks + Security hardening -->
  <div class="health-row">
    <HealthChecks />
    <HardeningScore />
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
</style>
