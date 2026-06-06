<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import '@xterm/xterm/css/xterm.css';

  interface QuickCmd { label: string; cmd: string; }

  let termContainer: HTMLDivElement;
  let terminal: Terminal | null = null;
  let fitAddon: FitAddon | null = null;
  // Use a plain object ref so onData closes over the ref, not the $state value
  // $state uses a proxy — closures capture the original null, not live updates
  const wsRef = { current: null as WebSocket | null };
  let connected = $state(false);
  let shell = $state('');
  let configuredShell = $state('');
  let quickCmds: QuickCmd[] = $state([]);
  let showSettings = $state(false);
  let settingsShell = $state('');
  let settingsQuickCmds = $state('');
  let settingsError = $state('');
  let _closeDropdown: ((e: MouseEvent) => void) | null = null;
  let showMoreCmds = $state(false);
  let resizeObserver: ResizeObserver | null = null;

  async function loadSettings() {
    try {
      const res = await fetch('/api/terminal/settings');
      const data = await res.json();
      shell = data.shell ?? '';
      configuredShell = data.configured_shell ?? '';
      try { quickCmds = JSON.parse(data.quick_commands ?? '[]'); } catch { quickCmds = []; }
      settingsShell = configuredShell;
      settingsQuickCmds = JSON.stringify(quickCmds, null, 2);
    } catch {}
  }

  async function saveSettings() {
    settingsError = '';
    // Validate JSON
    try { JSON.parse(settingsQuickCmds); } catch {
      settingsError = 'Quick commands must be valid JSON'; return;
    }
    try {
      await fetch('/api/terminal/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          shell: settingsShell,
          quick_commands: settingsQuickCmds,
        }),
      });
      await loadSettings();
      showSettings = false;
      // Reconnect with new shell
      disconnect();
      setTimeout(connect, 300);
    } catch(e: any) { settingsError = e.message; }
  }

  function connect() {
    if (wsRef.current) return;
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    wsRef.current = new WebSocket(`${proto}//${location.host}/ws/terminal`);
    wsRef.current.binaryType = 'arraybuffer';

    wsRef.current.onopen = () => {
      connected = true;
      // Send initial terminal size
      if (terminal) {
        wsRef.current?.send(JSON.stringify({
          type: 'resize',
          cols: terminal.cols,
          rows: terminal.rows,
        }));
      }
    };

    wsRef.current.onclose = () => {
      connected = false;
      terminal?.writeln('\r\n\x1b[33m[disconnected — click Reconnect to start a new session]\x1b[0m');
      wsRef.current = null;
    };

    wsRef.current.onerror = () => {
      connected = false;
      wsRef.current = null;
    };

    wsRef.current.onmessage = (e) => {
      if (!terminal) return;
      if (e.data instanceof ArrayBuffer) {
        terminal.write(new Uint8Array(e.data));
      } else {
        terminal.write(e.data);
      }
      // Focus on first message — PTY is ready at this point
      terminal.focus();
    };
  }

  function disconnect() {
    wsRef.current?.close();
    wsRef.current = null;
    connected = false;
  }

  // Called from CLIEchoPane play button — exported via window
  export function runCommand(cmd: string) {
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      connect();
      // Wait for connection then send
      const check = setInterval(() => {
        if (wsRef.current?.readyState === WebSocket.OPEN) {
          clearInterval(check);
          setTimeout(() => {
            wsRef.current?.send(JSON.stringify({ type: 'run', cmd }));
          }, 200);
        }
      }, 50);
      return;
    }
    wsRef.current.send(JSON.stringify({ type: 'run', cmd }));
    terminal?.focus();
  }

  function runQuickCmd(cmd: string) {
    runCommand(cmd);
    terminal?.focus();
  }

  function sendResize() {
    if (wsRef.current?.readyState === WebSocket.OPEN && terminal) {
      wsRef.current.send(JSON.stringify({
        type: 'resize',
        cols: terminal.cols,
        rows: terminal.rows,
      }));
    }
  }

  onMount(async () => {
    await loadSettings();

    // Expose runCommand globally so CLIEchoPane can reach it
    (window as any).__webuxTerminal = { runCommand };

    // Init xterm.js
    terminal = new Terminal({
      theme: {
        background:  '#0a0d14',
        foreground:  '#dde4f5',
        cursor:      '#2ed898',
        cursorAccent:'#0a0d14',
        black:       '#1a2238',
        brightBlack: '#424e6e',
        red:         '#e05c6a',
        brightRed:   '#ff7b86',
        green:       '#2ed898',
        brightGreen: '#4ee8a8',
        yellow:      '#e0c44e',
        brightYellow:'#f5d76e',
        blue:        '#4e8de0',
        brightBlue:  '#6ea8ff',
        magenta:     '#8b72e0',
        brightMagenta:'#aa94ff',
        cyan:        '#2ed8c8',
        brightCyan:  '#4ef5e0',
        white:       '#dde4f5',
        brightWhite: '#ffffff',
      },
      fontFamily: '"IBM Plex Mono", "Cascadia Code", "Fira Code", monospace',
      fontSize: 13,
      lineHeight: 1.3,
      cursorBlink: true,
      cursorStyle: 'bar',
      scrollback: 5000,
      allowProposedApi: true,
    });

    fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.open(termContainer);
    fitAddon.fit();

    // Re-focus after fit (fit can cause internal re-render losing focus)
    const fitAndFocus = () => {
      if (fitAddon && terminal) {
        try {
          fitAddon.fit();
          sendResize();
          terminal.focus();
        } catch {}
      }
    };

    // Forward keystrokes to WebSocket via onData (standard path)
    terminal.onData((data: string) => {
      if (wsRef.current?.readyState === WebSocket.OPEN) {
        wsRef.current.send(data);
      }
    });

    // Firefox strips key properties from events dispatched to off-screen textareas.
    // Intercept at window level where key properties are intact, convert to
    // terminal escape sequences, and send directly to the WebSocket.
    const keyMap: Record<string, string> = {
      'Enter':     '\r',
      'Backspace': '\x7f',
      'Tab':       '\t',
      'Escape':    '\x1b',
      'ArrowUp':   '\x1b[A',
      'ArrowDown': '\x1b[B',
      'ArrowRight':'\x1b[C',
      'ArrowLeft': '\x1b[D',
      'Home':      '\x1b[H',
      'End':       '\x1b[F',
      'Delete':    '\x1b[3~',
      'PageUp':    '\x1b[5~',
      'PageDown':  '\x1b[6~',
      'F1':        '\x1bOP', 'F2': '\x1bOQ', 'F3': '\x1bOR', 'F4': '\x1bOS',
      'F5':        '\x1b[15~', 'F6': '\x1b[17~', 'F7': '\x1b[18~',
      'F8':        '\x1b[19~', 'F9': '\x1b[20~', 'F10': '\x1b[21~',
      'F11':       '\x1b[23~', 'F12': '\x1b[24~',
    };

    const windowKeyHandler = (e: KeyboardEvent) => {
      // Only intercept when terminal area is focused/active
      // and no browser UI element has taken focus
      const tag = (document.activeElement as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'SELECT' || tag === 'BUTTON') return;
      if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) return;

      // Skip modifier-only keys
      if (['Control','Alt','Shift','Meta','CapsLock'].includes(e.key)) return;

      let seq = '';

      if (e.ctrlKey && e.key.length === 1) {
        // Ctrl+A → \x01, Ctrl+C → \x03, etc.
        const code = e.key.toUpperCase().charCodeAt(0) - 64;
        if (code >= 1 && code <= 31) {
          seq = String.fromCharCode(code);
        }
      } else if (e.altKey && e.key.length === 1) {
        seq = '\x1b' + e.key;
      } else if (keyMap[e.key]) {
        seq = keyMap[e.key];
      } else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
        seq = e.key;
      }

      if (seq) {
        e.preventDefault();
        e.stopPropagation();
        wsRef.current.send(seq);
      }
    };

    window.addEventListener('keydown', windowKeyHandler, true);
    (termContainer as any)._windowKeyHandler = windowKeyHandler;

    // Keep focus when terminal loses it (e.g. after fit re-render)
    terminal.textarea?.addEventListener('blur', () => {
      // Small delay so legitimate focus changes (clicking settings) aren't overridden
      setTimeout(() => {
        if (document.activeElement?.tagName !== 'INPUT' &&
            document.activeElement?.tagName !== 'TEXTAREA' &&
            document.activeElement?.tagName !== 'BUTTON') {
          terminal?.focus();
        }
      }, 100);
    });

    // Watch container resize
    resizeObserver = new ResizeObserver(fitAndFocus);
    resizeObserver.observe(termContainer);

    // Also watch the whole page for learn mode pane open/close
    resizeObserver.observe(document.querySelector('.echo-pane') || document.body);

    // Close quick-cmd dropdown when clicking outside
    const closeDropdown = (e: MouseEvent) => {
      if (!(e.target as Element)?.closest('.qcmd-more')) {
        showMoreCmds = false;
      }
    };
    _closeDropdown = closeDropdown;
    setTimeout(() => window.addEventListener('click', closeDropdown), 100);

    setTimeout(connect, 50);
  });

  onDestroy(() => {
    resizeObserver?.disconnect();
    if ((termContainer as any)?._windowKeyHandler) {
      window.removeEventListener('keydown', (termContainer as any)._windowKeyHandler, true);
    }
    if (_closeDropdown) {
      window.removeEventListener('click', _closeDropdown);
    }
    disconnect();
    terminal?.dispose();
    delete (window as any).__webuxTerminal;
  });
</script>

<div class="term-page">
  <!-- Header bar -->
  <div class="term-header">
    <div class="term-header-left">
      <span class="dot {connected ? 'dot-green' : 'dot-red'}"></span>
      <span class="term-shell mono">{shell || 'shell'}</span>
      {#if !connected}
        <span style="font-size:0.75rem;color:var(--text-tertiary)">disconnected</span>
      {/if}
    </div>
    <div class="term-header-right">
      <!-- Quick commands -->
      {#each quickCmds.slice(0, 6) as qc}
        <button class="qcmd-btn" onclick={() => runQuickCmd(qc.cmd)} title={qc.cmd}>
          {qc.label}
        </button>
      {/each}
      {#if quickCmds.length > 6}
        <div class="qcmd-more">
          <button class="qcmd-btn" onclick={() => showMoreCmds = !showMoreCmds}>
            +{quickCmds.length - 6} more {showMoreCmds ? '▴' : '▾'}
          </button>
          {#if showMoreCmds}
            <div class="qcmd-dropdown">
              {#each quickCmds.slice(6) as qc}
                <button class="qcmd-item" onclick={() => { runQuickCmd(qc.cmd); showMoreCmds = false; }}>
                  {qc.label}
                </button>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
      <button class="btn btn-ghost" onclick={() => { disconnect(); setTimeout(connect, 100); }}>
        ↺ Reconnect
      </button>
      <button class="btn btn-ghost" onclick={() => { showSettings = !showSettings; }}>
        ⚙ Shell
      </button>
    </div>
  </div>

  <!-- Settings panel (inline, above terminal) -->
  {#if showSettings}
    <div class="settings-panel card">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.75rem">
        <h3 style="margin:0;font-size:0.82rem">Terminal settings</h3>
        <button class="btn btn-ghost" onclick={() => showSettings = false}>✕</button>
      </div>
      {#if settingsError}<div class="alert alert-error" style="margin-bottom:0.5rem">{settingsError}</div>{/if}
      <div class="settings-grid">
        <div class="form-row">
          <label for="ts-shell">Shell override</label>
          <input id="ts-shell" class="search-input mono" bind:value={settingsShell}
            placeholder="Leave blank to use login shell ({shell})" />
          <span style="font-size:0.68rem;color:var(--text-tertiary)">
            Current: {shell} · blank = auto-detect from /etc/passwd
          </span>
        </div>
        <div class="form-row" style="grid-column:1/-1">
          <label for="ts-cmds">Quick commands (JSON array)</label>
          <textarea id="ts-cmds" class="search-input mono" rows="8" bind:value={settingsQuickCmds}
            style="resize:vertical;font-size:0.75rem;line-height:1.5"></textarea>
          <span style="font-size:0.68rem;color:var(--text-tertiary)">
            Format: <code>[{"{\"label\":\"Name\",\"cmd\":\"command\"}"}]</code>
          </span>
        </div>
      </div>
      <div style="display:flex;gap:0.5rem;margin-top:0.75rem">
        <button class="btn btn-primary" onclick={saveSettings}>Save & reconnect</button>
        <button class="btn btn-ghost" onclick={() => showSettings = false}>Cancel</button>
      </div>
    </div>
  {/if}

  <!-- xterm.js container — fills remaining height -->
  <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
  <div class="term-wrap" bind:this={termContainer}
    onmousedown={(e) => { e.preventDefault(); terminal?.focus(); }}
  ></div>
</div>

<style>
.term-page {
  display: flex;
  flex-direction: column;
  /* 48px topbar only — learn mode pane is hidden on terminal page */
  height: calc(100vh - 48px);
  overflow: hidden;
  padding: 0;
  margin: -1.25rem -1.5rem;
}

.term-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.375rem 0.75rem;
  background: var(--bg-panel);
  border-bottom: 1px solid var(--border-subtle);
  flex-shrink: 0;
  flex-wrap: wrap;
  gap: 0.5rem;
  min-height: 40px;
}

.term-header-left {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.term-shell { font-size: 0.78rem; color: var(--text-secondary); }

.term-header-right {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  flex-wrap: wrap;
}

/* Quick command chips */
.qcmd-btn {
  padding: 0.2rem 0.6rem;
  background: var(--bg-raised);
  border: 1px solid var(--border-default);
  border-radius: var(--r-sm);
  font-size: 0.72rem;
  font-family: var(--font-sans);
  color: var(--nav-item);
  cursor: pointer;
  white-space: nowrap;
  transition: border-color 0.1s, color 0.1s;
}
.qcmd-btn:hover { border-color: var(--accent); color: var(--accent); }

.qcmd-more { position: relative; }
.qcmd-dropdown {
  position: absolute;
  top: calc(100% + 4px);
  right: 0;
  background: var(--bg-panel);
  border: 1px solid var(--border-default);
  border-radius: var(--r-md);
  padding: 0.375rem;
  z-index: 100;
  min-width: 180px;
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
  box-shadow: 0 4px 12px rgba(0,0,0,0.3);
}
.qcmd-item {
  padding: 0.3rem 0.6rem;
  background: none;
  border: none;
  border-radius: var(--r-sm);
  font-size: 0.78rem;
  color: var(--text-secondary);
  cursor: pointer;
  text-align: left;
}
.qcmd-item:hover { background: var(--bg-hover); color: var(--accent); }

/* Settings panel */
.settings-panel {
  margin: 0;
  border-radius: 0;
  border-left: none;
  border-right: none;
  border-top: none;
  flex-shrink: 0;
  padding: 0.875rem 1rem;
}
.settings-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem; }
.form-row { display: flex; flex-direction: column; gap: 0.25rem; }
.form-row label { font-size: 0.72rem; color: var(--text-secondary); font-weight: 500; }

/* xterm container — fills all remaining space */
.term-wrap {
  flex: 1;
  overflow: hidden;
  background: #0a0d14;
  padding: 4px 6px;
}

/* Let xterm.js own its own sizing */
:global(.xterm) { height: 100% !important; }
:global(.xterm-viewport) { overflow-y: auto !important; }
:global(.xterm-screen) { width: 100% !important; }
</style>
