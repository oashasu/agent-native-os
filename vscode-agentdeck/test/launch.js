'use strict';
// Download (once) a portable VS Code into .vscode-test/ and open it with this extension loaded. No system install.
const path = require('path');
const { spawn } = require('child_process');
const { vscodeExecutable } = require('./vscodePath');

(async () => {
  const exe = await vscodeExecutable();
  // launch the app binary directly (the `code` CLI shim is not needed for a dev-extension window)
  const [cli, args] = [exe, []];
  const ext = path.resolve(__dirname, '..');
  const target = process.argv[2] || process.cwd();
  // own profile next to the download: never mixes with another VS Code's settings/extensions
  const base = path.join(ext, '.vscode-test');
  const flags = [`--user-data-dir=${path.join(base, 'user-data')}`, `--extensions-dir=${path.join(base, 'extensions')}`];
  spawn(cli, [...args, ...flags, `--extensionDevelopmentPath=${ext}`, target], { stdio: 'ignore', detached: true }).unref();
})().catch((e) => { console.error(e); process.exit(1); });
