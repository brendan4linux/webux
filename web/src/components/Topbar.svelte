<script lang="ts">
  import { wsStore } from '$lib/ws';

  let wsConnected = $state(false);
  let hostname = $state('');
  let username = $state('');

  $effect(() => {
    const unsub = wsStore.subscribe(evt => {
      if (evt) wsConnected = true;
    });
    return unsub;
  });

  fetch('/api/system/info')
    .then(r => r.ok ? r.json() : null)
    .then(d => { if (d?.hostname) hostname = d.hostname; })
    .catch(() => {});

  fetch('/auth/whoami')
    .then(r => r.ok ? r.json() : null)
    .then(d => { if (d?.username) username = d.username; })
    .catch(() => {});

  async function logout() {
    await fetch('/auth/logout', { method: 'POST' });
    window.location.reload();
  }
</script>

<header class="topbar">
  <div class="topbar-left">
    {#if hostname}
      <span class="hostname-pill">
        <span class="host-dot"></span>
        <span class="mono hostname-text">{hostname}</span>
      </span>
    {/if}
  </div>

  <div class="topbar-right">
    <div class="ws-indicator" title={wsConnected ? 'Live — WebSocket connected' : 'Not connected'}>
      <span class="dot {wsConnected ? 'dot-green' : 'dot-gray'}"></span>
      <span class="ws-label mono">{wsConnected ? 'live' : 'offline'}</span>
    </div>
    {#if username}
      <div class="user-pill">
        <span class="user-icon">◉</span>
        <span class="mono user-name">{username}</span>
        <button class="logout-btn" onclick={logout} title="Sign out">⏻</button>
      </div>
    {/if}
  </div>
</header>

<style>
.topbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 1.25rem;
  height: 48px;
  flex-shrink: 0;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border-subtle);
}

.topbar-left {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}

.hostname-pill {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  background: var(--bg-raised);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-md);
  padding: 0.2rem 0.55rem;
}
.host-dot {
  width: 5px; height: 5px;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 4px var(--accent);
}
.hostname-text { font-size: 0.72rem; color: var(--nav-item); }

.topbar-right {
  display: flex;
  align-items: center;
  gap: 0.875rem;
}

.ws-indicator {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  cursor: default;
}
.ws-label { font-size: 0.68rem; color: var(--text-tertiary); }

.user-pill {
  display: flex;
  align-items: center;
  gap: 0.35rem;
  background: var(--bg-raised);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-md);
  padding: 0.2rem 0.2rem 0.2rem 0.55rem;
}
.user-icon { font-size: 0.65rem; color: var(--accent); }
.user-name { font-size: 0.72rem; color: var(--nav-item); }

.logout-btn {
  background: none;
  border: none;
  padding: 0.15rem 0.4rem;
  cursor: pointer;
  color: var(--text-tertiary);
  font-size: 0.78rem;
  border-radius: var(--r-sm);
  line-height: 1;
  transition: color 0.15s, background 0.15s;
}
.logout-btn:hover { color: var(--red); background: var(--bg-hover); }
</style>
