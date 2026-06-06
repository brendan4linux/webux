<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface Var { name: string; default: string; prompt: boolean; private: boolean; }
  interface Play { name: string; hosts: string; vars: Var[]; }
  interface Playbook { path: string; name: string; plays: Play[]; description: string; tags: string[]; }
  interface Group { name: string; hosts: string[]; }

  let installed = $state(false);
  let version = $state('');
  let playbookDir = $state('/etc/ansible');
  let inventory = $state('/etc/ansible/hosts');
  let playbooks: Playbook[] = $state([]);
  let groups: Group[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let showSettings = $state(false);
  let installing = $state(false);
  let installOutput: string[] = $state([]);

  // Per-playbook run state
  let expandedPb = $state<string|null>(null);
  let runVars = $state<Record<string, Record<string, string>>>({});
  let runLimit = $state<Record<string, string>>({});
  let runTags = $state<Record<string, string>>({});
  let runCheck = $state<Record<string, boolean>>({});
  let runDiff = $state<Record<string, boolean>>({});
  let runOutput = $state<Record<string, string[]>>({});
  let running = $state<string|null>(null);

  async function load() {
    loading = true; error = '';
    try {
      const status = await api.get<any>('/api/ansible/status');
      installed = status.installed;
      version = status.version;
      playbookDir = status.playbook_dir;
      inventory = status.inventory;

      if (installed) {
        const [pbRes, invRes] = await Promise.all([
          api.get<any>('/api/ansible/playbooks'),
          api.get<any>('/api/ansible/inventory'),
        ]);
        playbooks = pbRes.playbooks ?? [];
        if (pbRes.error) error = pbRes.error;
        groups = invRes.groups ?? [];

        // Init var maps
        for (const pb of playbooks) {
          if (!runVars[pb.path]) {
            runVars[pb.path] = {};
            for (const play of pb.plays ?? []) {
              for (const v of play.vars ?? []) {
                runVars[pb.path][v.name] = v.default ?? '';
              }
            }
          }
        }
      }
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function saveSettings() {
    await api.put('/api/ansible/settings', { playbook_dir: playbookDir, inventory });
    showSettings = false;
    await load();
  }

  async function installAnsible() {
    installing = true; installOutput = [];
    try {
      const resp = await fetch('/api/ansible/install', { method: 'POST' });
      const reader = resp.body!.getReader();
      const dec = new TextDecoder(); let buf = '';
      while (true) {
        const { done, value } = await reader.read(); if (done) break;
        buf += dec.decode(value, { stream: true });
        const lines = buf.split('\n');
        for (const l of lines.slice(0, -1)) {
          if (l.startsWith('data: ')) installOutput = [...installOutput, l.slice(6)];
        }
        buf = lines[lines.length - 1];
      }
      await load();
    } catch(e: any) { error = String(e); }
    finally { installing = false; }
  }

  async function runPlaybook(pb: Playbook) {
    running = pb.path;
    runOutput = { ...runOutput, [pb.path]: [] };
    try {
      const resp = await fetch('/api/ansible/run', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          playbook_path: pb.path,
          inventory,
          limit:      runLimit[pb.path] ?? '',
          tags:       runTags[pb.path] ?? '',
          extra_vars: runVars[pb.path] ?? {},
          check:      runCheck[pb.path] ?? false,
          diff:       runDiff[pb.path] ?? false,
        }),
      });
      const reader = resp.body!.getReader();
      const dec = new TextDecoder(); let buf = '';
      while (true) {
        const { done, value } = await reader.read(); if (done) break;
        buf += dec.decode(value, { stream: true });
        const lines = buf.split('\n');
        for (const l of lines.slice(0, -1)) {
          if (l.startsWith('data: ')) runOutput = { ...runOutput, [pb.path]: [...(runOutput[pb.path]??[]), l.slice(6)] };
        }
        buf = lines[lines.length-1];
      }
    } catch(e: any) { runOutput = { ...runOutput, [pb.path]: [...(runOutput[pb.path]??[]), '[error] '+String(e)] }; }
    finally { running = null; }
  }

  function allVars(pb: Playbook): Var[] {
    const seen = new Set<string>();
    const vars: Var[] = [];
    for (const play of pb.plays ?? []) {
      for (const v of play.vars ?? []) {
        if (!seen.has(v.name)) { seen.add(v.name); vars.push(v); }
      }
    }
    return vars;
  }

  function lineClass(line: string) {
    if (line.includes('PLAY RECAP')) return 'line-recap';
    if (line.includes('ok=') && line.includes('changed=')) return 'line-recap';
    if (line.includes('fatal') || line.includes('FAILED') || line.includes('ERROR')) return 'line-err';
    if (line.includes('changed') || line.includes('CHANGED')) return 'line-changed';
    if (line.includes('ok') || line.includes('SUCCESS')) return 'line-ok';
    if (line.includes('PLAY [') || line.includes('TASK [')) return 'line-header';
    if (line.includes('skipping')) return 'line-skip';
    return '';
  }

  onMount(load);
</script>

<div class="ans-page">
  <div class="page-header">
    <div>
      <h1>Ansible</h1>
      {#if installed}
        <p class="subtitle mono">{version} · {playbooks.length} playbooks in {playbookDir}</p>
      {:else}
        <p class="subtitle">Not installed</p>
      {/if}
    </div>
    <div class="actions">
      <button class="btn btn-ghost" onclick={() => showSettings = !showSettings}>⚙ Settings</button>
      <button class="btn" onclick={load} disabled={loading}>⟳ Refresh</button>
    </div>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  <!-- Settings panel -->
  {#if showSettings}
    <div class="card settings-panel">
      <h3 style="margin-bottom:0.75rem">Ansible settings</h3>
      <div class="settings-grid">
        <div class="form-row">
          <label for="pb-dir">Playbook directory</label>
          <input id="pb-dir" class="search-input mono" bind:value={playbookDir} placeholder="/etc/ansible" />
        </div>
        <div class="form-row">
          <label for="inv-path">Inventory file</label>
          <input id="inv-path" class="search-input mono" bind:value={inventory} placeholder="/etc/ansible/hosts" />
        </div>
      </div>
      <div style="margin-top:0.75rem;display:flex;gap:0.5rem">
        <button class="btn btn-primary" onclick={saveSettings}>Save & rescan</button>
        <button class="btn btn-ghost" onclick={() => showSettings = false}>Cancel</button>
      </div>
    </div>
  {/if}

  {#if loading}
    <div class="card skeleton" style="height:80px"></div>
  {:else if !installed}
    <!-- Not installed card -->
    <div class="card not-installed">
      <div style="font-size:2rem;margin-bottom:1rem">▶</div>
      <h2>Ansible not detected</h2>
      <p>Install Ansible using your system's package manager to manage playbooks from Webux.</p>

      {#if installOutput.length > 0}
        <div class="install-output">
          {#each installOutput as line}
            <div class="mono" style="font-size:0.72rem;color:var(--text-secondary)">{line}</div>
          {/each}
        </div>
      {/if}

      <button class="btn btn-primary" onclick={installAnsible} disabled={installing} style="margin-top:1rem">
        {installing ? '⟳ Installing…' : '⬇ Install Ansible'}
      </button>
    </div>

  {:else if playbooks.length === 0}
    <div class="card" style="text-align:center;padding:2.5rem;color:var(--text-tertiary)">
      <p>No playbooks found in <code class="mono">{playbookDir}</code></p>
      <p style="margin-top:0.5rem;font-size:0.82rem">Change the directory in Settings, or create a <code>.yml</code> playbook file there.</p>
    </div>

  {:else}
    <!-- Inventory groups summary -->
    {#if groups.length > 0}
      <div class="groups-bar">
        <span style="font-size:0.72rem;color:var(--text-tertiary)">Inventory groups:</span>
        {#each groups as g}
          <span class="badge badge-gray">{g.name} ({g.hosts.length})</span>
        {/each}
      </div>
    {/if}

    <!-- Playbook list -->
    <div class="pb-list">
      {#each playbooks as pb (pb.path)}
        <div class="card pb-card">
          <!-- Summary row -->
          <div class="pb-header" onclick={() => expandedPb = expandedPb === pb.path ? null : pb.path}
            role="button" tabindex="0" onkeydown={(e) => e.key === 'Enter' && (expandedPb = expandedPb === pb.path ? null : pb.path)}>
            <div class="pb-info">
              <span class="mono pb-name">{pb.name}.yml</span>
              {#if pb.description && pb.description !== pb.name}
                <span class="pb-desc">{pb.description}</span>
              {/if}
              <div class="pb-meta">
                {#each (pb.plays ?? []) as play}
                  <span class="badge badge-blue">{play.hosts}</span>
                {/each}
                {#if allVars(pb).length > 0}
                  <span class="badge badge-purple">{allVars(pb).length} vars</span>
                {/if}
                {#each (pb.tags ?? []).slice(0,4) as tag}
                  <span class="badge badge-gray">{tag}</span>
                {/each}
              </div>
            </div>
            <div class="pb-actions" onclick={(e) => e.stopPropagation()}>
              <button class="btn btn-primary"
                disabled={running === pb.path}
                onclick={() => runPlaybook(pb)}>
                {running === pb.path ? '⟳ Running…' : '▶ Run'}
              </button>
              <span class="expand-chevron">{expandedPb === pb.path ? '▾' : '▸'}</span>
            </div>
          </div>

          <!-- Expanded run form -->
          {#if expandedPb === pb.path}
            <div class="pb-runform">
              <!-- Variable inputs -->
              {#if allVars(pb).length > 0}
                <div class="vars-section">
                  <div class="vars-label">Variables</div>
                  <div class="vars-grid">
                    {#each allVars(pb) as v}
                      <div class="form-row">
                        <label for="var-{pb.name}-{v.name}" title={v.prompt ? 'vars_prompt' : 'vars'}>
                          {v.name}
                          {#if v.prompt}<span class="badge badge-purple" style="font-size:0.6rem;margin-left:4px">prompt</span>{/if}
                        </label>
                        <input
                          id="var-{pb.name}-{v.name}"
                          class="search-input mono"
                          type={v.private ? 'password' : 'text'}
                          placeholder={v.default || '(empty)'}
                          bind:value={runVars[pb.path][v.name]}
                        />
                      </div>
                    {/each}
                  </div>
                </div>
              {/if}

              <!-- Run options -->
              <div class="run-options">
                <div class="form-row">
                  <label for="limit-{pb.name}">--limit</label>
                  <input id="limit-{pb.name}" class="search-input mono" style="max-width:200px"
                    bind:value={runLimit[pb.path]} placeholder="all hosts" />
                </div>
                <div class="form-row">
                  <label for="tags-{pb.name}">--tags</label>
                  <input id="tags-{pb.name}" class="search-input mono" style="max-width:200px"
                    bind:value={runTags[pb.path]} placeholder="all tags" />
                </div>
                <label class="toggle-label">
                  <input type="checkbox" bind:checked={runCheck[pb.path]} />
                  <span>--check (dry run)</span>
                </label>
                <label class="toggle-label">
                  <input type="checkbox" bind:checked={runDiff[pb.path]} />
                  <span>--diff</span>
                </label>
              </div>

              <!-- Output -->
              {#if runOutput[pb.path]?.length > 0}
                <div class="pb-output">
                  {#each runOutput[pb.path] as line}
                    <div class="out-line mono {lineClass(line)}">{line}</div>
                  {/each}
                  {#if running === pb.path}
                    <div class="out-line" style="color:var(--text-tertiary)">⟳ running…</div>
                  {/if}
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  <CLIEchoPane context="ansible" />
</div>

<style>
.ans-page { max-width:1100px; padding-bottom:220px; }
.settings-panel { margin-bottom:0.75rem; }
.settings-grid { display:grid; grid-template-columns:1fr 1fr; gap:0.75rem; }
.form-row { display:flex; flex-direction:column; gap:0.25rem; }
.form-row label { font-size:0.75rem; color:var(--text-secondary); font-weight:500; }

.not-installed { text-align:center; padding:3rem 2rem; }
.not-installed h2 { margin-bottom:0.5rem; }
.not-installed p { color:var(--text-secondary); font-size:0.875rem; }
.install-output { max-height:200px; overflow-y:auto; background:var(--bg-base); border-radius:var(--r-md); padding:0.75rem; margin-top:1rem; text-align:left; }

.groups-bar { display:flex; align-items:center; gap:0.375rem; flex-wrap:wrap; margin-bottom:0.75rem; }

.pb-list { display:flex; flex-direction:column; gap:0.5rem; }
.pb-card { padding:0; }

.pb-header {
  display:flex; align-items:center; justify-content:space-between;
  padding:0.875rem 1rem; cursor:pointer; gap:1rem;
}
.pb-header:hover { background:var(--bg-hover); }

.pb-info { display:flex; flex-direction:column; gap:0.25rem; min-width:0; }
.pb-name { font-weight:600; font-size:0.9rem; }
.pb-desc { font-size:0.78rem; color:var(--text-secondary); }
.pb-meta { display:flex; gap:0.25rem; flex-wrap:wrap; }

.pb-actions { display:flex; align-items:center; gap:0.75rem; flex-shrink:0; }
.expand-chevron { color:var(--text-tertiary); font-size:0.75rem; }

.pb-runform {
  border-top:1px solid var(--border-subtle);
  padding:1rem;
  display:flex; flex-direction:column; gap:0.875rem;
}

.vars-section { display:flex; flex-direction:column; gap:0.5rem; }
.vars-label { font-size:0.68rem; font-weight:500; color:var(--text-tertiary); text-transform:uppercase; letter-spacing:0.06em; }
.vars-grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(220px,1fr)); gap:0.625rem; }

.run-options { display:flex; align-items:flex-end; gap:1rem; flex-wrap:wrap; }
.toggle-label { display:flex; align-items:center; gap:0.375rem; font-size:0.82rem; color:var(--text-secondary); cursor:pointer; }

.pb-output { background:var(--bg-base); border-radius:var(--r-md); padding:0.625rem 0.875rem; max-height:300px; overflow-y:auto; }
.out-line { font-size:0.72rem; line-height:1.6; white-space:pre-wrap; word-break:break-all; color:var(--text-secondary); }
.out-line.line-ok      { color:var(--green); }
.out-line.line-err     { color:var(--red); }
.out-line.line-changed { color:var(--yellow); }
.out-line.line-header  { color:var(--accent); font-weight:600; }
.out-line.line-skip    { color:var(--text-tertiary); }
.out-line.line-recap   { color:var(--text-primary); font-weight:600; border-top:1px solid var(--border-subtle); margin-top:0.25rem; padding-top:0.25rem; }
</style>
