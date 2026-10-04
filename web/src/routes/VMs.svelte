<script lang="ts">
  import { onMount } from 'svelte';

  interface VM {
    id: string;
    name: string;
    state: string;
    memory_mb: number;
    vcpus: number;
    disk?: string;
    backend: string;
  }

  interface Info {
    available: boolean;
    backend: string;
    version: string;
  }

  let info = $state<Info | null>(null);
  let vms = $state<VM[]>([]);
  let loading = $state(true);
  let error = $state('');
  let actionPending = $state<Record<string, string>>({});

  onMount(() => {
    loadAll();
  });

  async function loadAll() {
    loading = true;
    error = '';
    try {
      const [infoRes, listRes] = await Promise.all([
        fetch('/api/vms/info'),
        fetch('/api/vms'),
      ]);
      info = await infoRes.json();
      vms  = await listRes.json();
    } catch (e) {
      error = String(e);
    } finally {
      loading = false;
    }
  }

  async function doAction(vm: VM, action: string) {
    actionPending = { ...actionPending, [vm.id]: action };
    try {
      const res = await fetch(`/api/vms/${encodeURIComponent(vm.id)}/action`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action }),
      });
      if (!res.ok) {
        const msg = await res.text();
        alert(`Action failed: ${msg}`);
      } else {
        await loadAll();
      }
    } catch (e) {
      alert(String(e));
    } finally {
      const next = { ...actionPending };
      delete next[vm.id];
      actionPending = next;
    }
  }

  function stateBadge(state: string): string {
    switch (state) {
      case 'running':   return 'state-running';
      case 'stopped':   return 'state-stopped';
      case 'paused':    return 'state-paused';
      case 'suspended': return 'state-paused';
      case 'starting':  return 'state-starting';
      default:          return 'state-other';
    }
  }

  function backendLabel(b: string): string {
    if (b === 'proxmox') return 'Proxmox';
    if (b === 'libvirt') return 'KVM/QEMU (libvirt)';
    return b;
  }

  function fmtMem(mb: number): string {
    if (!mb) return '—';
    if (mb >= 1024) return (mb / 1024).toFixed(1) + ' GiB';
    return mb + ' MiB';
  }
</script>

<div class="vms-wrap">
  <div class="page-header">
    <div class="header-left">
      <h1 class="page-title">Virtual Machines</h1>
      {#if info}
        <span class="backend-chip">{backendLabel(info.backend)}</span>
      {/if}
    </div>
    <button class="btn-refresh" onclick={loadAll} disabled={loading}>
      {loading ? 'Loading…' : '↻ Refresh'}
    </button>
  </div>

  {#if loading && !vms.length}
    <div class="loading-msg">Detecting hypervisor…</div>

  {:else if error}
    <div class="error-msg">{error}</div>

  {:else if !info?.available}
    <div class="unavail-card">
      <div class="unavail-icon">⬡</div>
      <h2>No Hypervisor Detected</h2>
      <p>Webux supports <strong>KVM/QEMU</strong> (via libvirt / virsh) and <strong>Proxmox VE</strong> (via qm).</p>
      <div class="unavail-hints">
        <div class="hint">
          <strong>Proxmox VE</strong>
          <span>Install Proxmox VE, or run on a Proxmox node — <code>qm</code> will be detected automatically.</span>
        </div>
        <div class="hint">
          <strong>KVM / QEMU</strong>
          <span>Install libvirt: <code>apt install libvirt-clients</code> or <code>dnf install libvirt-client</code></span>
        </div>
      </div>
    </div>

  {:else if !vms.length}
    <div class="empty-msg">No virtual machines found.</div>

  {:else}
    <div class="vm-table-wrap">
      <table class="vm-table">
        <thead>
          <tr>
            <th>ID</th>
            <th>Name</th>
            <th>State</th>
            <th>Memory</th>
            <th>vCPUs</th>
            <th>Disk</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each vms as vm (vm.id)}
            {@const pending = actionPending[vm.id]}
            <tr>
              <td class="vm-id">{vm.id}</td>
              <td class="vm-name">{vm.name}</td>
              <td>
                <span class="state-badge {stateBadge(vm.state)}">{vm.state}</span>
              </td>
              <td>{fmtMem(vm.memory_mb)}</td>
              <td>{vm.vcpus || '—'}</td>
              <td class="vm-disk">{vm.disk || '—'}</td>
              <td class="vm-actions">
                {#if pending}
                  <span class="action-pending">{pending}…</span>
                {:else}
                  {#if vm.state !== 'running'}
                    <button class="act-btn act-start" onclick={() => doAction(vm, 'start')} title="Start">▶ Start</button>
                  {/if}
                  {#if vm.state === 'running'}
                    <button class="act-btn act-stop" onclick={() => doAction(vm, 'stop')} title="Graceful Stop">⏹ Stop</button>
                    <button class="act-btn act-reboot" onclick={() => doAction(vm, 'reboot')} title="Reboot">↺ Reboot</button>
                    <button class="act-btn act-suspend" onclick={() => doAction(vm, 'suspend')} title="Suspend">⏸ Suspend</button>
                    <button class="act-btn act-force" onclick={() => doAction(vm, 'force-stop')} title="Force Stop">✕ Force</button>
                  {/if}
                  {#if vm.state === 'suspended' || vm.state === 'paused'}
                    <button class="act-btn act-start" onclick={() => doAction(vm, 'resume')} title="Resume">▶ Resume</button>
                  {/if}
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<style>
.vms-wrap {
  padding: 1.5rem 2rem;
  max-width: 1200px;
}
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 1.5rem;
}
.header-left {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.page-title {
  font-size: 1.35rem;
  font-weight: 600;
  margin: 0;
}
.backend-chip {
  font-size: 0.7rem;
  padding: 0.2rem 0.55rem;
  border-radius: 999px;
  background: color-mix(in srgb, var(--accent, #6d8aff) 15%, transparent);
  color: var(--accent, #6d8aff);
  font-weight: 600;
  letter-spacing: 0.03em;
}
.btn-refresh {
  padding: 0.4rem 1rem;
  border-radius: 6px;
  border: 1px solid var(--border, #333);
  background: var(--surface2, #1e1e1e);
  color: var(--text, #e0e0e0);
  cursor: pointer;
  font-size: 0.85rem;
}
.btn-refresh:hover { background: var(--surface3, #2a2a2a); }
.btn-refresh:disabled { opacity: 0.5; cursor: default; }

.loading-msg, .empty-msg {
  color: var(--text-muted, #888);
  padding: 2rem 0;
}
.error-msg {
  color: #f87171;
  padding: 1rem 0;
}

/* Unavailable card */
.unavail-card {
  background: var(--surface2, #161616);
  border: 1px solid var(--border, #2a2a2a);
  border-radius: 12px;
  padding: 2.5rem;
  max-width: 580px;
  text-align: center;
}
.unavail-icon { font-size: 2.5rem; margin-bottom: 0.75rem; opacity: 0.4; }
.unavail-card h2 { font-size: 1.2rem; margin: 0 0 0.5rem; }
.unavail-card p { color: var(--text-muted, #888); font-size: 0.9rem; margin-bottom: 1.5rem; }
.unavail-hints { display: flex; flex-direction: column; gap: 0.75rem; text-align: left; }
.hint {
  background: var(--surface3, #1e1e1e);
  border-radius: 8px;
  padding: 0.75rem 1rem;
  font-size: 0.85rem;
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
}
.hint strong { font-size: 0.8rem; color: var(--accent, #6d8aff); }
.hint span { color: var(--text-muted, #888); }
.hint code { font-family: monospace; color: var(--text, #e0e0e0); }

/* Table */
.vm-table-wrap { overflow-x: auto; }
.vm-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.88rem;
}
.vm-table th {
  text-align: left;
  padding: 0.5rem 0.75rem;
  color: var(--text-muted, #888);
  font-weight: 500;
  font-size: 0.78rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  border-bottom: 1px solid var(--border, #2a2a2a);
}
.vm-table td {
  padding: 0.65rem 0.75rem;
  border-bottom: 1px solid color-mix(in srgb, var(--border, #2a2a2a) 50%, transparent);
  vertical-align: middle;
}
.vm-table tr:last-child td { border-bottom: none; }
.vm-id { font-family: monospace; color: var(--text-muted, #888); font-size: 0.8rem; }
.vm-name { font-weight: 500; }
.vm-disk { font-family: monospace; font-size: 0.8rem; color: var(--text-muted, #888); }

/* State badges */
.state-badge {
  display: inline-block;
  padding: 0.15rem 0.55rem;
  border-radius: 999px;
  font-size: 0.72rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.state-running  { background: color-mix(in srgb, #4ade80 15%, transparent); color: #4ade80; }
.state-stopped  { background: color-mix(in srgb, #888 15%, transparent);    color: #888; }
.state-paused   { background: color-mix(in srgb, #facc15 15%, transparent); color: #facc15; }
.state-starting { background: color-mix(in srgb, #60a5fa 15%, transparent); color: #60a5fa; }
.state-other    { background: color-mix(in srgb, #f87171 15%, transparent); color: #f87171; }

/* Action buttons */
.vm-actions { display: flex; gap: 0.35rem; flex-wrap: wrap; }
.act-btn {
  padding: 0.25rem 0.55rem;
  border-radius: 5px;
  border: 1px solid var(--border, #333);
  background: var(--surface2, #1e1e1e);
  color: var(--text, #e0e0e0);
  cursor: pointer;
  font-size: 0.75rem;
  white-space: nowrap;
}
.act-btn:hover { background: var(--surface3, #2a2a2a); }
.act-start:hover  { border-color: #4ade80; color: #4ade80; }
.act-stop:hover   { border-color: #f87171; color: #f87171; }
.act-reboot:hover { border-color: #60a5fa; color: #60a5fa; }
.act-suspend:hover { border-color: #facc15; color: #facc15; }
.act-force:hover  { border-color: #f87171; color: #f87171; background: color-mix(in srgb, #f87171 10%, transparent); }
.action-pending {
  font-size: 0.75rem;
  color: var(--text-muted, #888);
  font-style: italic;
}
</style>
