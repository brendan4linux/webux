import { writable } from 'svelte/store';

export interface EchoEntry {
  id: number;
  cmd: string;
  explanation: string;
  context: string;
  created_at: string;
}

export interface WSEvent {
  type: string;
  payload: any;
  sent_at: string;
}

function createWSStore() {
  const { subscribe, set } = writable<WSEvent | null>(null);
  const { subscribe: subConnected, set: setConnected } = writable(false);

  let ws: WebSocket | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let destroyed = false;

  function connect() {
    if (destroyed) return;
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    ws = new WebSocket(`${proto}//${location.host}/ws`);

    ws.onopen = () => setConnected(true);

    ws.onmessage = (e) => {
      try { set(JSON.parse(e.data)); } catch {}
    };

    ws.onclose = () => {
      setConnected(false);
      if (!destroyed) reconnectTimer = setTimeout(connect, 3000);
    };

    ws.onerror = () => ws?.close();
  }

  connect();

  return {
    subscribe,
    connected: { subscribe: subConnected },
    destroy() {
      destroyed = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      ws?.close();
    }
  };
}

export const wsStore = createWSStore();
