<script lang="ts">
  import { onMount } from 'svelte';

  let { onLogin }: { onLogin?: () => void } = $props();

  let username  = $state('');
  let password  = $state('');
  let error     = $state('');
  let loading   = $state(false);
  let usernameEl: HTMLInputElement;

  async function login() {
    if (!username || !password) { error = 'Username and password are required'; return; }
    loading = true; error = '';
    try {
      const res = await fetch('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      });
      if (res.ok) {
        onLogin?.();
      } else {
        const d = await res.json().catch(() => ({}));
        error = d.error ?? 'Login failed';
      }
    } catch { error = 'Connection error'; }
    finally { loading = false; }
  }

  onMount(() => { usernameEl?.focus(); });
</script>

<div class="login-wrap">
  <div class="login-box">
    <div class="login-logo">
      <span class="logo-w">web</span><span class="logo-ux">ux</span>
    </div>
    <div class="login-tagline">LINUX PANEL</div>

    {#if error}
      <div class="login-error">{error}</div>
    {/if}

    <div class="field">
      <label for="lf-user">Username</label>
      <input id="lf-user" type="text" class="lf-input"
        bind:this={usernameEl}
        bind:value={username}
        placeholder="root"
        autocomplete="username"
        onkeydown={(e) => e.key === 'Enter' && login()} />
    </div>

    <div class="field">
      <label for="lf-pass">Password</label>
      <input id="lf-pass" type="password" class="lf-input"
        bind:value={password}
        placeholder="••••••••"
        autocomplete="current-password"
        onkeydown={(e) => e.key === 'Enter' && login()} />
    </div>

    <button class="lf-btn" onclick={login} disabled={loading}>
      {loading ? 'Signing in…' : 'Sign in'}
    </button>

    <div class="login-hint">Access is logged · AGPL-3.0</div>
  </div>
</div>

<style>
/* Full-viewport centering */
.login-wrap {
  position: fixed;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-base);
}

.login-box {
  width: 340px;
  background: var(--bg-panel);
  border: 1px solid var(--border-default);
  border-radius: var(--r-lg);
  padding: 2rem 1.75rem 1.5rem;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.login-logo {
  font-family: var(--font-mono);
  font-size: 2rem;
  font-weight: 700;
  letter-spacing: -0.04em;
  margin-bottom: 0.2rem;
}
.logo-w   { color: var(--text-primary); }
.logo-ux  { color: var(--accent); }

.login-tagline {
  font-family: var(--font-mono);
  font-size: 0.55rem;
  letter-spacing: 0.22em;
  color: var(--text-tertiary);
  margin-bottom: 1.75rem;
}

.login-error {
  width: 100%;
  background: rgba(255,80,80,0.1);
  border: 1px solid var(--red);
  color: var(--red);
  border-radius: var(--r-md);
  padding: 0.5rem 0.75rem;
  font-size: 0.8rem;
  margin-bottom: 1rem;
  text-align: center;
}

.field {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  margin-bottom: 0.875rem;
}
.field label {
  font-size: 0.72rem;
  font-weight: 500;
  color: var(--text-secondary);
}
.lf-input {
  width: 100%;
  background: var(--bg-raised);
  border: 1px solid var(--border-default);
  border-radius: var(--r-md);
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 0.9rem;
  padding: 0.55rem 0.75rem;
  outline: none;
  transition: border-color 0.15s;
  box-sizing: border-box;
}
.lf-input:focus { border-color: var(--accent); }

.lf-btn {
  width: 100%;
  margin-top: 0.25rem;
  padding: 0.65rem;
  background: var(--accent);
  color: var(--bg-base);
  border: none;
  border-radius: var(--r-md);
  font-family: var(--font-mono);
  font-size: 0.9rem;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s;
}
.lf-btn:hover   { opacity: 0.88; }
.lf-btn:disabled { opacity: 0.5; cursor: not-allowed; }

.login-hint {
  margin-top: 1.25rem;
  font-size: 0.65rem;
  color: var(--text-tertiary);
  text-align: center;
}
</style>
