<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface CronJob {
    id: string; owner: string; schedule: string; command: string;
    comment: string; source_type: string; source_file: string;
    line_number: number; enabled: boolean;
  }
  interface Schedule { label: string; value: string; }

  let jobs: CronJob[] = $state([]);
  let schedules: Schedule[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let filterText = $state('');
  let showAdd = $state(false);
  let addError = $state('');
  let adding = $state(false);

  // Edit state
  let editingId = $state<string|null>(null);
  let editSchedule = $state('');
  let editCommand = $state('');
  let editOwner = $state('');
  let editCustom = $state(false);
  let editValid = $state(true);
  let editError = $state('');
  let saving = $state(false);

  let newJob = $state({ owner: 'root', schedule: '0 * * * *', command: '', comment: '' });
  let customSchedule = $state(false);
  let scheduleValid = $state(true);

  async function load() {
    loading = true; error = '';
    try {
      const [jRes, sRes] = await Promise.all([
        api.get<any>('/api/cron'),
        api.get<any>('/api/cron/schedules'),
      ]);
      jobs = jRes.jobs ?? [];
      schedules = sRes.schedules ?? [];
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function validateSchedule(expr: string): Promise<boolean> {
    try {
      const res = await api.post<any>('/api/cron/validate', { schedule: expr });
      return res.valid;
    } catch { return false; }
  }

  async function addJob() {
    if (!newJob.command) { addError = 'Command is required'; return; }
    adding = true; addError = '';
    try {
      await api.post('/api/cron', newJob);
      showAdd = false;
      newJob = { owner: 'root', schedule: '0 * * * *', command: '', comment: '' };
      await load();
    } catch(e: any) { addError = e.message; }
    finally { adding = false; }
  }

  async function deleteJob(job: CronJob) {
    if (!confirm(`Delete: ${job.command}?`)) return;
    try {
      await api.delete('/api/cron', job as any);
      await load();
    } catch(e: any) { error = e.message; }
  }

  function startEdit(job: CronJob) {
    editingId = job.id;
    editSchedule = job.schedule;
    editCommand = job.command;
    editOwner = job.owner;
    editCustom = true; // always custom when editing
    editError = '';
    editValid = true;
  }

  function cancelEdit() {
    editingId = null;
    editError = '';
  }

  async function saveEdit(job: CronJob) {
    if (!editCommand) { editError = 'Command is required'; return; }
    editValid = await validateSchedule(editSchedule);
    if (!editValid) { editError = 'Invalid schedule expression'; return; }
    saving = true; editError = '';
    try {
      await api.put('/api/cron', {
        old: job,
        updated: {
          ...job,
          schedule: editSchedule,
          command: editCommand,
          owner: editOwner,
        }
      });
      editingId = null;
      await load();
    } catch(e: any) { editError = e.message; }
    finally { saving = false; }
  }

  function sourceLabel(type: string) {
    if (type === '/etc/crontab') return '/etc/crontab';
    if (type === 'cron.d') return '/etc/cron.d';
    if (type === 'user-spool') return 'user crontab';
    return type;
  }

  function sourceClass(type: string) {
    if (type === '/etc/crontab') return 'badge-blue';
    if (type === 'cron.d') return 'badge-purple';
    return 'badge-gray';
  }

  let filtered = $derived(jobs.filter(j => {
    if (!filterText) return true;
    const t = filterText.toLowerCase();
    return j.command.toLowerCase().includes(t)
      || j.owner?.toLowerCase().includes(t)
      || j.schedule.toLowerCase().includes(t)
      || j.comment?.toLowerCase().includes(t);
  }));

  onMount(load);
</script>

<div class="cron-page">
  <div class="page-header">
    <div>
      <h1>Cron Jobs</h1>
      <p class="subtitle">{filtered.length} of {jobs.length} jobs</p>
    </div>
    <div class="actions">
      <button class="btn btn-primary" onclick={() => { showAdd = !showAdd; editingId = null; }}>
        {showAdd ? '✕ Cancel' : '+ Add job'}
      </button>
      <button class="btn" onclick={load} disabled={loading}>⟳ Refresh</button>
    </div>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  <!-- Add job panel -->
  {#if showAdd}
    <div class="card add-panel">
      <h3 style="margin-bottom:1rem">New cron job</h3>
      {#if addError}<div class="alert alert-error" style="margin-bottom:0.75rem">{addError}</div>{/if}
      <div class="form-grid">
        <div class="form-row">
          <label for="cj-owner">Run as</label>
          <input id="cj-owner" class="search-input" bind:value={newJob.owner} placeholder="root" />
        </div>
        <div class="form-row" style="grid-column:1/-1">
          <label for="cj-schedule">Schedule</label>
          <div style="display:flex;gap:0.5rem;align-items:center">
            {#if !customSchedule}
              <select class="search-input" style="flex:1;background:var(--bg-raised);color:var(--text-primary)"
                bind:value={newJob.schedule}>
                {#each schedules as s}
                  <option value={s.value}>{s.label} ({s.value})</option>
                {/each}
              </select>
            {:else}
              <input class="search-input mono" style="flex:1" bind:value={newJob.schedule}
                placeholder="* * * * *" />
            {/if}
            <button class="btn btn-ghost" onclick={() => customSchedule = !customSchedule}>
              {customSchedule ? 'Preset' : 'Custom'}
            </button>
          </div>
          <div style="font-size:0.72rem;color:var(--text-tertiary);margin-top:0.25rem">
            min · hour · day · month · weekday
          </div>
        </div>
        <div class="form-row" style="grid-column:1/-1">
          <label for="cj-cmd">Command *</label>
          <input id="cj-cmd" class="search-input mono" bind:value={newJob.command}
            placeholder="/usr/bin/backup.sh >> /var/log/backup.log 2>&1" />
        </div>
        <div class="form-row" style="grid-column:1/-1">
          <label for="cj-comment">Comment</label>
          <input id="cj-comment" class="search-input" bind:value={newJob.comment}
            placeholder="Optional description" />
        </div>
      </div>
      <div style="margin-top:0.75rem;display:flex;gap:0.5rem">
        <button class="btn btn-primary" onclick={addJob} disabled={adding || !newJob.command}>
          {adding ? 'Adding…' : 'Add job'}
        </button>
        <button class="btn btn-ghost" onclick={() => showAdd = false}>Cancel</button>
      </div>
    </div>
  {/if}

  <div style="margin-bottom:0.75rem">
    <input class="search-input" style="max-width:320px" type="search"
      placeholder="Filter by command, owner, schedule…" bind:value={filterText} />
  </div>

  <div class="card" style="padding:0">
    <table class="data-table">
      <thead>
        <tr>
          <th>Schedule</th>
          <th>Command</th>
          <th>Owner</th>
          <th>Source</th>
          <th>Comment</th>
          <th style="text-align:right">Actions</th>
        </tr>
      </thead>
      <tbody>
        {#if loading}
          {#each [1,2,3,4] as _}
            <tr>{#each [1,2,3,4,5,6] as _}<td><div class="skeleton" style="height:14px;width:80%"></div></td>{/each}</tr>
          {/each}
        {:else if filtered.length === 0}
          <tr><td colspan="6" style="text-align:center;padding:2rem;color:var(--text-tertiary)">
            {jobs.length === 0 ? 'No cron jobs found' : 'No jobs match filter'}
          </td></tr>
        {:else}
          {#each filtered as job (job.id)}
            {#if editingId === job.id}
              <!-- ── Inline edit row ── -->
              <tr class="edit-row">
                <td>
                  <input class="search-input mono edit-input"
                    bind:value={editSchedule}
                    placeholder="* * * * *"
                    style="min-width:130px" />
                  <div class="sched-hint">min hr day mon wday</div>
                  <!-- preset picker -->
                  <select class="search-input" style="margin-top:0.3rem;font-size:0.72rem;background:var(--bg-raised);color:var(--text-primary)"
                    onchange={(e) => editSchedule = (e.target as HTMLSelectElement).value}>
                    <option value="">— preset —</option>
                    {#each schedules as s}
                      <option value={s.value}>{s.label}</option>
                    {/each}
                  </select>
                </td>
                <td colspan="2">
                  <input class="search-input mono edit-input"
                    bind:value={editCommand}
                    placeholder="command…"
                    style="width:100%" />
                  <input class="search-input edit-input"
                    bind:value={editOwner}
                    placeholder="owner"
                    style="margin-top:0.3rem;width:120px;font-size:0.78rem" />
                  {#if editError}
                    <div style="color:var(--red);font-size:0.72rem;margin-top:0.25rem">{editError}</div>
                  {/if}
                </td>
                <td>
                  <span class="badge {sourceClass(job.source_type)}" style="font-size:0.7rem">
                    {sourceLabel(job.source_type)}
                  </span>
                  <div style="font-size:0.7rem;color:var(--text-tertiary);margin-top:0.25rem">
                    line {job.line_number}
                  </div>
                </td>
                <td style="color:var(--text-tertiary);font-size:0.8rem">{job.comment || '—'}</td>
                <td>
                  <div style="display:flex;flex-direction:column;gap:0.3rem;align-items:flex-end">
                    <button class="btn btn-primary" style="font-size:0.78rem"
                      disabled={saving} onclick={() => saveEdit(job)}>
                      {saving ? '…' : '✓ Save'}
                    </button>
                    <button class="btn btn-ghost" style="font-size:0.78rem"
                      onclick={cancelEdit}>Cancel</button>
                  </div>
                </td>
              </tr>
            {:else}
              <!-- ── Normal display row ── -->
              <tr class="cron-row">
                <td>
                  <code class="schedule-badge">{job.schedule}</code>
                </td>
                <td>
                  <code class="mono" style="font-size:0.82rem;color:var(--text-primary);word-break:break-all">
                    {job.command}
                  </code>
                </td>
                <td class="mono" style="color:var(--text-secondary)">{job.owner || '—'}</td>
                <td>
                  <span class="badge {sourceClass(job.source_type)}" style="font-size:0.7rem">
                    {sourceLabel(job.source_type)}
                  </span>
                </td>
                <td style="color:var(--text-secondary)">{job.comment || '—'}</td>
                <td>
                  <div style="display:flex;gap:0.3rem;justify-content:flex-end">
                    <button class="btn btn-ghost" style="font-size:0.78rem"
                      onclick={() => startEdit(job)}>✎ Edit</button>
                    <button class="btn btn-ghost" style="font-size:0.78rem;color:var(--red)"
                      onclick={() => deleteJob(job)}>✕</button>
                  </div>
                </td>
              </tr>
            {/if}
          {/each}
        {/if}
      </tbody>
    </table>
  </div>

  <CLIEchoPane context="cron" />
</div>

<style>
.cron-page { max-width:1200px; padding-bottom:220px; }
.add-panel { margin-bottom:1rem; }
.form-grid { display:grid; grid-template-columns:1fr 1fr; gap:0.75rem; }
.form-row { display:flex; flex-direction:column; gap:0.3rem; }
.form-row label { font-size:0.75rem; color:var(--text-secondary); font-weight:500; }

.cron-row:hover { background:var(--bg-hover); }
.cron-row:hover td { background:transparent; }

.edit-row td { background:var(--bg-active); padding:0.75rem 0.875rem; vertical-align:top; }
.edit-row:hover td { background:var(--bg-active); }
.edit-input { font-size:0.82rem; padding:0.35rem 0.55rem; }

.sched-hint {
  font-size:0.62rem;
  color:var(--text-tertiary);
  font-family:var(--font-mono);
  margin-top:0.2rem;
  letter-spacing:0.04em;
}

.schedule-badge {
  display:inline-block;
  padding:0.18rem 0.55rem;
  background:var(--bg-raised);
  border:1px solid var(--border-default);
  border-radius:var(--r-sm);
  font-family:var(--font-mono);
  font-size:0.78rem;
  color:var(--accent);
  white-space:nowrap;
}
</style>
