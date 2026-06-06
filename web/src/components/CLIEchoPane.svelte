<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { wsStore } from '$lib/ws';
  import type { EchoEntry } from '$lib/ws';

  let { context = '' }: { context?: string } = $props();

  // Detect if we're on the terminal page to hide the pane
  let currentHash = $state(window.location.hash);
  window.addEventListener('hashchange', () => { currentHash = window.location.hash; });
  let hidden = $derived(currentHash === '#/terminal');

  let entries: EchoEntry[] = $state([]);
  let open = $state(false);
  let autoscroll = $state(true);
  let logEl: HTMLElement | undefined = $state();
  let playingCmd = $state<string|null>(null);

  let unsub: (() => void) | undefined;

  onMount(async () => {
    try {
      const res = await fetch('/api/learn/recent?n=50');
      const data = await res.json();
      entries = (data ?? []).filter((e: EchoEntry) => !context || e.context === context);
    } catch {}

    unsub = wsStore.subscribe(event => {
      if (event?.type !== 'cli_echo') return;
      const entry: EchoEntry = event.payload;
      if (context && entry.context !== context) return;
      entries = [...entries.slice(-499), entry];
      if (autoscroll && logEl) {
        requestAnimationFrame(() => { if (logEl) logEl.scrollTop = logEl.scrollHeight; });
      }
    });
  });

  onDestroy(() => unsub?.());

  function playCommand(cmd: string) {
    playingCmd = cmd;

    // If terminal is already mounted, use it directly
    const term = (window as any).__webuxTerminal;
    if (term?.runCommand) {
      term.runCommand(cmd);
      playingCmd = null;
      return;
    }

    // Not on terminal page — navigate there and wait for it to mount
    window.location.hash = '#/terminal';

    // Poll until terminal mounts (xterm.js + WS can take ~500ms)
    let attempts = 0;
    const poll = setInterval(() => {
      const t = (window as any).__webuxTerminal;
      if (t?.runCommand) {
        clearInterval(poll);
        setTimeout(() => {
          t.runCommand(cmd);
          playingCmd = null;
        }, 400); // brief extra delay for PTY to be ready
        return;
      }
      if (++attempts > 40) { // 2s timeout
        clearInterval(poll);
        playingCmd = null;
      }
    }, 50);
  }
</script>

<div class="echo-pane" class:open class:hidden>
  <button class="echo-toggle" onclick={() => open = !open}>
    <span class="toggle-chevron">{open ? '▾' : '▸'}</span>
    <span class="toggle-label">Learn mode</span>
    <span class="toggle-sub">— CLI equivalent of each action</span>
    {#if entries.length > 0 && !open}
      <span class="entry-count">{entries.length}</span>
    {/if}
  </button>

  {#if open}
    <div class="echo-body">
      <div class="echo-toolbar">
        <span class="echo-hint">Every action is shown here as a shell command. ▶ runs it in the terminal.</span>
        <label class="autoscroll-toggle">
          <input type="checkbox" bind:checked={autoscroll} />
          <span>Auto-scroll</span>
        </label>
        <button class="btn btn-ghost" onclick={() => entries = []}>Clear</button>
      </div>
      <div class="echo-log" bind:this={logEl}>
        {#if entries.length === 0}
          <div class="echo-empty">No commands yet — interact with a page to see CLI equivalents here.</div>
        {/if}
        {#each entries as entry (entry.id ?? entry.created_at)}
          <div class="echo-entry">
            <span class="echo-time mono">{new Date(entry.created_at).toLocaleTimeString()}</span>
            <div class="echo-cmd-row">
              <code class="echo-cmd">$ {entry.cmd}</code>
              <button
                class="play-btn"
                class:playing={playingCmd === entry.cmd}
                title="Run in terminal"
                onclick={() => playCommand(entry.cmd)}
              >
                {#if playingCmd === entry.cmd}
                  <span class="play-spinner">⟳</span>
                {:else}
                  ▶
                {/if}
              </button>
            </div>
            {#if entry.explanation}
              <span class="echo-explain">{entry.explanation}</span>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
.echo-pane {
  position: fixed;
  bottom: 0;
  left: var(--sidebar-width);
  right: 0;
  z-index: 200;
  background: var(--bg-panel);
  border-top: 1px solid var(--border-subtle);
}

.echo-pane.hidden { display: none; }

/* Hide learn mode on terminal page — it obscures the prompt */
:global(.term-page) ~ .echo-pane,
:global(body:has(.term-page)) .echo-pane {
  display: none;
}

.echo-toggle {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  width: 100%;
  padding: 0.4rem 1rem;
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-tertiary);
  font-family: var(--font-sans);
  font-size: 0.75rem;
  text-align: left;
  transition: color 0.15s;
}
.echo-toggle:hover { color: var(--text-secondary); }

.toggle-chevron { font-size: 0.65rem; }
.toggle-label { font-weight: 500; color: var(--text-secondary); }
.toggle-sub { color: var(--text-tertiary); }

.entry-count {
  margin-left: auto;
  background: var(--accent-dim);
  color: var(--accent);
  border-radius: 10px;
  padding: 0.1rem 0.45rem;
  font-size: 0.68rem;
  font-family: var(--font-mono);
}

.echo-body { border-top: 1px solid var(--border-subtle); }

.echo-toolbar {
  display: flex;
  align-items: center;
  gap: 1rem;
  padding: 0.375rem 1rem;
  border-bottom: 1px solid var(--border-subtle);
  background: var(--bg-raised);
}
.echo-hint { flex: 1; font-size: 0.72rem; color: var(--text-tertiary); font-style: italic; }

.autoscroll-toggle {
  display: flex;
  align-items: center;
  gap: 0.3rem;
  cursor: pointer;
  font-size: 0.72rem;
  color: var(--text-tertiary);
}

.echo-log {
  max-height: 180px;
  overflow-y: auto;
  padding: 0.5rem 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.echo-empty { font-size: 0.75rem; color: var(--text-tertiary); font-style: italic; padding: 0.375rem 0; }

.echo-entry { display: flex; flex-direction: column; gap: 2px; }

.echo-time { font-size: 0.68rem; color: var(--text-tertiary); }

.echo-cmd-row {
  display: flex;
  align-items: center;
  gap: 0.375rem;
}

.echo-cmd {
  flex: 1;
  font-family: var(--font-mono);
  font-size: 0.78rem;
  color: var(--accent);
  background: var(--bg-raised);
  padding: 0.2rem 0.5rem;
  border-radius: var(--r-sm);
  border-left: 2px solid var(--accent);
}

.play-btn {
  flex-shrink: 0;
  padding: 0.15rem 0.45rem;
  background: var(--bg-raised);
  border: 1px solid var(--border-default);
  border-radius: var(--r-sm);
  color: var(--accent);
  font-size: 0.72rem;
  cursor: pointer;
  transition: background 0.12s, border-color 0.12s;
  line-height: 1;
}
.play-btn:hover {
  background: var(--accent-dim);
  border-color: var(--accent);
}
.play-btn.playing {
  opacity: 0.6;
  cursor: wait;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}
.play-spinner {
  display: inline-block;
  animation: spin 0.8s linear infinite;
}

.echo-explain {
  font-size: 0.72rem;
  color: var(--text-secondary);
  padding-left: 0.5rem;
}
</style>
