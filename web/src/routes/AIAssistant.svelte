<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';

  interface OllamaModel { name: string; size: number; details: { family: string; parameter_size: string; }; }
  interface Recommended { name: string; description: string; ram: string; good_for: string; }
  interface Message { role: string; content: string; }

  let status: any = $state(null);
  let loading = $state(true);
  let error = $state('');
  let tab = $state<'chat'|'models'|'settings'>('chat');

  // Chat
  let messages: Message[] = $state([]);
  let input = $state('');
  let thinking = $state(false);
  let chatEl: HTMLDivElement;

  // Model management
  let localModels: OllamaModel[] = $state([]);
  let recommended: Recommended[] = $state([]);
  let pullingModel = $state('');
  let pullOutput: string[] = $state([]);
  let modelsLoading = $state(false);

  // Settings
  let settingsProvider = $state('ollama');
  let settingsOllamaURL = $state('http://localhost:11434');
  let settingsOllamaModel = $state('');
  let settingsAPIKey = $state('');
  let settingsModel = $state('');
  let settingsSystemPrompt = $state('');
  let settingsSaving = $state(false);

  async function loadStatus() {
    loading = true; error = '';
    try {
      status = await api.get<any>('/api/ai/status');
      localModels = status.local_models ?? [];
      recommended = status.recommended ?? [];
      settingsProvider = status.provider ?? 'ollama';
      settingsOllamaURL = status.ollama_url ?? 'http://localhost:11434';
      settingsOllamaModel = status.ollama_model ?? '';
      settingsSystemPrompt = status.system_prompt ?? '';
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function loadModels() {
    modelsLoading = true;
    try {
      const res = await api.get<any>('/api/ai/models');
      localModels = res.models ?? [];
      recommended = res.recommended ?? [];
    } catch(e: any) { error = e.message; }
    finally { modelsLoading = false; }
  }

  async function pullModel(name: string) {
    pullingModel = name; pullOutput = [];
    try {
      const resp = await fetch('/api/ai/models/pull', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: name }),
      });
      const reader = resp.body!.getReader();
      const dec = new TextDecoder(); let buf = '';
      while (true) {
        const { done, value } = await reader.read(); if (done) break;
        buf += dec.decode(value, { stream: true });
        const lines = buf.split('\n');
        for (const l of lines.slice(0, -1)) {
          if (l.startsWith('data: ')) {
            pullOutput = [...pullOutput, l.slice(6)];
          }
        }
        buf = lines[lines.length - 1];
      }
      await loadModels();
      settingsOllamaModel = name;
    } catch(e: any) { error = String(e); }
    finally { pullingModel = ''; }
  }

  async function deleteModel(name: string) {
    if (!confirm(`Delete model ${name}?`)) return;
    await api.delete('/api/ai/models', { model: name } as any);
    await loadModels();
  }

  async function saveSettings() {
    settingsSaving = true;
    try {
      await api.put('/api/ai/settings', {
        provider: settingsProvider,
        ollama_url: settingsOllamaURL,
        ollama_model: settingsOllamaModel,
        api_key: settingsAPIKey,
        model: settingsModel,
        system_prompt: settingsSystemPrompt,
      });
      await loadStatus();
      tab = 'chat';
    } catch(e: any) { error = e.message; }
    finally { settingsSaving = false; }
  }

  // Build context string from current system state
  async function buildContext(): Promise<string> {
    try {
      const [stats, services, ports] = await Promise.all([
        fetch('/api/system/stats').then(r => r.json()).catch(() => null),
        fetch('/api/services?type=service').then(r => r.json()).catch(() => null),
        fetch('/api/ports').then(r => r.json()).catch(() => null),
      ]);
      const parts = [];
      if (stats) {
        parts.push(`CPU: ${stats.cpu_percent?.toFixed(1)}% | RAM: ${stats.mem_used_human}/${stats.mem_total_human} | Load: ${stats.load_avg?.join(', ')}`);
        parts.push(`Disk: ${stats.disk_used_human}/${stats.disk_total_human} (${stats.disk_percent?.toFixed(0)}% used)`);
        parts.push(`Uptime: ${stats.uptime_human}`);
      }
      if (services) {
        const failed = (services.services ?? []).filter((s: any) => s.active_state === 'failed');
        const running = (services.services ?? []).filter((s: any) => s.active_state === 'active').length;
        parts.push(`Services: ${running} active, ${failed.length} failed${failed.length ? ': ' + failed.map((s: any) => s.name).join(', ') : ''}`);
      }
      if (ports) {
        const listening = (ports.ports ?? []).filter((p: any) => p.state === 'LISTEN').length;
        parts.push(`Open ports: ${listening} listening`);
      }
      return parts.join('\n');
    } catch {
      return '';
    }
  }

  async function sendMessage() {
    const text = input.trim();
    if (!text || thinking) return;
    input = '';

    const userMsg: Message = { role: 'user', content: text };
    messages = [...messages, userMsg];

    thinking = true;
    let assistantContent = '';
    const assistantIdx = messages.length;
    messages = [...messages, { role: 'assistant', content: '' }];

    try {
      const ctx = await buildContext();
      const resp = await fetch('/api/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          messages: messages.slice(0, assistantIdx),
          context: ctx,
        }),
      });

      const reader = resp.body!.getReader();
      const dec = new TextDecoder(); let buf = '';
      while (true) {
        const { done, value } = await reader.read(); if (done) break;
        buf += dec.decode(value, { stream: true });
        const lines = buf.split('\n');
        for (const l of lines.slice(0, -1)) {
          if (!l.startsWith('data: ')) continue;
          const data = l.slice(6);
          if (data === '[DONE]') continue;
          try {
            const token = JSON.parse(data);
            assistantContent += token;
            messages = messages.map((m, i) =>
              i === assistantIdx ? { ...m, content: assistantContent } : m
            );
            // Scroll to bottom
            setTimeout(() => { if (chatEl) chatEl.scrollTop = chatEl.scrollHeight; }, 0);
          } catch {}
        }
        buf = lines[lines.length - 1];
      }
    } catch(e: any) {
      messages = messages.map((m, i) =>
        i === assistantIdx ? { ...m, content: '[error: ' + String(e) + ']' } : m
      );
    } finally {
      thinking = false;
    }
  }

  function fmtBytes(b: number) {
    if (b > 1e9) return (b/1e9).toFixed(1) + ' GB';
    if (b > 1e6) return (b/1e6).toFixed(1) + ' MB';
    return b + ' B';
  }

  function isInstalled(name: string) {
    return localModels.some(m => m.name === name);
  }

  const ollamaInstallCmd: Record<string, string> = {
    'arch':   'curl -fsSL https://ollama.com/install.sh | sh',
    'debian': 'curl -fsSL https://ollama.com/install.sh | sh',
    'rhel':   'curl -fsSL https://ollama.com/install.sh | sh',
  };

  onMount(loadStatus);
</script>

<div class="ai-page">
  <div class="page-header">
    <div>
      <h1>AI Assistant</h1>
      {#if status?.ollama_online}
        <p class="subtitle">Ollama online · {localModels.length} models · {status.ollama_model || 'auto-select'}</p>
      {:else if status}
        <p class="subtitle" style="color:var(--red)">Ollama offline — <a href="https://ollama.com/download" target="_blank" rel="noreferrer">install Ollama</a> or check the URL in Settings</p>
      {/if}
    </div>
    <div class="actions">
      <div class="seg-control">
        <button class="seg-btn" class:active={tab==='chat'}     onclick={() => tab='chat'}>Chat</button>
        <button class="seg-btn" class:active={tab==='models'}   onclick={() => { tab='models'; loadModels(); }}>Models</button>
        <button class="seg-btn" class:active={tab==='settings'} onclick={() => tab='settings'}>Settings</button>
      </div>
    </div>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  {#if loading}
    <div class="card skeleton" style="height:200px"></div>

  {:else if tab === 'chat'}
    {#if !status?.ollama_online && !status?.has_api_key}
      <!-- Setup wizard -->
      <div class="card wizard">
        <h2 style="margin-bottom:0.75rem">Get started</h2>
        <p style="color:var(--text-secondary);margin-bottom:1.5rem">
          Webux uses Ollama to run AI models locally — no API key needed, no data leaves your server.
        </p>
        <div class="wizard-steps">
          <div class="wizard-step">
            <div class="step-num">1</div>
            <div class="step-body">
              <div class="step-title">Install Ollama</div>
              <code class="step-cmd">curl -fsSL https://ollama.com/install.sh | sh</code>
              <div style="font-size:0.72rem;color:var(--text-tertiary);margin-top:0.25rem">
                Works on Arch, Debian, Ubuntu, RHEL, Fedora — one command
              </div>
            </div>
          </div>
          <div class="wizard-step">
            <div class="step-num">2</div>
            <div class="step-body">
              <div class="step-title">Pull a model</div>
              <div style="font-size:0.82rem;color:var(--text-secondary)">
                Go to the <button class="link-btn" onclick={() => tab='models'}>Models tab</button> and pull a model. Start with <code>llama3.2:3b</code> (4 GB RAM) for most servers.
              </div>
            </div>
          </div>
          <div class="wizard-step">
            <div class="step-num">3</div>
            <div class="step-body">
              <div class="step-title">Start chatting</div>
              <div style="font-size:0.82rem;color:var(--text-secondary)">
                The assistant has live access to your system stats, services, ports, and more.
              </div>
            </div>
          </div>
        </div>
        <div style="margin-top:1.5rem;display:flex;gap:0.5rem">
          <button class="btn btn-primary" onclick={() => tab='models'}>→ Choose a model</button>
          <button class="btn btn-ghost" onclick={() => tab='settings'}>Configure API key instead</button>
        </div>
      </div>

    {:else}
      <!-- Chat interface -->
      <div class="chat-wrap card">
        <div class="chat-messages" bind:this={chatEl}>
          {#if messages.length === 0}
            <div class="chat-empty">
              <div style="font-size:1.5rem;margin-bottom:0.5rem">◍</div>
              <div style="font-weight:500;margin-bottom:0.25rem">Ask me anything about this server</div>
              <div style="font-size:0.82rem;color:var(--text-tertiary)">I have live access to your system stats, services, processes, ports, and more.</div>
              <div class="chat-suggestions">
                {#each ["Why is my server slow?", "Which services have failed?", "What's listening on port 3389?", "Is my disk running low?"] as s}
                  <button class="suggestion" onclick={() => { input = s; sendMessage(); }}>{s}</button>
                {/each}
              </div>
            </div>
          {/if}
          {#each messages as msg, i (i)}
            <div class="msg msg-{msg.role}">
              <div class="msg-role">{msg.role === 'user' ? 'You' : '◍ Assistant'}</div>
              <div class="msg-content">{msg.content || (thinking && i === messages.length - 1 ? '▌' : '')}</div>
            </div>
          {/each}
        </div>
        <div class="chat-input-row">
          <textarea class="chat-input" bind:value={input}
            placeholder="Ask about your server…"
            rows="2"
            onkeydown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMessage(); } }}
          ></textarea>
          <button class="btn btn-primary send-btn" onclick={sendMessage} disabled={thinking || !input.trim()}>
            {thinking ? '⟳' : '↑'}
          </button>
        </div>
      </div>
    {/if}

  {:else if tab === 'models'}
    <!-- Installed models -->
    {#if localModels.length > 0}
      <div class="card" style="padding:0;margin-bottom:1rem">
        <div class="section-header">Installed models</div>
        <table class="data-table">
          <thead><tr><th>Model</th><th>Family</th><th>Size</th><th>Active</th><th style="text-align:right">Remove</th></tr></thead>
          <tbody>
            {#each localModels as m (m.name)}
              <tr>
                <td class="mono" style="font-weight:600">{m.name}</td>
                <td style="color:var(--text-secondary);font-size:0.8rem">{m.details?.family ?? '—'} {m.details?.parameter_size ?? ''}</td>
                <td class="mono" style="font-size:0.8rem">{fmtBytes(m.size)}</td>
                <td>
                  {#if m.name === (status?.ollama_model || localModels[0]?.name)}
                    <span class="badge badge-green">active</span>
                  {:else}
                    <button class="btn btn-ghost" style="font-size:0.72rem"
                      onclick={() => { settingsOllamaModel = m.name; api.put('/api/ai/settings', {ollama_model: m.name}); }}>
                      Use
                    </button>
                  {/if}
                </td>
                <td style="text-align:right">
                  <button class="btn btn-ghost" style="font-size:0.72rem;color:var(--red)"
                    onclick={() => deleteModel(m.name)}>✕</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    <!-- Pull progress -->
    {#if pullOutput.length > 0}
      <div class="card pull-progress">
        <div style="font-size:0.75rem;font-weight:500;margin-bottom:0.375rem">
          Pulling {pullingModel || 'model'}…
        </div>
        <div class="pull-output">
          {#each pullOutput.slice(-5) as line}
            <div class="mono" style="font-size:0.72rem;color:var(--accent)">{line}</div>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Recommended models -->
    <div class="card" style="padding:0">
      <div class="section-header">Recommended models</div>
      <table class="data-table">
        <thead><tr><th>Model</th><th>RAM needed</th><th>Good for</th><th style="text-align:right">Action</th></tr></thead>
        <tbody>
          {#each recommended as r}
            <tr>
              <td>
                <div class="mono" style="font-weight:600">{r.name}</div>
                <div style="font-size:0.72rem;color:var(--text-tertiary)">{r.description}</div>
              </td>
              <td><span class="badge badge-yellow">{r.ram}</span></td>
              <td style="font-size:0.78rem;color:var(--text-secondary)">{r.good_for}</td>
              <td style="text-align:right">
                {#if isInstalled(r.name)}
                  <span class="badge badge-green">installed</span>
                {:else}
                  <button class="btn btn-primary" style="font-size:0.75rem"
                    disabled={pullingModel !== ''}
                    onclick={() => pullModel(r.name)}>
                    {pullingModel === r.name ? '⟳ Pulling…' : '⬇ Pull'}
                  </button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

  {:else if tab === 'settings'}
    <div class="card settings-panel">
      <div class="form-grid">
        <div class="form-row" style="grid-column:1/-1">
          <label for="ai-provider">Provider</label>
          <select id="ai-provider" class="search-input" bind:value={settingsProvider}
            style="background:var(--bg-raised);color:var(--text-primary)">
            <option value="ollama">Ollama (local — recommended)</option>
            <option value="openai">OpenAI</option>
            <option value="anthropic">Anthropic</option>
            <option value="custom">Custom OpenAI-compatible URL</option>
          </select>
        </div>

        {#if settingsProvider === 'ollama'}
          <div class="form-row">
            <label for="ollama-url">Ollama URL</label>
            <input id="ollama-url" class="search-input mono" bind:value={settingsOllamaURL}
              placeholder="http://localhost:11434" />
          </div>
          <div class="form-row">
            <label for="ollama-model">Model (blank = auto-select first available)</label>
            <input id="ollama-model" class="search-input mono" bind:value={settingsOllamaModel}
              placeholder="llama3.2:3b" />
          </div>
        {:else}
          <div class="form-row">
            <label for="ai-key">API Key</label>
            <input id="ai-key" class="search-input mono" type="password" bind:value={settingsAPIKey}
              placeholder="sk-…" />
          </div>
          <div class="form-row">
            <label for="ai-model">Model</label>
            <input id="ai-model" class="search-input mono" bind:value={settingsModel}
              placeholder={settingsProvider === 'anthropic' ? 'claude-haiku-4-5-20251001' : 'gpt-4o-mini'} />
          </div>
          {#if settingsProvider === 'custom'}
            <div class="form-row" style="grid-column:1/-1">
              <label for="ai-baseurl">Base URL</label>
              <input id="ai-baseurl" class="search-input mono" bind:value={settingsOllamaURL}
                placeholder="http://my-llm-server:8000" />
            </div>
          {/if}
        {/if}

        <div class="form-row" style="grid-column:1/-1">
          <label for="sys-prompt">System prompt</label>
          <textarea id="sys-prompt" class="search-input" rows="4" bind:value={settingsSystemPrompt}
            style="resize:vertical;font-size:0.8rem"></textarea>
        </div>
      </div>

      <div style="display:flex;gap:0.5rem;margin-top:1rem">
        <button class="btn btn-primary" onclick={saveSettings} disabled={settingsSaving}>
          {settingsSaving ? 'Saving…' : 'Save'}
        </button>
        <button class="btn btn-ghost" onclick={() => tab='chat'}>Cancel</button>
      </div>
    </div>
  {/if}
</div>

<style>
.ai-page { max-width:900px; padding-bottom:220px; }
.seg-control { display:flex; border:1px solid var(--border-default); border-radius:var(--r-md); overflow:hidden; }
.seg-btn { padding:0.35rem 0.75rem; background:var(--bg-raised); border:none; border-right:1px solid var(--border-default); cursor:pointer; font-size:0.8rem; color:var(--text-secondary); }
.seg-btn:last-child { border-right:none; }
.seg-btn.active { background:var(--accent); color:var(--bg-base); font-weight:500; }

/* Wizard */
.wizard { padding:1.5rem; }
.wizard-steps { display:flex; flex-direction:column; gap:1rem; }
.wizard-step { display:flex; gap:1rem; align-items:flex-start; }
.step-num { width:28px; height:28px; border-radius:50%; background:var(--accent); color:var(--bg-base); font-weight:700; font-size:0.82rem; display:flex; align-items:center; justify-content:center; flex-shrink:0; }
.step-body { flex:1; }
.step-title { font-weight:600; font-size:0.9rem; margin-bottom:0.25rem; }
.step-cmd { display:inline-block; background:var(--bg-base); padding:0.3rem 0.6rem; border-radius:var(--r-sm); font-family:var(--font-mono); font-size:0.8rem; color:var(--accent); }
.link-btn { background:none; border:none; color:var(--accent); cursor:pointer; font-size:inherit; padding:0; text-decoration:underline; }

/* Chat */
.chat-wrap { padding:0; display:flex; flex-direction:column; height:calc(100vh - 260px); min-height:400px; }
.chat-messages { flex:1; overflow-y:auto; padding:1rem; display:flex; flex-direction:column; gap:1rem; }
.chat-empty { display:flex; flex-direction:column; align-items:center; justify-content:center; height:100%; text-align:center; color:var(--text-secondary); }
.chat-suggestions { display:flex; flex-wrap:wrap; gap:0.375rem; justify-content:center; margin-top:1rem; }
.suggestion { padding:0.35rem 0.75rem; background:var(--bg-raised); border:1px solid var(--border-default); border-radius:var(--r-md); font-size:0.78rem; color:var(--text-secondary); cursor:pointer; }
.suggestion:hover { border-color:var(--accent); color:var(--accent); }

.msg { display:flex; flex-direction:column; gap:0.25rem; max-width:85%; }
.msg-user { align-self:flex-end; }
.msg-assistant { align-self:flex-start; }
.msg-role { font-size:0.68rem; color:var(--text-tertiary); text-transform:uppercase; letter-spacing:0.06em; }
.msg-user .msg-role { text-align:right; }
.msg-content { padding:0.625rem 0.875rem; border-radius:var(--r-lg); font-size:0.85rem; line-height:1.55; white-space:pre-wrap; word-break:break-word; }
.msg-user .msg-content { background:var(--accent); color:var(--bg-base); border-radius:var(--r-lg) var(--r-lg) var(--r-sm) var(--r-lg); }
.msg-assistant .msg-content { background:var(--bg-raised); border:1px solid var(--border-subtle); border-radius:var(--r-sm) var(--r-lg) var(--r-lg) var(--r-lg); }

.chat-input-row { display:flex; gap:0.5rem; padding:0.75rem; border-top:1px solid var(--border-subtle); }
.chat-input { flex:1; background:var(--bg-raised); border:1px solid var(--border-default); border-radius:var(--r-md); color:var(--text-primary); font-family:var(--font-sans); font-size:0.85rem; padding:0.5rem 0.75rem; resize:none; outline:none; }
.chat-input:focus { border-color:var(--accent); }
.send-btn { align-self:flex-end; font-size:1rem; padding:0.5rem 0.875rem; }

/* Models */
.section-header { padding:0.625rem 0.875rem; font-size:0.68rem; font-weight:500; color:var(--text-tertiary); text-transform:uppercase; letter-spacing:0.06em; border-bottom:1px solid var(--border-subtle); }
.pull-progress { margin-bottom:0.75rem; }
.pull-output { background:var(--bg-base); border-radius:var(--r-sm); padding:0.5rem 0.625rem; }

/* Settings */
.settings-panel { padding:1.25rem; }
.form-grid { display:grid; grid-template-columns:1fr 1fr; gap:0.875rem; }
.form-row { display:flex; flex-direction:column; gap:0.3rem; }
.form-row label { font-size:0.75rem; color:var(--text-secondary); font-weight:500; }
.form-row select { background:var(--bg-raised); color:var(--text-primary); }
</style>
