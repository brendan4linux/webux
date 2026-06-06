<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface User {
    username: string;
    uid: number;
    gid: number;
    gecos: string;
    home: string;
    shell: string;
    groups: string[];
    primary_group: string;
    locked: boolean;
    is_system: boolean;
    has_password: boolean;
  }

  interface Group {
    name: string;
    gid: number;
    members: string[];
  }

  type Tab = 'users' | 'groups';

  let tab = $state<Tab>('users');
  let users: User[] = $state([]);
  let groups: Group[] = $state([]);
  let shells: string[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let filterText = $state('');
  let showSystem = $state(false);
  let showCreateUser = $state(false);
  let showCreateGroup = $state(false);
  let actionPending = $state<string | null>(null);

  // Create user form state
  let newUser = $state({ username:'', full_name:'', shell:'/bin/bash', groups:[] as string[], password:'' });
  let newGroup = $state({ name:'', gid:0 });

  async function loadUsers() {
    loading = true; error = '';
    try {
      const [uRes, gRes, sRes] = await Promise.all([
        api.get<any>(`/api/users?system=${showSystem}`),
        api.get<any>('/api/groups'),
        api.get<any>('/api/users/shells'),
      ]);
      users = uRes.users ?? [];
      groups = gRes.groups ?? [];
      shells = sRes.shells ?? [];
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function lockToggle(u: User) {
    actionPending = u.username;
    try {
      const action = u.locked ? 'unlock' : 'lock';
      await api.post(`/api/users/${u.username}/${action}`, {});
      await loadUsers();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  async function deleteUser(username: string) {
    if (!confirm(`Delete user "${username}"? This cannot be undone.`)) return;
    actionPending = username;
    try {
      await api.delete(`/api/users/${username}`);
      await loadUsers();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  async function createUser() {
    actionPending = 'create';
    try {
      await api.post('/api/users', newUser);
      showCreateUser = false;
      newUser = { username:'', full_name:'', shell:'/bin/bash', groups:[], password:'' };
      await loadUsers();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  async function createGroup() {
    actionPending = 'create-group';
    try {
      await api.post('/api/groups', newGroup);
      showCreateGroup = false;
      newGroup = { name:'', gid:0 };
      await loadUsers();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  let filteredUsers = $derived(users.filter(u => {
    if (!filterText) return true;
    const t = filterText.toLowerCase();
    return u.username.toLowerCase().includes(t)
      || u.gecos?.toLowerCase().includes(t)
      || String(u.uid).includes(t);
  }));

  let filteredGroups = $derived(groups.filter(g => {
    if (!filterText) return true;
    const t = filterText.toLowerCase();
    return g.name.toLowerCase().includes(t) || String(g.gid).includes(t);
  }));

  onMount(loadUsers);
</script>

<div class="users-page">
  <div class="page-header">
    <div>
      <h1>Users &amp; Groups</h1>
      <p class="subtitle">
        {tab === 'users' ? filteredUsers.length + ' users' : filteredGroups.length + ' groups'}
      </p>
    </div>
    <div class="actions">
      {#if tab === 'users'}
        <label class="sys-toggle">
          <input type="checkbox" bind:checked={showSystem}
            onchange={loadUsers} />
          <span>Show system</span>
        </label>
        <button class="btn btn-primary" onclick={() => showCreateUser = true}>+ New user</button>
      {:else}
        <button class="btn btn-primary" onclick={() => showCreateGroup = true}>+ New group</button>
      {/if}
      <button class="btn" onclick={loadUsers} disabled={loading}>⟳</button>
    </div>
  </div>

  {#if error}
    <div class="alert alert-error" style="margin-bottom:1rem">{error}</div>
  {/if}

  <!-- Tab switcher -->
  <div class="tab-bar">
    <button class="tab-btn" class:active={tab === 'users'}
      onclick={() => tab = 'users'}>Users</button>
    <button class="tab-btn" class:active={tab === 'groups'}
      onclick={() => tab = 'groups'}>Groups</button>
  </div>

  <div class="filter-bar" style="margin-bottom:0.75rem">
    <input class="search-input" style="max-width:280px" type="search"
      placeholder="Filter…" bind:value={filterText} />
  </div>

  <!-- Users tab -->
  {#if tab === 'users'}
    <div class="card" style="padding:0;overflow-x:auto">
      <table class="data-table">
        <thead>
          <tr>
            <th>Username</th>
            <th>UID / GID</th>
            <th>Full name</th>
            <th>Home</th>
            <th>Shell</th>
            <th>Groups</th>
            <th>Status</th>
            <th style="text-align:right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {#if loading}
            {#each [1,2,3] as _}
              <tr>{#each [1,2,3,4,5,6,7,8] as _}
                <td><div class="skeleton" style="height:13px;width:80%"></div></td>
              {/each}</tr>
            {/each}
          {:else if filteredUsers.length === 0}
            <tr><td colspan="8" style="text-align:center;padding:2rem;color:var(--text-tertiary)">
              No users found
            </td></tr>
          {:else}
            {#each filteredUsers as u (u.username)}
              <tr class="user-row">
                <td>
                  <div class="mono" style="font-weight:600">{u.username}</div>
                </td>
                <td class="mono" style="font-size:0.72rem;color:var(--text-tertiary)">
                  {u.uid} / {u.gid}
                </td>
                <td style="color:var(--text-secondary);font-size:0.8rem">{u.gecos || '—'}</td>
                <td class="mono" style="font-size:0.72rem;color:var(--text-secondary)">{u.home}</td>
                <td class="mono" style="font-size:0.72rem">{u.shell}</td>
                <td>
                  <div class="group-pills">
                    {#if u.primary_group}
                      <span class="badge badge-blue">{u.primary_group}</span>
                    {/if}
                    {#each (u.groups ?? []).slice(0,3) as g}
                      <span class="badge badge-gray">{g}</span>
                    {/each}
                    {#if (u.groups?.length ?? 0) > 3}
                      <span class="badge badge-gray">+{u.groups.length - 3}</span>
                    {/if}
                  </div>
                </td>
                <td>
                  <div style="display:flex;gap:0.25rem;flex-wrap:wrap">
                    {#if u.locked}
                      <span class="badge badge-red">locked</span>
                    {:else}
                      <span class="badge badge-green">active</span>
                    {/if}
                    {#if !u.has_password}
                      <span class="badge badge-yellow">no pw</span>
                    {/if}
                  </div>
                </td>
                <td>
                  <div class="user-actions">
                    <button class="btn btn-ghost" style="font-size:0.72rem;padding:0.2rem 0.4rem"
                      disabled={actionPending === u.username}
                      onclick={() => lockToggle(u)}>
                      {u.locked ? '🔓 Unlock' : '🔒 Lock'}
                    </button>
                    <button class="btn btn-ghost btn-danger-hover" style="font-size:0.72rem;padding:0.2rem 0.4rem"
                      disabled={actionPending === u.username}
                      onclick={() => deleteUser(u.username)}>
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>

  <!-- Groups tab -->
  {:else}
    <div class="card" style="padding:0;overflow-x:auto">
      <table class="data-table">
        <thead>
          <tr>
            <th>Group name</th>
            <th>GID</th>
            <th>Members</th>
          </tr>
        </thead>
        <tbody>
          {#if loading}
            {#each [1,2,3] as _}
              <tr>{#each [1,2,3] as _}
                <td><div class="skeleton" style="height:13px;width:80%"></div></td>
              {/each}</tr>
            {/each}
          {:else if filteredGroups.length === 0}
            <tr><td colspan="3" style="text-align:center;padding:2rem;color:var(--text-tertiary)">
              No groups found
            </td></tr>
          {:else}
            {#each filteredGroups as g (g.name)}
              <tr>
                <td class="mono" style="font-weight:600">{g.name}</td>
                <td class="mono" style="color:var(--text-tertiary)">{g.gid}</td>
                <td>
                  <div class="group-pills">
                    {#each (g.members ?? []).slice(0,8) as m}
                      <span class="badge badge-gray">{m}</span>
                    {/each}
                    {#if (g.members?.length ?? 0) > 8}
                      <span class="badge badge-gray">+{g.members.length - 8} more</span>
                    {/if}
                    {#if !g.members?.length}
                      <span style="color:var(--text-tertiary);font-size:0.75rem">—</span>
                    {/if}
                  </div>
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  {/if}

  <!-- Create user modal -->
  {#if showCreateUser}
    <div class="modal-overlay" role="button" tabindex="0" onkeydown={(e)=>e.key==="Escape"&&(showCreateUser=false)} onclick={() => showCreateUser = false}>
      <div class="modal" role="dialog" onclick={(e) => e.stopPropagation()}>
        <div class="modal-header">
          <h2>Create user</h2>
          <button class="btn btn-ghost" onclick={() => showCreateUser = false}>✕</button>
        </div>
        <div class="modal-body">
          <div class="form-row">
            <label for="nu-username">Username *</label>
            <input id="nu-username" class="search-input" bind:value={newUser.username} placeholder="johndoe" />
          </div>
          <div class="form-row">
            <label for="nu-fullname">Full name</label>
            <input id="nu-fullname" class="search-input" bind:value={newUser.full_name} placeholder="John Doe" />
          </div>
          <div class="form-row">
            <label for="nu-password">Password</label>
            <input id="nu-password" class="search-input" type="password" bind:value={newUser.password} placeholder="Leave blank for no password" />
          </div>
          <div class="form-row">
            <label for="nu-shell">Shell</label>
            <select id="nu-shell" class="search-input" bind:value={newUser.shell}>
              {#each shells as sh}
                <option value={sh}>{sh}</option>
              {/each}
            </select>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn" onclick={() => showCreateUser = false}>Cancel</button>
          <button class="btn btn-primary" onclick={createUser}
            disabled={!newUser.username || actionPending === 'create'}>
            {actionPending === 'create' ? 'Creating…' : 'Create user'}
          </button>
        </div>
      </div>
    </div>
  {/if}

  <CLIEchoPane context="users" />
</div>

<style>
.users-page { max-width:1200px; padding-bottom:220px; }

.tab-bar { display:flex; border-bottom:1px solid var(--border-subtle); margin-bottom:0.75rem; }
.tab-btn { padding:0.5rem 1rem; background:none; border:none; border-bottom:2px solid transparent; cursor:pointer; font-size:0.85rem; color:var(--text-secondary); margin-bottom:-1px; }
.tab-btn.active { color:var(--accent); border-bottom-color:var(--accent); font-weight:500; }

.sys-toggle { display:flex; align-items:center; gap:0.375rem; font-size:0.78rem; color:var(--text-secondary); cursor:pointer; }

.user-row:hover { background:var(--bg-hover); }
.group-pills { display:flex; flex-wrap:wrap; gap:0.25rem; }

.user-actions { display:flex; gap:0.25rem; justify-content:flex-end; }
.btn-danger-hover:hover { color:var(--red); border-color:var(--red); background:var(--red-dim); }

.modal-overlay {
  position:fixed; inset:0; background:rgba(0,0,0,0.6);
  display:flex; align-items:center; justify-content:center; z-index:500;
}
.modal {
  background:var(--bg-panel); border:1px solid var(--border-default);
  border-radius:var(--r-lg); width:420px; max-width:95vw;
}
.modal-header { display:flex; justify-content:space-between; align-items:center; padding:1rem; border-bottom:1px solid var(--border-subtle); }
.modal-header h2 { font-size:1rem; margin:0; }
.modal-body { padding:1rem; display:flex; flex-direction:column; gap:0.75rem; }
.modal-footer { display:flex; justify-content:flex-end; gap:0.5rem; padding:1rem; border-top:1px solid var(--border-subtle); }
.form-row { display:flex; flex-direction:column; gap:0.25rem; }
.form-row label { font-size:0.75rem; color:var(--text-secondary); font-weight:500; }
.form-row select { background:var(--bg-raised); color:var(--text-primary); }
</style>
