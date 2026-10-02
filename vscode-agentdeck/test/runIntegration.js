'use strict';
// Launches a real (downloaded, portable) VS Code with the extension loaded and runs test/suite/index.js inside it.
// Fully isolated: temp HOME, own tmux socket, own port, fake `claude` binary. Never touches the user's real sessions.
const path = require('path');
const fs = require('fs');
const os = require('os');
const { execFileSync, spawnSync } = require('child_process');
const { runTests } = require('@vscode/test-electron');
const { vscodeExecutable } = require('./vscodePath');

const PORT = 47031;
const SOCKET = 'adtest';

async function main() {
  const repoRoot = path.resolve(__dirname, '..', '..');
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'adx-'));
  const home = path.join(tmp, 'home');
  const bin = path.join(tmp, 'bin');
  const work = path.join(tmp, 'work');
  const userData = path.join(tmp, 'userdata');
  const extDir = path.join(tmp, 'exts');
  for (const d of [home, bin, work, userData, extDir, path.join(userData, 'User')]) fs.mkdirSync(d, { recursive: true });

  // 1) the real agentdeck binary
  const agentdeckBin = path.join(tmp, 'agentdeck');
  const b = spawnSync('go', ['build', '-o', agentdeckBin, '.'], { cwd: path.join(repoRoot, 'agentdeck'), env: { ...process.env, GOWORK: 'off' }, encoding: 'utf8' });
  if (b.status !== 0) throw new Error('go build failed: ' + b.stderr);

  // 2) fake claude: proves it was started with --resume <id>, then behaves like an interactive process
  fs.writeFileSync(path.join(bin, 'claude'), '#!/bin/sh\necho "FAKE-CLAUDE $@"\nexec cat\n', { mode: 0o755 });
  fs.writeFileSync(path.join(home, '.zprofile'), `export PATH="${bin}:$PATH"\n`);
  fs.writeFileSync(path.join(home, '.zshenv'), `export PATH="${bin}:$PATH"\n`);

  // 3) a git repo with changes, and two claude sessions pointing at it
  const repo = path.join(work, 'proj');
  fs.mkdirSync(repo);
  const g = (...a) => execFileSync('git', ['-C', repo, ...a], { encoding: 'utf8' });
  g('init', '-q'); g('config', 'user.email', 't@t'); g('config', 'user.name', 't');
  fs.writeFileSync(path.join(repo, 'a.txt'), 'one\n');
  g('add', '.'); g('commit', '-qm', 'init');
  fs.writeFileSync(path.join(repo, 'a.txt'), 'one\ntwo\n');      // modified
  fs.writeFileSync(path.join(repo, 'new.txt'), 'brand new\n');   // untracked

  const projDir = path.join(home, '.claude', 'projects', '-proj');
  fs.mkdirSync(projDir, { recursive: true });
  const sid = (n) => `${n}0000000-aaaa-4bbb-8ccc-dddddddddddd`;
  const line = (o) => JSON.stringify(o) + '\n';
  fs.writeFileSync(path.join(projDir, sid(1) + '.jsonl'),
    line({ type: 'user', cwd: repo, message: { role: 'user', content: '修复支付回调 bug' } }) +
    line({ type: 'assistant', message: { role: 'assistant', content: [{ type: 'text', text: '已定位到验签逻辑' }] } }) +
    line({ type: 'ai-title', aiTitle: '支付回调排查' }));
  fs.writeFileSync(path.join(projDir, sid(2) + '.jsonl'),
    line({ type: 'user', cwd: repo, message: { role: 'user', content: '写单元测试' } }) +
    line({ type: 'ai-title', aiTitle: '补单元测试' }));

  fs.writeFileSync(path.join(userData, 'User', 'settings.json'), JSON.stringify({
    'agentdeck.port': PORT,
    'agentdeck.tmuxSocket': SOCKET,
    'agentdeck.binaryPath': agentdeckBin,
    'agentdeck.refreshSeconds': 2,
    'security.workspace.trust.enabled': false,
    'workbench.startupEditor': 'none',
    'telemetry.telemetryLevel': 'off',
    'update.mode': 'none',
  }));

  const env = {
    HOME: home, AGENTDECK_HOME: home, SHELL: '/bin/zsh',
    PATH: `${bin}:/opt/homebrew/bin:/usr/local/bin:${process.env.PATH}`,
    AD_TEST_REPO: repo, AD_TEST_SOCKET: SOCKET, AD_TEST_SID1: sid(1), AD_TEST_SID2: sid(2),
  };
  Object.assign(process.env, env);

  let failed = false;
  try {
    const vscodeExecutablePath = await vscodeExecutable();
    await runTests({
      vscodeExecutablePath,
      extensionDevelopmentPath: path.resolve(__dirname, '..'),
      extensionTestsPath: path.resolve(__dirname, 'suite', 'index.js'),
      launchArgs: [repo, `--user-data-dir=${userData}`, `--extensions-dir=${extDir}`, '--disable-extensions', '--disable-workspace-trust', '--skip-welcome', '--skip-release-notes'],
      extensionTestsEnv: env,
    });
  } catch (e) {
    failed = true;
    console.error('INTEGRATION FAILED:', e && e.message);
  } finally {
    spawnSync('tmux', ['-L', SOCKET, 'kill-server']);
    spawnSync('pkill', ['-f', `agentdeck.* -port ${PORT}`]);
    spawnSync('pkill', ['-f', `${agentdeckBin}`]);
    fs.rmSync(tmp, { recursive: true, force: true });
  }
  process.exit(failed ? 1 : 0);
}
main().catch((e) => { console.error(e); process.exit(1); });
