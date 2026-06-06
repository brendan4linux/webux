<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';

  interface Setting { key: string; value: string; }

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
  let puppetEnabled = $state(true);

  // Auth
  let bypassToken = $state('');
  let showBypassToken = $state(false);
  let whoami = $state<any>(null);

  async function load() {
    loading = true; error = '';
    try {
      // Load all settings at once
      const [whoRes, ansRes, portRes, bypassRes] = await Promise.all([
        fetch('/auth/whoami').then(r => r.ok ? r.json() : null).catch(() => null),
        api.get<any>('/api/ansible/status'),
        fetch('/api/system/info').then(r => r.json()).catch(() => null),
        fetch('/api/settings/auth').then(r => r.ok ? r.json() : null).catch(() => null),
      ]);

      whoami = whoRes;
      if (ansRes) {
        ansibleDir = ansRes.playbook_dir ?? '/etc/ansible';
        ansibleInventory = ansRes.inventory ?? '/etc/ansible/hosts';
      }
      if (bypassRes) {
        bypassToken = bypassRes.bypass_token ?? '';
      }

      // Read port from DB setting
      const portSetting = await fetch('/api/settings/server').then(r => r.ok ? r.json() : null).catch(() => null);
      if (portSetting?.port) port = portSetting.port;

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

  function generateToken() {
    const arr = new Uint8Array(32);
    crypto.getRandomValues(arr);
    bypassToken = Array.from(arr).map(b => b.toString(16).padStart(2,'0')).join('');
  }

  async function logout() {
    await fetch('/auth/logout', { method: 'POST' });
    window.location.href = '/';
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
    {#each [1,2,3] as _}
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

    <!-- Puppet -->
    <div class="card settings-section">
      <h2 class="section-title">Puppet</h2>
      <div class="form-grid">
        <div class="form-row">
          <label for="p-conf">Config directory</label>
          <input id="p-conf" class="search-input mono" bind:value={puppetConfDir}
            placeholder="/etc/puppetlabs/puppet" />
          <span class="field-hint">Used to locate puppet.conf and SSL certs</span>
        </div>
        <div class="form-row" style="justify-content:flex-end;padding-top:1.5rem">
          <label class="toggle-label">
            <input type="checkbox" bind:checked={puppetEnabled} />
            <span>Show Puppet menu item</span>
          </label>
        </div>
      </div>
    </div>

    <!-- SSO / Auth -->
    <div class="card settings-section">
      <h2 class="section-title">Authentication & SSO</h2>

      <div class="auth-info">
        {#if whoami?.pam_available}
          <span class="badge badge-green">PAM</span>
          <span style="font-size:0.82rem;color:var(--text-secondary)">Full PAM authentication — supports LDAP, 2FA, and system accounts</span>
        {:else}
          <span class="badge badge-yellow">Shadow</span>
          <span style="font-size:0.82rem;color:var(--text-secondary)">
            Using /etc/shadow — rebuild with <code class="mono">-tags pam</code> for full PAM support
          </span>
        {/if}
      </div>

      <div class="form-grid" style="margin-top:1rem">
        <div class="form-row" style="grid-column:1/-1">
          <label for="bypass-token">SSO bypass token</label>
          <div style="display:flex;gap:0.5rem">
            <input id="bypass-token" class="search-input mono" style="flex:1"
              type={showBypassToken ? 'text' : 'password'}
              bind:value={bypassToken}
              placeholder="Leave blank to disable bypass" />
            <button class="btn btn-ghost" onclick={() => showBypassToken = !showBypassToken}>
              {showBypassToken ? 'Hide' : 'Show'}
            </button>
            <button class="btn btn-ghost" onclick={generateToken}>Generate</button>
          </div>
          {#if bypassToken}
            <div class="field-hint">
              SSO URL: <code class="mono" style="font-size:0.72rem">
                http://yourserver:8989/auth/bypass?token={bypassToken}
              </code>
              <br>Or via header: <code class="mono" style="font-size:0.72rem">X-Webux-Token: {bypassToken}</code>
            </div>
          {:else}
            <span class="field-hint">When set, your SSO system can redirect to <code class="mono">/?token=&lt;token&gt;</code> to bypass login</span>
          {/if}
        </div>
      </div>
    </div>

    <!-- About -->
    <div class="card settings-section">
      <h2 class="section-title">About</h2>
      <div class="about-grid">
        <div class="about-item">
          <span class="about-label">Auth backend</span>
          <code class="mono about-val">{whoami?.auth_backend ?? '—'}</code>
        </div>
        <div class="about-item">
          <span class="about-label">License</span>
          <span class="about-val">AGPL-3.0</span>
        </div>
        <div class="about-item">
          <span class="about-label">Source</span>
          <a href="https://github.com/brendan4linux/webux" target="_blank" rel="noreferrer" class="about-val">
            github.com/brendan4linux/webux
          </a>
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
.settings-page { max-width:800px; padding-bottom:220px; }

.settings-section { margin-bottom:0.75rem; padding:1.25rem; }
.section-title { font-size:0.9rem; font-weight:600; margin-bottom:1rem; color:var(--text-primary); border-bottom:1px solid var(--border-subtle); padding-bottom:0.5rem; }

.form-grid { display:grid; grid-template-columns:1fr 1fr; gap:0.875rem; }
.form-row { display:flex; flex-direction:column; gap:0.3rem; }
.form-row label { font-size:0.75rem; color:var(--text-secondary); font-weight:500; }
.field-hint { font-size:0.7rem; color:var(--text-tertiary); margin-top:0.2rem; }

.auth-info { display:flex; align-items:center; gap:0.625rem; padding:0.75rem; background:var(--bg-raised); border-radius:var(--r-md); }
.toggle-label { display:flex; align-items:center; gap:0.4rem; font-size:0.85rem; cursor:pointer; }

.about-grid { display:flex; flex-direction:column; gap:0.5rem; }
.about-item { display:flex; gap:1rem; font-size:0.85rem; }
.about-label { color:var(--text-tertiary); width:120px; flex-shrink:0; }
.about-val { color:var(--text-secondary); }
</style>
