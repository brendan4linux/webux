// Global package-operation store — survives component unmount/remount so
// operations keep streaming while the user navigates away and can be
// resumed when they return to the Packages page.

class PkgOpStore {
  output   = $state<string[]>([]);
  running  = $state(false);
  title    = $state('');
  /** Set true when an op finishes so the page can refresh its lists. */
  needsRefresh = $state(false);

  async start(url: string, body: unknown, opTitle: string): Promise<void> {
    this.running     = true;
    this.output      = [];
    this.title       = opTitle;
    this.needsRefresh = false;
    try {
      const resp = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!resp.body) throw new Error('No response body');
      const reader = resp.body.getReader();
      const dec    = new TextDecoder();
      let buf = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const parts = buf.split('\n');
        for (const line of parts.slice(0, -1)) {
          if (line.startsWith('data: ')) this.output = [...this.output, line.slice(6)];
        }
        buf = parts[parts.length - 1];
      }
    } catch (e: unknown) {
      this.output = [...this.output, '[error] ' + String(e)];
    } finally {
      this.running     = false;
      this.needsRefresh = true;
    }
  }

  clear() {
    this.output      = [];
    this.title       = '';
    this.needsRefresh = false;
  }
}

export const pkgOp = new PkgOpStore();
