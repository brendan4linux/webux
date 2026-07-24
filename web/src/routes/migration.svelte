<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  type Format = 'markdown' | 'yaml' | 'ansible';

  interface HostSnapshot {
    captured_at: string;
    hostname: string;
    distro: string;
    kernel_version: string;
    arch: string;
    ports: any[];
    enabled_services: any[];
    databases: any[];
    webservers: any[];
    cron_jobs: any[];
    users: any[];
    firewall_rules: string[];
    ansible_inventories: string[];
    puppet_facts: Record<string, any> | null;
    systemd_sockets: any[];
  }

  let snapshot: HostSnapshot | null = $state(null);
  let preview = $state('');
  let format: Format = $state('markdown');
  let loading = $state(false);
  let capturing = $state(false);
  let error = $state('');
  let copied = $state(false);

  async function captureSnapshot() {
    capturing = true;
    error = '';
    try {
      snapshot = await api.get('/api/migration/snapshot');
      await loadPreview();
    } catch (e: any) {
      error = e.message;
    } finally {
      capturing = false;
    }
  }

  async function loadPreview() {
    if (!snapshot) return;
    loading = true;
    try {
      const res = await fetch(`/api/migration/template?format=${format}`);
      preview = await res.text();
    } catch (e: any) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  async function download() {
    const res = await fetch(`/api/migration/template?format=${format}`);
    const blob = await res.blob();
    const ext = { markdown: 'md', yaml: 'json', ansible: 'yml' }[format];
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `webux-migration-${snapshot?.hostname ?? 'host'}.${ext}`;
    a.click();
    URL.revokeObjectURL(url);
  }

  async function copyToClipboard() {
    await navigator.clipboard.writeText(preview);
    copied = true;
    setTimeout(() => copied = false, 2000);
  }

  $effect(() => { if (format && snapshot) loadPreview(); });

  onMount(() => {});
</script>

<div class="migration-page">
  <header class="page-header">
    <div>
      <h1>Migration Template</h1>
      <p class="subtitle">Capture everything that needs to move when you replace this host</p>
    </div>
    <a class="btn-secondary" href="#/ports">← Open Ports</a>
  </header>

  {#if error}
    <div class="error-banner">{error}</div>
  {/if}

  {#if !snapshot}
    <!-- Pre-capture state -->
    <div class="capture-card">
      <div class="capture-icon">🖥</div>
      <h2>Ready to snapshot <code>{window.location.hostname}</code></h2>
      <p>Webux will collect:</p>
      <ul class="collect-list">
        <li>All listening TCP/UDP ports and their owning processes</li>
        <li>systemd socket units and listen directives</li>
        <li>Enabled systemd services</li>
        <li>Detected databases (MySQL, Postgres, Redis…)</li>
        <li>Webserver configs and virtual hosts (Nginx, Apache, Caddy)</li>
        <li>System and per-user cron jobs</li>
        <li>Non-system users (UID ≥ 1000) and their groups</li>
        <li>Firewall rules (ufw / nftables / iptables)</li>
        <li>/etc/environment variables</li>
        <li>Ansible inventories and Puppet facts (if present)</li>
      </ul>
      <button class="btn-capture" onclick={captureSnapshot} disabled={capturing}>
        {capturing ? 'Capturing…' : '📸 Capture Snapshot'}
      </button>
      <p class="read-note">Read-only — Webux does not modify any system state during capture.</p>
    </div>
  {:else}
    <!-- Snapshot summary bar -->
    <div class="summary-bar">
      <div class="summary-item">
        <span class="summary-label">Host</span>
        <span class="summary-value">{snapshot.hostname}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">Distro</span>
        <span class="summary-value">{snapshot.distro}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">Arch</span>
        <span class="summary-value">{snapshot.arch}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">Open ports</span>
        <span class="summary-value count">{snapshot.ports?.length ?? 0}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">Services</span>
        <span class="summary-value count">{snapshot.enabled_services?.length ?? 0}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">Users</span>
        <span class="summary-value count">{snapshot.users?.length ?? 0}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">Cron jobs</span>
        <span class="summary-value count">{snapshot.cron_jobs?.length ?? 0}</span>
      </div>
      <div class="summary-item">
        <span class="summary-label">Captured</span>
        <span class="summary-value">{new Date(snapshot.captured_at).toLocaleTimeString()}</span>
      </div>
      <button class="btn-recapture" onclick={captureSnapshot} disabled={capturing}>
        {capturing ? '…' : '⟳ Recapture'}
      </button>
    </div>

    <!-- Section summaries -->
    <div class="sections-grid">
      {#if snapshot.databases?.length}
        <div class="section-card">
          <h3>Databases</h3>
          {#each snapshot.databases as db}
            <div class="section-item">
              <span class="section-badge db">{db.type}</span>
              <span>:{db.port}</span>
              {#if db.data_dir}<code class="data-dir">{db.data_dir}</code>{/if}
            </div>
          {/each}
        </div>
      {/if}

      {#if snapshot.webservers?.length}
        <div class="section-card">
          <h3>Webservers</h3>
          {#each snapshot.webservers as ws}
            <div class="section-item">
              <span class="section-badge ws">{ws.type}</span>
              <code>{ws.config_path}</code>
            </div>
            {#if ws.vhosts?.length}
              <div class="vhosts">
                {#each ws.vhosts as vh}<span class="vhost-pill">{vh}</span>{/each}
              </div>
            {/if}
          {/each}
        </div>
      {/if}

      {#if snapshot.ansible_inventories?.length || snapshot.puppet_facts}
        <div class="section-card">
          <h3>Config management</h3>
          {#each snapshot.ansible_inventories ?? [] as inv}
            <div class="section-item"><span class="section-badge ansible">Ansible</span><code>{inv}</code></div>
          {/each}
          {#if snapshot.puppet_facts}
            <div class="section-item"><span class="section-badge puppet">Puppet</span> facts captured</div>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Template preview -->
    <div class="template-panel">
      <div class="template-toolbar">
        <div class="format-tabs">
          {#each [['markdown', '📋 Markdown'], ['ansible', '⚙ Ansible'], ['yaml', '{ } YAML']] as [f, label]}
            <button
              class="format-tab"
              class:active={format === f}
              onclick={() => format = f as Format}
            >{label}</button>
          {/each}
        </div>
        <div class="toolbar-actions">
          <button class="btn-icon" onclick={copyToClipboard} title="Copy to clipboard">
            {copied ? '✓ Copied' : '📋 Copy'}
          </button>
          <button class="btn-icon" onclick={download} title="Download file">
            ⬇ Download
          </button>
        </div>
      </div>

      <div class="preview-wrap">
        {#if loading}
          <div class="preview-loading">Rendering template…</div>
        {:else}
          <pre class="preview-content">{preview}</pre>
        {/if}
      </div>
    </div>
  {/if}

  <CLIEchoPane context="migration" />
</div>

<style>
.migration-page { padding: 2rem; max-width: 1200px; }
.page-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 1.5rem; }
.page-header h1 { margin: 0 0 0.25rem; font-size: 1.5rem; font-weight: 600; }
.subtitle { margin: 0; font-size: 0.875rem; color: var(--color-text-secondary); }

.btn-secondary { padding: 0.5rem 1rem; background: var(--color-background-secondary); border: 1px solid var(--color-border-secondary); border-radius: 6px; cursor: pointer; font-size: 0.875rem; text-decoration: none; color: var(--color-text-primary); }
.error-banner { background: var(--color-background-danger); color: var(--color-text-danger); padding: 0.75rem 1rem; border-radius: 6px; margin-bottom: 1rem; font-size: 0.875rem; }

/* Pre-capture card */
.capture-card { max-width: 560px; margin: 3rem auto; text-align: center; padding: 2.5rem; border: 1px solid var(--color-border-tertiary); border-radius: 12px; background: var(--color-background-secondary); }
.capture-icon { font-size: 3rem; margin-bottom: 1rem; }
.capture-card h2 { margin: 0 0 1rem; font-size: 1.25rem; }
.capture-card p { color: var(--color-text-secondary); font-size: 0.875rem; margin: 0 0 0.5rem; }
.collect-list { text-align: left; font-size: 0.85rem; color: var(--color-text-secondary); line-height: 1.8; margin: 0 0 1.5rem; padding-left: 1.5rem; }
.btn-capture { padding: 0.75rem 2rem; background: var(--color-text-info); color: #fff; border: none; border-radius: 8px; cursor: pointer; font-size: 1rem; margin-bottom: 0.75rem; }
.btn-capture:disabled { opacity: 0.6; cursor: default; }
.read-note { font-size: 0.75rem; color: var(--color-text-secondary); margin: 0; }

/* Summary bar */
.summary-bar { display: flex; gap: 0; border: 1px solid var(--color-border-tertiary); border-radius: 8px; overflow: hidden; margin-bottom: 1.25rem; flex-wrap: wrap; }
.summary-item { flex: 1; min-width: 100px; padding: 0.75rem 1rem; border-right: 1px solid var(--color-border-tertiary); }
.summary-item:last-child { border-right: none; }
.summary-label { display: block; font-size: 0.7rem; font-weight: 500; color: var(--color-text-secondary); text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 0.25rem; }
.summary-value { font-size: 0.9rem; font-weight: 600; }
.count { color: var(--color-text-info); }
.btn-recapture { align-self: center; margin: 0.5rem; padding: 0.375rem 0.75rem; background: var(--color-background-secondary); border: 1px solid var(--color-border-secondary); border-radius: 6px; cursor: pointer; font-size: 0.8rem; white-space: nowrap; }

/* Section cards */
.sections-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 0.75rem; margin-bottom: 1.25rem; }
.section-card { padding: 1rem; border: 1px solid var(--color-border-tertiary); border-radius: 8px; background: var(--color-background-secondary); }
.section-card h3 { margin: 0 0 0.75rem; font-size: 0.875rem; font-weight: 600; color: var(--color-text-secondary); text-transform: uppercase; letter-spacing: 0.05em; }
.section-item { display: flex; align-items: center; gap: 0.5rem; margin-bottom: 0.4rem; font-size: 0.85rem; }
.section-badge { display: inline-block; padding: 0.1rem 0.4rem; border-radius: 4px; font-size: 0.7rem; font-weight: 600; }
.db { background: #dcfce7; color: #166534; }
.ws { background: #dbeafe; color: #1e40af; }
.ansible { background: #fef3c7; color: #92400e; }
.puppet { background: #ede9fe; color: #4c1d95; }
.data-dir { font-size: 0.75rem; color: var(--color-text-secondary); }
.vhosts { margin: 0.25rem 0 0.5rem 0; display: flex; flex-wrap: wrap; gap: 0.25rem; }
.vhost-pill { display: inline-block; padding: 0.1rem 0.4rem; background: var(--color-background-primary); border: 1px solid var(--color-border-secondary); border-radius: 4px; font-size: 0.75rem; font-family: monospace; }

/* Template panel */
.template-panel { border: 1px solid var(--color-border-tertiary); border-radius: 8px; overflow: hidden; }
.template-toolbar { display: flex; justify-content: space-between; align-items: center; padding: 0.5rem 0.75rem; background: var(--color-background-secondary); border-bottom: 1px solid var(--color-border-tertiary); }
.format-tabs { display: flex; gap: 0; }
.format-tab { padding: 0.35rem 0.75rem; background: transparent; border: 1px solid var(--color-border-secondary); border-right: none; cursor: pointer; font-size: 0.8rem; color: var(--color-text-secondary); }
.format-tab:first-child { border-radius: 6px 0 0 6px; }
.format-tab:last-child { border-right: 1px solid var(--color-border-secondary); border-radius: 0 6px 6px 0; }
.format-tab.active { background: var(--color-text-info); color: #fff; border-color: var(--color-text-info); }
.toolbar-actions { display: flex; gap: 0.5rem; }
.btn-icon { padding: 0.35rem 0.65rem; background: var(--color-background-primary); border: 1px solid var(--color-border-secondary); border-radius: 6px; cursor: pointer; font-size: 0.8rem; color: var(--color-text-primary); }
.preview-wrap { max-height: 60vh; overflow-y: auto; }
.preview-loading { padding: 2rem; text-align: center; color: var(--color-text-secondary); font-size: 0.875rem; }
.preview-content { margin: 0; padding: 1.25rem; font-family: monospace; font-size: 0.8rem; line-height: 1.6; white-space: pre-wrap; word-break: break-word; color: var(--color-text-primary); background: var(--color-background-primary); }
</style>
