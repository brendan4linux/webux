<script lang="ts">
  import './app.css';
  import Sidebar from '$components/Sidebar.svelte';
  import Topbar from '$components/Topbar.svelte';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  import Dashboard from './routes/Dashboard.svelte';
  import Ports from './routes/ports.svelte';
  import Migration from './routes/migration.svelte';
  import Services from './routes/Services.svelte';
  import Processes from './routes/Processes.svelte';
  import Users from './routes/Users.svelte';
  import Network from './routes/Network.svelte';
  import Firewall from './routes/Firewall.svelte';
  import Containers from './routes/Containers.svelte';
  import Databases from './routes/Databases.svelte';
  import Webservers from './routes/Webservers.svelte';
  import Files from './routes/Files.svelte';
  import Cron from './routes/Cron.svelte';
  import Packages from './routes/Packages.svelte';
  import Puppet from './routes/Puppet.svelte';
  import Terminal from './routes/Terminal.svelte';
  import Ansible from './routes/Ansible.svelte';
  import AIAssistant from './routes/AIAssistant.svelte';
  import Disks from './routes/Disks.svelte';
  import Logs from './routes/Logs.svelte';
  import Settings from './routes/Settings.svelte';
  import Login from './routes/Login.svelte';
  import NotFound from './routes/NotFound.svelte';

  let hash = $state(window.location.hash || '#/');
  let authenticated = $state(false);
  let authChecking = $state(true);

  window.addEventListener('hashchange', () => { hash = window.location.hash || '#/'; });

  async function checkAuth() {
    try {
      const res = await fetch('/auth/whoami');
      authenticated = res.ok;
    } catch { authenticated = false; }
    finally { authChecking = false; }
  }

  function onLogin() {
    authenticated = true;
    window.location.hash = '#/';
    hash = '#/';
  }

  import { onMount } from 'svelte';
  onMount(checkAuth);

  const routes: Record<string, any> = {
    '#/':           Dashboard,
    '#/ports':      Ports,
    '#/migration':  Migration,
    '#/services':   Services,
    '#/processes':  Processes,
    '#/users':      Users,
    '#/network':    Network,
    '#/firewall':   Firewall,
    '#/containers': Containers,
    '#/databases':  Databases,
    '#/webservers': Webservers,
    '#/files':      Files,
    '#/cron':       Cron,
    '#/packages':   Packages,
    '#/puppet':     Puppet,
    '#/terminal':   Terminal,
    '#/ansible':    Ansible,
    '#/ai':         AIAssistant,
    '#/disks':      Disks,
    '#/logs':       Logs,
    '#/settings':   Settings,
  };

  let CurrentPage = $derived(routes[hash] ?? NotFound);
</script>

{#if authChecking}
  <div style="display:flex;align-items:center;justify-content:center;height:100vh;background:var(--bg-base);color:var(--text-tertiary)">Loading…</div>
{:else if !authenticated}
  <Login onLogin={onLogin} />
{:else}
<div class="app-shell">
  <Sidebar currentHash={hash} />
  <div class="main-area">
    <Topbar />
    <div class="page-content">
      <CurrentPage />
    </div>
    <CLIEchoPane />
  </div>
</div>
{/if}
