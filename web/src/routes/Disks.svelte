<script lang="ts">
  import { onMount } from 'svelte';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface MountUsage {
    mountpoint: string; total: number; used: number; free: number;
    use_percent: number; total_human: string; used_human: string;
    free_human: string; fstype: string; device: string;
  }
  interface LogicalVolume {
    name: string; full_name: string; vg_name: string;
    size: number; size_human: string; mountpoint: string;
    fstype: string; usage?: MountUsage; extendable: boolean;
  }
  interface VolumeGroup {
    name: string; size: number; free: number; used: number;
    size_human: string; free_human: string; used_human: string;
    pv_count: number; lv_count: number; volumes: LogicalVolume[];
  }
  interface BlockDevice {
    name: string; path: string; type: string; size: number;
    size_human: string; fstype: string; mountpoint: string;
    label: string; model: string; rota: boolean;
    children: BlockDevice[];
  }
  interface Summary {
    devices: BlockDevice[]; mounts: MountUsage[];
    volume_groups: VolumeGroup[]; lvm_available: boolean;
    total_disks: number; total_partitions: number;
  }

  let summary: Summary | null = $state(null);
  let loading = $state(true);
  let error = $state('');

  // ── Directory usage drill-down ───────────────────────────────────
  interface DirUsage {
    path: string; bytes: number; human: string; size_pct: number;
  }
  let usageMount = $state('');
  let usageBreadcrumb: string[] = $state([]);
  let usageEntries: DirUsage[] = $state([]);
  let usageLoading = $state(false);
  let usageError = $state('');

  async function openUsage(mountpoint: string) {
    if (usageMount === mountpoint) { closeUsage(); return; } // toggle off
    usageMount = mountpoint;
    usageBreadcrumb = [mountpoint];
    await fetchUsage(mountpoint, mountpoint);
  }

  async function fetchUsage(mount: string, target: string) {
    usageLoading = true; usageError = ''; usageEntries = [];
    try {
      const url = target === mount
        ? `/api/disks/usage?mount=${encodeURIComponent(mount)}&top=20`
        : `/api/disks/drilldown?mount=${encodeURIComponent(mount)}&path=${encodeURIComponent(target)}&top=20`;
      const res = await fetch(url);
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      usageEntries = data.entries ?? [];
    } catch(e: any) { usageError = e.message; }
    finally { usageLoading = false; }
  }

  async function drillInto(entry: DirUsage) {
    usageBreadcrumb = [...usageBreadcrumb, entry.path];
    await fetchUsage(usageMount, entry.path);
  }

  async function jumpTo(idx: number) {
    usageBreadcrumb = usageBreadcrumb.slice(0, idx + 1);
    await fetchUsage(usageMount, usageBreadcrumb[usageBreadcrumb.length - 1]);
  }

  function closeUsage() {
    usageMount = ''; usageBreadcrumb = []; usageEntries = [];
  }

  const maxUsageBytes = $derived(
    usageEntries.length > 0 ? Math.max(...usageEntries.map(e => e.bytes)) : 1
  );

  // Extend modal
  let extendTarget: LogicalVolume | null = $state(null);
  let extendVG: VolumeGroup | null = $state(null);
  let extendGB = $state(1);
  let extending = $state(false);
  let extendOutput: string[] = $state([]);
  let extendDone = $state(false);

  async function load() {
    loading = true; error = '';
    try {
      const res = await fetch('/api/disks');
      if (!res.ok) throw new Error(await res.text());
      summary = await res.json();
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  function openExtend(lv: LogicalVolume, vg: VolumeGroup) {
    extendTarget = lv;
    extendVG = vg;
    extendGB = 1;
    extendOutput = [];
    extendDone = false;
  }

  async function runExtend() {
    if (!extendTarget) return;
    extending = true; extendOutput = []; extendDone = false;
    try {
      const resp = await fetch('/api/disks/extend', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          lv_path: extendTarget.full_name,
          size_gb: extendGB,
          fstype: extendTarget.fstype,
        }),
      });
      const reader = resp.body!.getReader();
      const dec = new TextDecoder(); let buf = '';
      while (true) {
        const { done, value } = await reader.read(); if (done) break;
        buf += dec.decode(value, { stream: true });
        const lines = buf.split('\n');
        for (const l of lines.slice(0, -1)) {
          if (l.startsWith('data: ')) extendOutput = [...extendOutput, l.slice(6)];
        }
        buf = lines[lines.length - 1];
      }
      extendDone = true;
      await load(); // refresh sizes
    } catch(e: any) { extendOutput = [...extendOutput, '[error] ' + String(e)]; }
    finally { extending = false; }
  }

  function pct(used: number, total: number) {
    if (!total) return 0;
    return Math.round(used / total * 100);
  }

  function barColor(p: number) {
    if (p >= 90) return 'var(--red)';
    if (p >= 75) return 'var(--yellow)';
    return 'var(--accent)';
  }

  function lineClass(line: string) {
    if (line.includes('✓') || line.includes('successfully')) return 'line-ok';
    if (line.includes('[error]') || line.includes('failed') || line.includes('Error')) return 'line-err';
    if (line.startsWith('[webux]')) return 'line-meta';
    return '';
  }

  function maxExtend(vg: VolumeGroup): number {
    return Math.floor(vg.free / 1024 / 1024 / 1024 * 10) / 10;
  }

  onMount(load);
</script>

<div class="disks-page">
  <div class="page-header">
    <div>
      <h1>Disks</h1>
      {#if summary}
        <p class="subtitle">
          {summary.total_disks} disk{summary.total_disks !== 1 ? 's' : ''}
          · {summary.total_partitions} partition{summary.total_partitions !== 1 ? 's' : ''}
          {#if summary.lvm_available}
            · <span style="color:var(--accent)">LVM detected</span>
            · {(summary.volume_groups ?? []).length} VG{(summary.volume_groups ?? []).length !== 1 ? 's' : ''}
          {/if}
        </p>
      {/if}
    </div>
    <div class="actions">
      <button class="btn" onclick={load} disabled={loading}>⟳ Refresh</button>
    </div>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  {#if loading}
    {#each [1,2,3] as _}
      <div class="card skeleton" style="height:80px;margin-bottom:0.5rem"></div>
    {/each}
  {:else if summary}

    <!-- ── Volume Groups (LVM) ─────────────────────────────────────── -->
    {#if summary.lvm_available && (summary.volume_groups ?? []).length > 0}
      <h2 class="section-heading">Volume Groups</h2>
      {#each (summary.volume_groups ?? []) as vg (vg.name)}
        <div class="card vg-card">
          <!-- VG header -->
          <div class="vg-header">
            <div class="vg-info">
              <span class="vg-name mono">{vg.name}</span>
              <span class="badge badge-gray">{vg.pv_count} PV{vg.pv_count !== 1 ? 's' : ''}</span>
              <span class="badge badge-blue">{vg.lv_count} LV{vg.lv_count !== 1 ? 's' : ''}</span>
              {#if vg.free > 0}
                <span class="badge badge-green">{vg.free_human} free</span>
              {/if}
            </div>
            <div class="vg-sizes">
              <span class="mono" style="font-size:0.78rem;color:var(--text-secondary)">
                {vg.used_human} / {vg.size_human}
              </span>
            </div>
          </div>

          <!-- VG usage bar -->
          <div class="usage-bar-wrap">
            <div class="usage-bar">
              <div class="usage-bar-fill"
                style="width:{pct(vg.used, vg.size)}%;background:{barColor(pct(vg.used, vg.size))}">
              </div>
              {#if vg.free > 0}
                <div class="usage-bar-free"
                  style="width:{pct(vg.free, vg.size)}%;background:rgba(255,255,255,0.06)">
                </div>
              {/if}
            </div>
            <span class="usage-pct">{pct(vg.used, vg.size)}%</span>
          </div>

          <!-- Logical Volumes -->
          {#if vg.volumes?.length > 0}
            <div class="lv-list">
              {#each vg.volumes as lv (lv.name)}
                <div class="lv-row">
                  <div class="lv-tree">└─</div>
                  <div class="lv-info">
                    <span class="mono lv-name">{lv.full_name}</span>
                    {#if lv.mountpoint}
                      <span class="badge badge-gray">{lv.mountpoint}</span>
                    {/if}
                    {#if lv.fstype}
                      <span class="badge badge-purple">{lv.fstype}</span>
                    {/if}
                  </div>

                  <div class="lv-usage">
                    {#if lv.usage}
                      <div class="mini-bar-wrap">
                        <div class="mini-bar">
                          <div class="mini-bar-fill"
                            style="width:{lv.usage.use_percent.toFixed(0)}%;background:{barColor(lv.usage.use_percent)}">
                          </div>
                        </div>
                        <span class="mono lv-pct">{lv.usage.use_percent.toFixed(0)}%</span>
                        <span style="font-size:0.72rem;color:var(--text-tertiary)">
                          {lv.usage.used_human}/{lv.usage.total_human}
                        </span>
                      </div>
                    {:else}
                      <span class="mono" style="font-size:0.78rem;color:var(--text-tertiary)">{lv.size_human}</span>
                    {/if}
                  </div>

                  {#if lv.extendable}
                    <button class="btn btn-primary extend-btn"
                      onclick={() => openExtend(lv, vg)}>
                      + Extend
                    </button>
                  {:else}
                    <span class="extend-placeholder"></span>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/each}
    {/if}

    <!-- ── Physical Disks ─────────────────────────────────────────── -->
    <h2 class="section-heading" style="margin-top:1.25rem">Physical Disks</h2>
    {#each (summary.devices ?? []) as dev (dev.name)}
      <div class="card disk-card">
        <div class="disk-header">
          <div class="disk-icon">{dev.rota ? '💿' : '⚡'}</div>
          <div class="disk-info">
            <span class="mono disk-name">{dev.name}</span>
            {#if dev.model}
              <span style="font-size:0.78rem;color:var(--text-secondary)">{dev.model}</span>
            {/if}
            <span class="badge badge-gray">{dev.size_human}</span>
            <span class="badge {dev.rota ? 'badge-gray' : 'badge-blue'}">{dev.rota ? 'HDD' : 'SSD'}</span>
          </div>
        </div>

        {#if dev.children?.length > 0}
          <div class="part-list">
            {#each dev.children as child (child.name)}
              {@const childMounts = child.fstype && child.fstype !== 'LVM2_member'
                ? (summary.mounts ?? []).filter(m => m.device === child.path)
                : []}
              <div class="part-row" class:row-active={childMounts.some(m => usageMount === m.mountpoint)}>
                <div class="part-tree">├─</div>
                <span class="mono part-name">{child.name}</span>
                <span class="badge badge-gray">{child.size_human}</span>
                {#if child.type === 'lvm' || child.fstype === 'LVM2_member'}
                  <span class="badge badge-green">LVM</span>
                {:else if child.fstype}
                  <span class="badge badge-purple">{child.fstype}</span>
                {/if}
                {#if child.label}
                  <span style="font-size:0.72rem;color:var(--text-tertiary)">({child.label})</span>
                {/if}
                {#each childMounts as m (m.mountpoint)}
                  <button class="btn btn-ghost browse-btn"
                    onclick={() => openUsage(m.mountpoint)}
                    title="Browse largest directories on {m.mountpoint}">
                    {usageMount === m.mountpoint ? '▾' : '🔍'} {m.mountpoint}
                  </button>
                {/each}
                <!-- Recurse one more level for nested LVM/crypto -->
                {#each child.children ?? [] as gc (gc.name)}
                  <div class="gc-row">
                    <span class="mono" style="color:var(--text-tertiary);font-size:0.68rem">
                      └─ {gc.name} {gc.size_human} {gc.fstype ?? ''} {gc.mountpoint ?? ''}
                    </span>
                  </div>
                {/each}
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/each}

    <!-- ── Non-LVM Mounts ─────────────────────────────────────────── -->
    {#if (summary.mounts ?? []).filter(m => !(summary.volume_groups ?? []).some(vg => vg.volumes.some(lv => lv.mountpoint === m.mountpoint))).length > 0}
      <h2 class="section-heading" style="margin-top:1.25rem">Other Mounts</h2>
      <div class="card" style="padding:0">
        <table class="data-table">
          <thead>
            <tr><th>Mount point</th><th>Device</th><th>FS</th><th>Size</th><th>Used</th><th style="min-width:120px">Usage</th><th></th></tr>
          </thead>
          <tbody>
            {#each (summary.mounts ?? []).filter(m => !(summary.volume_groups ?? []).some(vg => vg.volumes.some(lv => lv.mountpoint === m.mountpoint))) as m (m.mountpoint)}
              <tr class:row-active={usageMount === m.mountpoint}>
                <td class="mono" style="font-weight:600">{m.mountpoint}</td>
                <td class="mono" style="font-size:0.75rem;color:var(--text-secondary);max-width:160px;overflow:hidden;text-overflow:ellipsis">{m.device}</td>
                <td><span class="badge badge-gray">{m.fstype}</span></td>
                <td class="mono" style="font-size:0.8rem">{m.total_human}</td>
                <td class="mono" style="font-size:0.8rem;color:var(--text-secondary)">{m.used_human}</td>
                <td>
                  <div style="display:flex;align-items:center;gap:0.5rem">
                    <div class="mini-bar" style="width:90px">
                      <div class="mini-bar-fill"
                        style="width:{m.use_percent.toFixed(0)}%;background:{barColor(m.use_percent)}">
                      </div>
                    </div>
                    <span class="mono" style="font-size:0.72rem;color:{barColor(m.use_percent)}">{m.use_percent.toFixed(0)}%</span>
                  </div>
                </td>
                <td>
                  <button class="btn btn-ghost browse-btn"
                    onclick={() => openUsage(m.mountpoint)}
                    title="Browse largest directories">
                    {usageMount === m.mountpoint ? '▾ Close' : '🔍 Browse'}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

  {/if}

  <!-- ── Directory usage drill-down panel ──────────────────────── -->
  {#if usageMount}
    <div class="usage-panel card" style="margin-top:1rem;padding:0">
      <div class="usage-panel-header">
        <div style="display:flex;align-items:center;gap:0.5rem;flex-wrap:wrap">
          <span style="font-size:0.72rem;font-weight:600;text-transform:uppercase;letter-spacing:0.08em;color:var(--text-tertiary)">Directory usage</span>
          <!-- Breadcrumb -->
          <nav class="breadcrumb">
            {#each usageBreadcrumb as crumb, i}
              {#if i > 0}<span class="crumb-sep">/</span>{/if}
              {#if i === usageBreadcrumb.length - 1}
                <span class="mono crumb-current">{i === 0 ? crumb : crumb.split('/').filter(Boolean).pop()}</span>
              {:else}
                <button class="btn btn-ghost crumb-link mono" onclick={() => jumpTo(i)}>
                  {i === 0 ? crumb : crumb.split('/').filter(Boolean).pop()}
                </button>
              {/if}
            {/each}
          </nav>
        </div>
        <div style="display:flex;gap:0.5rem;align-items:center">
          <span style="font-size:0.72rem;color:var(--text-tertiary)">Device-locked · won't cross other mounts</span>
          <button class="btn btn-ghost" onclick={closeUsage}>✕</button>
        </div>
      </div>

      {#if usageError}
        <div class="alert alert-error" style="margin:0.75rem">{usageError}</div>
      {:else if usageLoading}
        <div class="usage-loading">
          <span class="spinner"></span> Scanning {usageMount}…
        </div>
      {:else if usageEntries.length === 0}
        <div style="padding:2rem;text-align:center;color:var(--text-tertiary);font-size:0.85rem">No subdirectories found.</div>
      {:else}
        <div class="usage-list">
          {#each usageEntries as entry (entry.path)}
            {@const label = entry.path.split('/').filter(Boolean).pop() || entry.path}
            {@const barW = maxUsageBytes > 0 ? (entry.bytes / maxUsageBytes * 100).toFixed(1) + '%' : '0%'}
            <button class="usage-row" onclick={() => drillInto(entry)}>
              <span class="mono usage-name" title={entry.path}>📁 {label}</span>
              <div class="usage-bar-track">
                <div class="usage-bar-fill" style="width:{barW}"></div>
              </div>
              <span class="mono usage-size">{entry.human}</span>
              <span class="usage-pct-label">{entry.size_pct.toFixed(1)}%</span>
              <span class="usage-drill">→</span>
            </button>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  <!-- ── Extend modal ───────────────────────────────────────────── -->
  {#if extendTarget && extendVG}
    <div class="modal-overlay" role="button" tabindex="0"
      onkeydown={(e) => e.key === 'Escape' && !extending && (extendTarget = null)}
      onclick={() => !extending && (extendTarget = null)}>
      <div class="modal extend-modal" role="dialog" tabindex="-1"
        onclick={(e) => e.stopPropagation()}>
        <div class="modal-header">
          <h2>Extend {extendTarget.name}</h2>
          {#if !extending}
            <button class="btn btn-ghost" onclick={() => extendTarget = null}>✕</button>
          {/if}
        </div>
        <div class="modal-body">
          <div class="extend-meta">
            <div class="meta-row">
              <span class="meta-label">Logical volume</span>
              <code class="mono">{extendTarget.full_name}</code>
            </div>
            <div class="meta-row">
              <span class="meta-label">Filesystem</span>
              <span class="badge badge-purple">{extendTarget.fstype || 'auto-detect'}</span>
            </div>
            {#if extendTarget.mountpoint}
              <div class="meta-row">
                <span class="meta-label">Mount point</span>
                <code class="mono">{extendTarget.mountpoint}</code>
              </div>
            {/if}
            <div class="meta-row">
              <span class="meta-label">VG free space</span>
              <span style="color:var(--accent);font-weight:500">{extendVG.free_human} available</span>
            </div>
          </div>

          <div class="extend-input-row">
            <label for="extend-gb">Add space (GB)</label>
            <div style="display:flex;align-items:center;gap:0.75rem">
              <input id="extend-gb" type="number" class="search-input mono"
                style="width:110px"
                bind:value={extendGB}
                min="0.1" max={maxExtend(extendVG)} step="0.5"
                disabled={extending} />
              <span style="font-size:0.8rem;color:var(--text-tertiary)">
                max {maxExtend(extendVG)} GB
              </span>
            </div>
          </div>

          {#if extendGB > maxExtend(extendVG)}
            <div class="alert alert-error">
              Exceeds available VG space ({extendVG.free_human} free)
            </div>
          {/if}

          <div class="preview-cmd">
            <span class="mono" style="font-size:0.72rem;color:var(--text-tertiary)">
              lvextend -L +{extendGB}G {extendTarget.full_name}
              {#if extendTarget.fstype === 'xfs'}
                &amp;&amp; xfs_growfs {extendTarget.mountpoint}
              {:else if extendTarget.fstype === 'btrfs'}
                &amp;&amp; btrfs filesystem resize max {extendTarget.mountpoint}
              {:else}
                &amp;&amp; resize2fs {extendTarget.full_name}
              {/if}
            </span>
          </div>

          {#if extendOutput.length > 0}
            <div class="extend-output">
              {#each extendOutput as line}
                <div class="mono out-line {lineClass(line)}">{line}</div>
              {/each}
              {#if extending}
                <div class="mono" style="color:var(--text-tertiary)">⟳ running…</div>
              {/if}
            </div>
          {/if}

          {#if extendDone}
            <div class="alert alert-success">✓ Extension complete — filesystem resized live, no reboot needed</div>
          {/if}
        </div>
        <div class="modal-footer">
          {#if extendDone}
            <button class="btn btn-primary" onclick={() => extendTarget = null}>Done</button>
          {:else}
            <button class="btn" onclick={() => extendTarget = null} disabled={extending}>Cancel</button>
            <button class="btn btn-primary" onclick={runExtend}
              disabled={extending || extendGB <= 0 || extendGB > maxExtend(extendVG)}>
              {extending ? '⟳ Extending…' : `+ Extend ${extendGB} GB`}
            </button>
          {/if}
        </div>
      </div>
    </div>
  {/if}

  <CLIEchoPane context="disks" />
</div>

<style>
.disks-page { max-width:1100px; padding-bottom:220px; }
.section-heading { font-size:0.72rem; font-weight:600; text-transform:uppercase; letter-spacing:0.08em; color:var(--text-tertiary); margin:0 0 0.5rem; }

/* VG card */
.vg-card { padding:0; margin-bottom:0.5rem; }
.vg-header { display:flex; align-items:center; justify-content:space-between; padding:0.875rem 1rem 0.5rem; }
.vg-info { display:flex; align-items:center; gap:0.375rem; }
.vg-name { font-weight:700; font-size:0.95rem; margin-right:0.25rem; }

.usage-bar-wrap { display:flex; align-items:center; gap:0.5rem; padding:0 1rem 0.625rem; }
.usage-bar { flex:1; height:8px; background:var(--bg-raised); border-radius:4px; overflow:hidden; display:flex; }
.usage-bar-fill { height:100%; border-radius:4px 0 0 4px; transition:width 0.3s; }
.usage-bar-free { height:100%; background:rgba(46,216,152,0.12); border:1px dashed rgba(46,216,152,0.3); border-radius:0 4px 4px 0; }
.usage-pct { font-size:0.72rem; font-family:var(--font-mono); color:var(--text-tertiary); min-width:32px; text-align:right; }

/* LV rows */
.lv-list { border-top:1px solid var(--border-subtle); }
.lv-row { display:flex; align-items:center; gap:0.625rem; padding:0.5rem 1rem; border-bottom:1px solid var(--border-subtle); }
.lv-row:last-child { border-bottom:none; }
.lv-tree { color:var(--text-tertiary); font-family:var(--font-mono); font-size:0.8rem; flex-shrink:0; }
.lv-info { display:flex; align-items:center; gap:0.375rem; flex:1; min-width:0; }
.lv-name { font-size:0.82rem; color:var(--text-secondary); }
.lv-usage { display:flex; align-items:center; gap:0.5rem; min-width:200px; }
.mini-bar-wrap { display:flex; align-items:center; gap:0.375rem; }
.mini-bar { width:80px; height:6px; background:var(--bg-raised); border-radius:3px; overflow:hidden; }
.mini-bar-fill { height:100%; border-radius:3px; transition:width 0.3s; }
.lv-pct { font-size:0.7rem; color:var(--text-tertiary); min-width:28px; }
.extend-btn { font-size:0.72rem; padding:0.2rem 0.6rem; flex-shrink:0; }
.extend-placeholder { width:72px; flex-shrink:0; }

/* Disk card */
.disk-card { padding:0; margin-bottom:0.5rem; }
.disk-header { display:flex; align-items:center; gap:0.75rem; padding:0.875rem 1rem; }
.disk-icon { font-size:1.1rem; }
.disk-info { display:flex; align-items:center; gap:0.375rem; flex-wrap:wrap; }
.disk-name { font-weight:700; font-size:0.9rem; margin-right:0.25rem; }
.part-list { border-top:1px solid var(--border-subtle); padding:0.5rem 1rem 0.625rem 2.5rem; display:flex; flex-direction:column; gap:0.375rem; }
.part-row { display:flex; align-items:center; gap:0.375rem; flex-wrap:wrap; }
.part-tree { color:var(--text-tertiary); font-family:var(--font-mono); font-size:0.8rem; }
.part-name { font-size:0.82rem; }
.gc-row { padding-left:1.5rem; }

/* Extend modal */
.extend-modal { width:500px; max-width:95vw; }
.extend-meta { display:flex; flex-direction:column; gap:0.5rem; background:var(--bg-raised); border-radius:var(--r-md); padding:0.75rem; margin-bottom:0.875rem; }
.meta-row { display:flex; align-items:center; gap:0.75rem; font-size:0.82rem; }
.meta-label { color:var(--text-tertiary); min-width:110px; font-size:0.75rem; }
.extend-input-row { display:flex; flex-direction:column; gap:0.375rem; margin-bottom:0.75rem; }
.extend-input-row label { font-size:0.75rem; color:var(--text-secondary); font-weight:500; }
.preview-cmd { background:var(--bg-base); border-radius:var(--r-sm); padding:0.5rem 0.625rem; margin-bottom:0.75rem; border-left:2px solid var(--border-default); }
.extend-output { background:var(--bg-base); border-radius:var(--r-md); padding:0.625rem; max-height:200px; overflow-y:auto; margin-bottom:0.5rem; }
.out-line { font-size:0.72rem; line-height:1.6; white-space:pre-wrap; }
.out-line.line-ok   { color:var(--accent); }
.out-line.line-err  { color:var(--red); }
.out-line.line-meta { color:var(--text-tertiary); }
.alert-success { background:rgba(46,216,152,0.1); border:1px solid var(--accent); color:var(--accent); border-radius:var(--r-md); padding:0.5rem 0.75rem; font-size:0.82rem; }

/* Modal shared */
.modal-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.6); display:flex; align-items:center; justify-content:center; z-index:500; }
.modal { background:var(--bg-panel); border:1px solid var(--border-default); border-radius:var(--r-lg); }
.modal-header { display:flex; justify-content:space-between; align-items:center; padding:1rem; border-bottom:1px solid var(--border-subtle); }
.modal-header h2 { font-size:1rem; margin:0; }
.modal-body { padding:1rem; }
.modal-footer { display:flex; justify-content:flex-end; gap:0.5rem; padding:1rem; border-top:1px solid var(--border-subtle); }

/* Directory usage drill-down */
.row-active td { background:color-mix(in srgb, var(--accent) 6%, transparent); }
.browse-btn { font-size:0.72rem; padding:0.2rem 0.5rem; white-space:nowrap; }

.usage-panel-header { display:flex; justify-content:space-between; align-items:center; flex-wrap:wrap; gap:0.5rem; padding:0.75rem 1rem; border-bottom:1px solid var(--border-subtle); }

.breadcrumb { display:flex; align-items:center; gap:0.1rem; flex-wrap:wrap; }
.crumb-sep { color:var(--text-tertiary); padding:0 0.1rem; font-size:0.78rem; }
.crumb-link { font-size:0.78rem; padding:0.1rem 0.25rem; color:var(--accent); }
.crumb-current { font-size:0.78rem; color:var(--text-primary); font-weight:600; }

.usage-loading { display:flex; align-items:center; gap:0.625rem; padding:1.5rem 1rem; color:var(--text-tertiary); font-size:0.85rem; }
.spinner { display:inline-block; width:14px; height:14px; border:2px solid var(--border-default); border-top-color:var(--accent); border-radius:50%; animation:spin 0.7s linear infinite; flex-shrink:0; }
@keyframes spin { to { transform:rotate(360deg); } }

.usage-list { display:flex; flex-direction:column; }
.usage-row {
  display:grid;
  grid-template-columns: minmax(140px,1.2fr) 1fr 80px 54px 16px;
  align-items:center;
  gap:0.75rem;
  padding:0.45rem 1rem;
  border-bottom:1px solid var(--border-subtle);
  background:none;
  border-left:none; border-right:none; border-top:none;
  cursor:pointer;
  text-align:left;
  color:inherit;
  font:inherit;
  transition:background 0.1s;
}
.usage-row:last-child { border-bottom:none; }
.usage-row:hover { background:var(--bg-raised); }
.usage-name { font-size:0.82rem; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
.usage-bar-track { height:6px; background:var(--bg-raised); border-radius:3px; overflow:hidden; }
.usage-bar-fill { height:100%; background:var(--accent); border-radius:3px; opacity:0.7; transition:width 0.2s; }
.usage-size { font-size:0.82rem; font-weight:600; text-align:right; }
.usage-pct-label { font-size:0.7rem; color:var(--text-tertiary); text-align:right; }
.usage-drill { color:var(--text-tertiary); font-size:0.75rem; }
</style>
