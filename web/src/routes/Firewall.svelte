<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface Rule {
    id: string; chain: string; table: string; action: string;
    protocol: string; src_ip: string; dst_ip: string;
    port: string; comment: string; raw: string;
  }

  interface FWStatus {
    backend: string; active: boolean;
    rules: Rule[]; raw_lines: string[];
  }

  let status: FWStatus | null = $state(null);
  let loading = $state(true);
  let error = $state('');
  let showRaw = $state(false);
  let showAddRule = $state(false);
  let actionPending = $state<string|null>(null);

  let newRule = $state({
    action: 'allow', protocol: 'tcp', port: '', src_ip: '', comment: ''
  });
  let addRuleError = $state('');

  async function load() {
    loading = true; error = '';
    try {
      status = await api.get<FWStatus>('/api/firewall');
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function toggleFirewall() {
    if (!status) return;
    actionPending = 'toggle';
    try {
      if (status.active) {
        await api.post('/api/firewall/disable', {});
      } else {
        await api.post('/api/firewall/enable', {});
      }
      await load();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  async function deleteRule(id: string) {
    if (!confirm(`Delete rule #${id}?`)) return;
    actionPending = 'del:' + id;
    try {
      await api.delete(`/api/firewall/rules/${encodeURIComponent(id)}`);
      await load();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  async function addRule() {
    addRuleError = '';
    if (!newRule.port && !newRule.src_ip) {
      addRuleError = 'Specify at least a port or source IP'; return;
    }
    actionPending = 'add';
    try {
      await api.post('/api/firewall/rules', newRule);
      showAddRule = false;
      newRule = { action:'allow', protocol:'tcp', port:'', src_ip:'', comment:'' };
      await load();
    } catch(e: any) { addRuleError = e.message; }
    finally { actionPending = null; }
  }

  function actionClass(action: string) {
    const a = action?.toLowerCase();
    if (a === 'allow' || a === 'accept') return 'badge-green';
    if (a === 'deny' || a === 'drop') return 'badge-red';
    if (a === 'reject') return 'badge-yellow';
    return 'badge-gray';
  }

  function backendLabel(b: string) {
    const labels: Record<string,string> = { ufw:'UFW', nftables:'nftables', iptables:'iptables', none:'None' };
    return labels[b] ?? b;
  }

  onMount(load);
</script>

<div class="fw-page">
  <div class="page-header">
    <div>
      <h1>Firewall</h1>
      {#if status}
        <p class="subtitle">
          {backendLabel(status.backend)}
          · {status.rules?.length ?? 0} rules
        </p>
      {/if}
    </div>
    <div class="actions">
      <button class="btn" onclick={load} disabled={loading}>⟳ Refresh</button>
      {#if status}
        <button class="btn {status.active ? 'btn-danger' : 'btn-primary'}"
          disabled={actionPending !== null || status.backend === 'none'}
          onclick={toggleFirewall}>
          {status.active ? '⬛ Disable' : '▶ Enable'}
        </button>
      {/if}
    </div>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  {#if loading}
    <div class="card" style="padding:2rem;text-align:center;color:var(--text-tertiary)">Loading firewall status…</div>
  {:else if !status || status.backend === 'none'}
    <div class="card no-fw">
      <div class="no-fw-icon">⬡</div>
      <h2>No firewall detected</h2>
      <p>Install ufw, nftables, or iptables to manage firewall rules through Webux.</p>
      <code class="install-hint">sudo apt install ufw && sudo ufw enable</code>
    </div>
  {:else}
    <!-- Status banner -->
    <div class="status-banner card" class:active={status.active}>
      <div class="status-left">
        <span class="dot {status.active ? 'dot-green' : 'dot-red'}"></span>
        <span class="status-text">{status.active ? 'Active' : 'Inactive'}</span>
        <span class="badge badge-gray">{backendLabel(status.backend)}</span>
      </div>
      <div class="status-right">
        <button class="btn btn-ghost" style="font-size:0.75rem"
          onclick={() => showRaw = !showRaw}>
          {showRaw ? 'Hide' : 'Show'} raw output
        </button>
        <button class="btn btn-primary" onclick={() => showAddRule = true}>+ Add rule</button>
      </div>
    </div>

    <!-- Raw output -->
    {#if showRaw}
      <div class="card raw-output">
        <pre>{(status.raw_lines ?? []).join('\n')}</pre>
      </div>
    {/if}

    <!-- Rules table -->
    <div class="card" style="padding:0;overflow-x:auto;margin-top:0.75rem">
      <table class="data-table">
        <thead>
          <tr>
            <th>#</th>
            <th>Action</th>
            <th>Protocol</th>
            <th>Port</th>
            <th>Source</th>
            <th>Chain</th>
            <th>Comment</th>
            <th style="text-align:right">Delete</th>
          </tr>
        </thead>
        <tbody>
          {#if !status.rules?.length}
            <tr>
              <td colspan="8" style="text-align:center;padding:2rem;color:var(--text-tertiary)">
                No rules found — firewall may be using default policies only
              </td>
            </tr>
          {:else}
            {#each status.rules as rule (rule.id)}
              <tr class="rule-row">
                <td class="mono" style="color:var(--text-tertiary)">{rule.id}</td>
                <td><span class="badge {actionClass(rule.action)}">{rule.action}</span></td>
                <td class="mono">{rule.protocol || 'any'}</td>
                <td class="mono">{rule.port || '—'}</td>
                <td class="mono" style="color:var(--text-secondary)">{rule.src_ip || 'Anywhere'}</td>
                <td class="mono" style="color:var(--text-tertiary)">{rule.chain || '—'}</td>
                <td style="color:var(--text-secondary);font-size:0.78rem">{rule.comment || '—'}</td>
                <td style="text-align:right">
                  <button class="btn btn-ghost" style="color:var(--red);font-size:0.75rem"
                    disabled={actionPending !== null}
                    onclick={() => deleteRule(rule.id)}>✕</button>
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  {/if}

  <!-- Add rule modal -->
  {#if showAddRule}
    <div class="modal-overlay" role="button" tabindex="0"
      onkeydown={(e) => e.key === 'Escape' && (showAddRule = false)}
      onclick={() => showAddRule = false}>
      <div class="modal" role="dialog" onclick={(e) => e.stopPropagation()}>
        <div class="modal-header">
          <h2>Add firewall rule</h2>
          <button class="btn btn-ghost" onclick={() => showAddRule = false}>✕</button>
        </div>
        <div class="modal-body">
          {#if addRuleError}<div class="alert alert-error" style="margin-bottom:0.75rem">{addRuleError}</div>{/if}
          <div class="form-grid">
            <div class="form-row">
              <label for="fr-action">Action</label>
              <select id="fr-action" class="search-input" bind:value={newRule.action}>
                <option value="allow">Allow</option>
                <option value="deny">Deny</option>
              </select>
            </div>
            <div class="form-row">
              <label for="fr-proto">Protocol</label>
              <select id="fr-proto" class="search-input" bind:value={newRule.protocol}>
                <option value="tcp">TCP</option>
                <option value="udp">UDP</option>
                <option value="any">Any</option>
              </select>
            </div>
            <div class="form-row">
              <label for="fr-port">Port</label>
              <input id="fr-port" class="search-input" bind:value={newRule.port} placeholder="80, 443, 8000:8100" />
            </div>
            <div class="form-row">
              <label for="fr-src">Source IP</label>
              <input id="fr-src" class="search-input" bind:value={newRule.src_ip} placeholder="Leave blank for anywhere" />
            </div>
            <div class="form-row" style="grid-column:1/-1">
              <label for="fr-comment">Comment</label>
              <input id="fr-comment" class="search-input" bind:value={newRule.comment} placeholder="Optional description" />
            </div>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn" onclick={() => showAddRule = false}>Cancel</button>
          <button class="btn btn-primary" onclick={addRule} disabled={actionPending === 'add'}>
            {actionPending === 'add' ? 'Adding…' : 'Add rule'}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <CLIEchoPane context="firewall" />
</div>

<style>
.fw-page { max-width:1100px; padding-bottom:220px; }

.status-banner { display:flex; align-items:center; justify-content:space-between; padding:0.875rem 1rem; margin-bottom:0; }
.status-banner.active { border-color:var(--green); }
.status-left { display:flex; align-items:center; gap:0.625rem; }
.status-text { font-weight:600; font-size:0.9rem; }
.status-right { display:flex; gap:0.5rem; align-items:center; }

.raw-output { margin-top:0.5rem; padding:0.75rem; background:var(--bg-base); }
.raw-output pre { font-family:var(--font-mono); font-size:0.72rem; color:var(--text-secondary); white-space:pre-wrap; word-break:break-all; margin:0; }

.rule-row:hover { background:var(--bg-hover); }

.no-fw { text-align:center; padding:3rem 2rem; }
.no-fw-icon { font-size:2.5rem; color:var(--text-tertiary); margin-bottom:1rem; }
.no-fw h2 { margin-bottom:0.5rem; }
.no-fw p { color:var(--text-secondary); font-size:0.875rem; margin-bottom:1rem; }
.install-hint { display:inline-block; background:var(--bg-raised); padding:0.4rem 0.75rem; border-radius:var(--r-md); font-family:var(--font-mono); font-size:0.8rem; color:var(--accent); }

.btn-danger { background:var(--red-dim); border-color:var(--red); color:var(--red); }

.modal-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.6); display:flex; align-items:center; justify-content:center; z-index:500; }
.modal { background:var(--bg-panel); border:1px solid var(--border-default); border-radius:var(--r-lg); width:440px; max-width:95vw; }
.modal-header { display:flex; justify-content:space-between; align-items:center; padding:1rem; border-bottom:1px solid var(--border-subtle); }
.modal-header h2 { font-size:1rem; margin:0; }
.modal-body { padding:1rem; }
.modal-footer { display:flex; justify-content:flex-end; gap:0.5rem; padding:1rem; border-top:1px solid var(--border-subtle); }
.form-grid { display:grid; grid-template-columns:1fr 1fr; gap:0.75rem; }
.form-row { display:flex; flex-direction:column; gap:0.25rem; }
.form-row label { font-size:0.75rem; color:var(--text-secondary); font-weight:500; }
.form-row select { background:var(--bg-raised); color:var(--text-primary); }
</style>
