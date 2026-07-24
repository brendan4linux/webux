<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';

  let loading = $state(true);
  let saving = $state(false);
  let saved = $state(false);
  let error = $state('');

  // Server
  let port = $state('8989');
  // Ansible
  let ansibleDir = $state('/etc/ansible');
  let ansibleInventory = $state('/etc/ansible/hosts');
  // Puppet
  let puppetConfDir = $state('/etc/puppetlabs/puppet');
  // Auth
  let bypassToken = $state('');
  let showBypassToken = $state(false);
  let whoami = $state<any>(null);

  // Health checks
  let healthChecks: any[] = $state([]);
  let healthExpanded = $state(false);
  let healthSaving = $state(false);
  let editingCheck = $state<any>(null);  // check currently being edited
  let showAddCheck = $state(false);
  let newCheck = $state({ id: '', label: '', description: '', command: '', detail_command: '', enabled: true });

  async function load() {
    loading = true; error = '';
    try {
      const [whoRes, ansRes, portRes, bypassRes, hcRes] = await Promise.all([
        fetch('/auth/whoami').then(r => r.ok ? r.json() : null).catch(() => null),
        api.get<any>('/api/ansible/status'),
        fetch('/api/settings/server').then(r => r.ok ? r.json() : null).catch(() => null),
        fetch('/api/settings/auth').then(r => r.ok ? r.json() : null).catch(() => null),
        fetch('/api/health/checks').then(r => r.ok ? r.json() : null).catch(() => null),
      ]);
      whoami = whoRes;
      if (ansRes) { ansibleDir = ansRes.playbook_dir ?? '/etc/ansible'; ansibleInventory = ansRes.inventory ?? '/etc/ansible/hosts'; }
      if (portRes?.port) port = portRes.port;
      if (bypassRes) bypassToken = bypassRes.bypass_token ?? '';
      if (hcRes?.checks) healthChecks = hcRes.checks;
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function save() {
    saving = true; saved = false; error = '';
    try {
      await Promise.all([
        api.put('/api/ansible/settings', { playbook_dir: ansibleDir, inventory: ansibleInventory }),
        api.put('/api/settings/server', { port }),
        api.put('/api/settings/auth', { bypass_token: bypassToken }),
      ]);
      saved = true;
      setTimeout(() => saved = false, 3000);
    } catch(e: any) { error = e.message; }
    finally { saving = false; }
  }

  async function saveHealthChecks() {
    healthSaving = true;
    try {
      await fetch('/api/health/checks', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ checks: healthChecks }),
      });
      saved = true; setTimeout(() => saved = false, 2500);
    } catch(e: any) { error = e.message; }
    finally { healthSaving = false; }
  }

  async function resetHealthChecks() {
    if (!confirm('Reset health checks to defaults?')) return;
    const res = await fetch('/api/health/checks', { method: 'DELETE' }).then(r => r.json()).catch(() => null);
    if (res?.checks) healthChecks = res.checks;
  }

  function addCheck() {
    if (!newCheck.id || !newCheck.label || !newCheck.command) return;
    healthChecks = [...healthChecks, { ...newCheck }];
    newCheck = { id: '', label: '', description: '', command: '', detail_command: '', enabled: true };
    showAddCheck = false;
  }

  function removeCheck(id: string) {
    if (!confirm('Remove this check?')) return;
    healthChecks = healthChecks.filter(c => c.id !== id);
  }

  function toggleCheck(id: string) {
    healthChecks = healthChecks.map(c => c.id === id ? { ...c, enabled: !c.enabled } : c);
  }

  function generateToken() {
    const arr = new Uint8Array(32);
    crypto.getRandomValues(arr);
    bypassToken = Array.from(arr).map(b => b.toString(16).padStart(2, '0')).join('');
  }

  async function logout() {
    await fetch('/auth/logout', { method: 'POST' });
    window.location.reload();
  }

  onMount(load);
</script>

<div class="settings-page">
  <div class="page-header">
    <div>
      <h1>Settings</h1>
      {#if whoami}
        <p class="subtitle">Logged in as <strong>{whoami.username}</strong> · {whoami.auth_backend}</p>
      {/if}
    </div>
    <div class="actions">
      {#if whoami}
        <button class="btn btn-ghost" onclick={logout}>Sign out</button>
      {/if}
      <button class="btn btn-primary" onclick={save} disabled={saving}
        style={saved ? 'background:var(--green);border-color:var(--green)' : ''}>
        {saving ? 'Saving…' : saved ? '✓ Saved' : 'Save settings'}
      </button>
    </div>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  {#if loading}
    {#each [1,2,3,4] as _}
      <div class="card skeleton" style="height:120px;margin-bottom:0.75rem"></div>
    {/each}
  {:else}
    <!-- Server -->
    <div class="card settings-section">
      <h2 class="section-title">Server</h2>
      <div class="form-grid">
        <div class="form-row">
          <label for="s-port">Web UI port</label>
          <input id="s-port" class="search-input mono" bind:value={port} placeholder="8989" />
          <span class="field-hint">Requires restart to take effect</span>
        </div>
      </div>
    </div>

    <!-- Ansible -->
    <div class="card settings-section">
      <h2 class="section-title">Ansible</h2>
      <div class="form-grid">
        <div class="form-row">
          <label for="a-dir">Playbook directory</label>
          <input id="a-dir" class="search-input mono" bind:value={ansibleDir} placeholder="/etc/ansible" />
        </div>
        <div class="form-row">
          <label for="a-inv">Inventory file</label>
          <input id="a-inv" class="search-input mono" bind:value={ansibleInventory} placeholder="/etc/ansible/hosts" />
        </div>
      </div>
    </div>

    <!-- Auth / SSO -->
    <div class="card settings-section">
      <h2 class="section-title">Authentication & SSO</h2>
      <div class="auth-info">
        {#if whoami?.pam_available}
          <span class="badge badge-green">PAM</span>
          <span style="font-size:0.82rem;color:var(--text-secondary)">Full PAM — supports LDAP, 2FA, system accounts</span>
        {:else}
          <span class="badge badge-yellow">Shadow</span>
          <span style="font-size:0.82rem;color:var(--text-secondary)">System auth via /etc/shadow — SSSD/LDAP also works if configured via PAM/NSS</span>
        {/if}
      </div>
      <div class="form-grid" style="margin-top:1rem">
        <div class="form-row" style="grid-column:1/-1">
          <label for="bypass-token">SSO bypass token</label>
          <div style="display:flex;gap:0.5rem">
            <input id="bypass-token" class="search-input mono" style="flex:1"
              type={showBypassToken ? 'text' : 'password'}
              bind:value={bypassToken}
              placeholder="Leave blank to disable" />
            <button class="btn btn-ghost" onclick={() => showBypassToken = !showBypassToken}>{showBypassToken ? 'Hide' : 'Show'}</button>
            <button class="btn btn-ghost" onclick={generateToken}>Generate</button>
          </div>
          {#if bypassToken}
            <div class="field-hint">
              Send header: <code class="mono" style="font-size:0.68rem">X-Webux-Token: {bypassToken}</code>
            </div>
          {:else}
            <span class="field-hint">Your SSO system must pass the token via the <code class="mono">X-Webux-Token</code> request header</span>
          {/if}
        </div>
      </div>
    </div>

    <!-- Health Checks -->
    <div class="card settings-section">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.75rem">
        <h2 class="section-title" style="margin:0">Health Checks</h2>
        <div style="display:flex;gap:0.5rem">
          <button class="btn btn-ghost" onclick={resetHealthChecks} style="font-size:0.75rem">Reset to defaults</button>
          <button class="btn btn-ghost" onclick={() => showAddCheck = true} style="font-size:0.75rem">+ Add check</button>
          <button class="btn btn-primary" onclick={saveHealthChecks} disabled={healthSaving} style="font-size:0.75rem">
            {healthSaving ? 'Saving…' : 'Save checks'}
          </button>
        </div>
      </div>

      <div style="font-size:0.75rem;color:var(--text-tertiary);margin-bottom:0.75rem">
        Each check runs a shell command — exit code 0 = pass, non-zero = fail. The detail command provides expanded info shown on click.
      </div>

      <!-- Check list -->
      <div class="check-editor-list">
        {#each healthChecks as check, i (check.id)}
          <div class="check-editor-row" class:disabled={!check.enabled}>
            <div class="check-editor-main">
              <label class="toggle-label" style="margin-right:0.5rem">
                <input type="checkbox" checked={check.enabled} onchange={() => toggleCheck(check.id)} />
              </label>
              <div class="check-editor-info">
                <span class="mono" style="font-weight:600;font-size:0.82rem">{check.label}</span>
                <span style="font-size:0.72rem;color:var(--text-tertiary)">{check.description}</span>
              </div>
              <div class="check-editor-actions">
                <button class="btn btn-ghost" style="font-size:0.7rem"
                  onclick={() => editingCheck = editingCheck?.id === check.id ? null : { ...check, _idx: i }}>
                  {editingCheck?.id === check.id ? 'Done' : 'Edit'}
                </button>
                <button class="btn btn-ghost" style="font-size:0.7rem;color:var(--red)"
                  onclick={() => removeCheck(check.id)}>✕</button>
              </div>
            </div>

            {#if editingCheck?.id === check.id}
              <div class="check-editor-form">
                <div class="form-row">
                  <label>Check command (exit 0 = pass)</label>
                  <textarea class="search-input mono" rows="2" style="font-size:0.75rem;resize:vertical"
                    bind:value={editingCheck.command}
                    onblur={() => { healthChecks = healthChecks.map(c => c.id === editingCheck.id ? { ...editingCheck } : c); }}
                  ></textarea>
                </div>
                <div class="form-row">
                  <label>Detail command (shown on expand)</label>
                  <textarea class="search-input mono" rows="2" style="font-size:0.75rem;resize:vertical"
                    bind:value={editingCheck.detail_command}
                    onblur={() => { healthChecks = healthChecks.map(c => c.id === editingCheck.id ? { ...editingCheck } : c); }}
                  ></textarea>
                </div>
                <div class="form-row">
                  <label>Description</label>
                  <input class="search-input" bind:value={editingCheck.description}
                    onblur={() => { healthChecks = healthChecks.map(c => c.id === editingCheck.id ? { ...editingCheck } : c); }}
                  />
                </div>
              </div>
            {/if}
          </div>
        {/each}
      </div>

      <!-- Add check modal -->
      {#if showAddCheck}
        <div class="add-check-form">
          <div class="form-grid">
            <div class="form-row">
              <label>ID (unique, no spaces)</label>
              <input class="search-input mono" bind:value={newCheck.id} placeholder="my_check" />
            </div>
            <div class="form-row">
              <label>Label</label>
              <input class="search-input" bind:value={newCheck.label} placeholder="My check" />
            </div>
            <div class="form-row" style="grid-column:1/-1">
              <label>Description</label>
              <input class="search-input" bind:value={newCheck.description} placeholder="What this checks" />
            </div>
            <div class="form-row" style="grid-column:1/-1">
              <label>Check command (exit 0 = pass)</label>
              <textarea class="search-input mono" rows="2" style="font-size:0.75rem"
                bind:value={newCheck.command} placeholder="test -f /etc/myapp/config"></textarea>
            </div>
            <div class="form-row" style="grid-column:1/-1">
              <label>Detail command (optional)</label>
              <textarea class="search-input mono" rows="2" style="font-size:0.75rem"
                bind:value={newCheck.detail_command} placeholder="cat /etc/myapp/config | head -5"></textarea>
            </div>
          </div>
          <div style="display:flex;gap:0.5rem;margin-top:0.75rem">
            <button class="btn btn-primary" onclick={addCheck}
              disabled={!newCheck.id || !newCheck.label || !newCheck.command}>
              Add check
            </button>
            <button class="btn btn-ghost" onclick={() => showAddCheck = false}>Cancel</button>
          </div>
        </div>
      {/if}
    </div>

    <!-- About -->
    <div class="card settings-section">
      <h2 class="section-title">About</h2>
      <div class="about-grid">
        <div class="about-item"><span class="about-label">Auth backend</span><code class="mono about-val">{whoami?.auth_backend ?? '—'}</code></div>
        <div class="about-item"><span class="about-label">License</span><span class="about-val">AGPL-3.0</span></div>
        <div class="about-item"><span class="about-label">Source</span><a href="https://github.com/brendan4linux/webux" target="_blank" rel="noreferrer" class="about-val">github.com/brendan4linux/webux</a></div>
      </div>
    </div>
  {/if}
</div>

<style>
.settings-page { max-width: 800px; padding-bottom: 220px; }
.settings-section { margin-bottom: 0.75rem; padding: 1.25rem; }
.section-title { font-size: 0.9rem; font-weight: 600; margin-bottom: 1rem; color: var(--text-primary); border-bottom: 1px solid var(--border-subtle); padding-bottom: 0.5rem; }
.form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0.875rem; }
.form-row { display: flex; flex-direction: column; gap: 0.3rem; }
.form-row label { font-size: 0.75rem; color: var(--text-secondary); font-weight: 500; }
.field-hint { font-size: 0.7rem; color: var(--text-tertiary); margin-top: 0.2rem; }
.auth-info { display: flex; align-items: center; gap: 0.625rem; padding: 0.75rem; background: var(--bg-raised); border-radius: var(--r-md); }
.toggle-label { display: flex; align-items: center; gap: 0.4rem; font-size: 0.85rem; cursor: pointer; }
.about-grid { display: flex; flex-direction: column; gap: 0.5rem; }
.about-item { display: flex; gap: 1rem; font-size: 0.85rem; }
.about-label { color: var(--text-tertiary); width: 120px; flex-shrink: 0; }
.about-val { color: var(--text-secondary); }

/* Health check editor */
.check-editor-list { display: flex; flex-direction: column; gap: 0.25rem; }
.check-editor-row { background: var(--bg-raised); border-radius: var(--r-md); border: 1px solid var(--border-subtle); overflow: hidden; }
.check-editor-row.disabled { opacity: 0.55; }
.check-editor-main { display: flex; align-items: center; gap: 0.625rem; padding: 0.625rem 0.75rem; }
.check-editor-info { display: flex; flex-direction: column; gap: 2px; flex: 1; }
.check-editor-actions { display: flex; gap: 0.25rem; flex-shrink: 0; }
.check-editor-form { border-top: 1px solid var(--border-subtle); padding: 0.75rem; display: flex; flex-direction: column; gap: 0.625rem; background: var(--bg-base); }
.add-check-form { margin-top: 0.75rem; background: var(--bg-raised); border-radius: var(--r-md); padding: 1rem; border: 1px solid var(--border-default); }
</style>
