'use strict';
const http = require('http');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawn } = require('child_process');

const home = () => process.env.AGENTDECK_HOME || os.homedir();

/** Client for the local agentdeck server (same API the standalone web window uses). */
class Api {
  /** @param {() => {port:number, binaryPath:string, autoStart:boolean, tmuxSocket:string, extensionPath:string}} getCfg */
  constructor(getCfg) {
    this.getCfg = getCfg;
  }

  token() {
    try {
      return fs.readFileSync(path.join(home(), '.config', 'agentdeck', 'token'), 'utf8').trim();
    } catch (_) {
      return '';
    }
  }

  request(method, urlPath, body) {
    const { port } = this.getCfg();
    return new Promise((resolve, reject) => {
      const data = body === undefined ? null : Buffer.from(JSON.stringify(body));
      const headers = { Cookie: `ad_token=${this.token()}`, 'X-AD': '1' };
      if (data) {
        headers['Content-Type'] = 'application/json';
        headers['Content-Length'] = data.length;
      }
      const req = http.request({ host: '127.0.0.1', port, path: urlPath, method, headers, timeout: 8000 }, (res) => {
        const chunks = [];
        res.on('data', (c) => chunks.push(c));
        res.on('end', () => {
          const text = Buffer.concat(chunks).toString('utf8');
          let json;
          try { json = text ? JSON.parse(text) : {}; } catch (_) { json = {}; }
          if (res.statusCode >= 200 && res.statusCode < 300) return resolve(json);
          const err = new Error(json.error || text.trim() || `HTTP ${res.statusCode}`);
          err.status = res.statusCode;
          reject(err);
        });
      });
      req.on('timeout', () => req.destroy(new Error('请求超时')));
      req.on('error', reject);
      if (data) req.write(data);
      req.end();
    });
  }

  sessions() { return this.request('GET', '/api/sessions'); }
  preview(provider, id) { return this.request('GET', `/api/preview?provider=${provider}&id=${encodeURIComponent(id)}`); }
  resume(provider, id) { return this.request('POST', '/api/resume', { provider, id }); }
  create(provider, cwd) { return this.request('POST', '/api/new', { provider, cwd }); }
  sleep(tmux) { return this.request('POST', '/api/sleep', { tmux }); }
  meta(provider, id, m) { return this.request('POST', '/api/meta', { provider, id, ...m }); }

  async isUp() {
    try { await this.sessions(); return true; } catch (e) { return e.status === 401; /* up but token not ready yet */ }
  }

  findBinary() {
    const cfg = this.getCfg();
    const cands = [
      cfg.binaryPath,
      path.join(cfg.extensionPath, '..', '.bin', 'agentdeck'),
      path.join(os.homedir(), 'agent-native-os', '.bin', 'agentdeck'),
    ].filter(Boolean);
    for (const c of cands) if (fs.existsSync(c)) return c;
    for (const d of (process.env.PATH || '').split(path.delimiter)) {
      const p = path.join(d, 'agentdeck');
      if (fs.existsSync(p)) return p;
    }
    return null;
  }

  /** Start the server if it is not running. Resolves true when reachable. */
  async ensureServer() {
    if (await this.isUp()) return true;
    const cfg = this.getCfg();
    if (!cfg.autoStart) return false;
    const bin = this.findBinary();
    if (!bin) throw new Error('找不到 agentdeck 可执行文件：先在仓库运行 `cd agentdeck && go build -o ../.bin/agentdeck .`，或设置 agentdeck.binaryPath');
    // GUI-launched VS Code has a minimal PATH: make sure tmux (homebrew) is findable by the server
    const env = { ...process.env, PATH: `/opt/homebrew/bin:/usr/local/bin:${process.env.PATH || ''}`, AGENTDECK_SOCKET: cfg.tmuxSocket };
    const child = spawn(bin, ['-port', String(cfg.port), 'serve'], { detached: true, stdio: 'ignore', env });
    child.unref();
    for (let i = 0; i < 40; i++) {
      await new Promise((r) => setTimeout(r, 100));
      if (await this.isUp()) return true;
    }
    return false;
  }
}

module.exports = { Api, home };
