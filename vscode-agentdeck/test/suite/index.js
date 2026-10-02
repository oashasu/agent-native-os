'use strict';
const vscode = require('vscode');
const assert = require('assert');
const path = require('path');
const fs = require('fs');
const { execFileSync } = require('child_process');

const SOCKET = process.env.AD_TEST_SOCKET;
const REPO = process.env.AD_TEST_REPO;
const tmux = (...a) => { try { return execFileSync('tmux', ['-L', SOCKET, ...a], { encoding: 'utf8' }); } catch (e) { return ''; } };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function waitFor(fn, what, ms = 20000) {
  const t0 = Date.now();
  for (;;) {
    const v = await fn();
    if (v) return v;
    if (Date.now() - t0 > ms) throw new Error(`timeout waiting for: ${what}`);
    await sleep(150);
  }
}

const tests = [];
const test = (name, fn) => tests.push({ name, fn });
let ext;
const byId = (id) => ext.getSessions().find((s) => s.id === id);

test('extension activates and the server is started automatically; fixture sessions appear', async () => {
  const e = vscode.extensions.getExtension('local.agentdeck');
  assert.ok(e, 'extension not found');
  ext = await e.activate();
  const list = await waitFor(() => { const l = ext.getSessions(); return l.length >= 2 && l; }, 'two fixture sessions');
  assert.deepStrictEqual(new Set(list.map((s) => s.title)), new Set(['补单元测试', '支付回调排查']));
  assert.ok(list.every((s) => !s.live), 'nothing should be live yet');
});

test('opening a dormant session shows a read-only preview and starts NO process', async () => {
  const s = byId(process.env.AD_TEST_SID1);
  await vscode.commands.executeCommand('agentdeck.open', s);
  const doc = await waitFor(() => vscode.window.activeTextEditor && vscode.window.activeTextEditor.document.uri.scheme === 'agentdeck-preview' && vscode.window.activeTextEditor.document, 'preview editor');
  const text = doc.getText();
  assert.ok(text.includes('修复支付回调 bug') && text.includes('已定位到验签逻辑'), 'preview must contain the transcript: ' + text.slice(0, 200));
  assert.strictEqual(tmux('ls').trim(), '', 'previewing must not start any agent');
});

test('changes view lists modified + untracked files of the session directory', async () => {
  const got = await waitFor(() => { const c = ext.state.changes.items; return c.length === 2 && c; }, 'two changes');
  const by = Object.fromEntries(got.map((c) => [c.path, c.kind]));
  assert.deepStrictEqual(by, { 'a.txt': 'modified', 'new.txt': 'untracked' });
});

test('openChange on a modified file opens a native diff editor (HEAD vs working tree)', async () => {
  const c = ext.state.changes.items.find((x) => x.kind === 'modified');
  await vscode.commands.executeCommand('agentdeck.openChange', c);
  const input = await waitFor(() => { const t = vscode.window.tabGroups.activeTabGroup.activeTab; return t && t.input && t.input.original && t.input; }, 'diff tab');
  assert.strictEqual(input.original.scheme, 'agentdeck-git');
  assert.ok(input.modified.fsPath.endsWith(path.join('proj', 'a.txt')), 'right side must be the working-tree file: ' + input.modified.fsPath);
  const left = (await vscode.workspace.openTextDocument(input.original)).getText();
  assert.strictEqual(left, 'one\n', 'left side must be the committed (HEAD) content');
});

test('resume: agent starts under tmux with --resume <id>, and a VS Code terminal is attached to it', async () => {
  const s = byId(process.env.AD_TEST_SID1);
  await vscode.commands.executeCommand('agentdeck.resume', s);
  const name = `ad-claude-${s.id.slice(0, 8)}`;
  await waitFor(() => tmux('ls').includes(name), 'tmux session');
  await waitFor(() => tmux('capture-pane', '-p', '-t', name).includes(`FAKE-CLAUDE --resume ${s.id}`), 'claude launched with --resume');
  await waitFor(() => tmux('list-clients', '-t', name).trim().length > 0, 'a client attached (the VS Code terminal)');
  assert.ok(vscode.window.terminals.some((t) => t.name.includes('支付回调排查')), 'terminal tab not created');
  await waitFor(() => byId(s.id).live, 'session reported live');
});

test('typing in the VS Code terminal reaches the agent', async () => {
  const s = byId(process.env.AD_TEST_SID1);
  const name = `ad-claude-${s.id.slice(0, 8)}`;
  const term = vscode.window.terminals.find((t) => t.name.includes('支付回调排查'));
  term.sendText('hello-from-vscode', true);
  await waitFor(() => tmux('capture-pane', '-p', '-t', name).includes('hello-from-vscode'), 'echoed input');
});

test('closing the terminal tab does NOT stop the agent (the core promise)', async () => {
  const s = byId(process.env.AD_TEST_SID1);
  const name = `ad-claude-${s.id.slice(0, 8)}`;
  vscode.window.terminals.filter((t) => t.name.includes('支付回调排查')).forEach((t) => t.dispose());
  await waitFor(() => tmux('list-clients', '-t', name).trim() === '', 'client detached');
  assert.ok(tmux('ls').includes(name), 'agent died when the tab was closed');
  await waitFor(() => byId(s.id).live, 'still reported live');
});

test('re-opening a live session re-attaches to the same agent (no second process)', async () => {
  const s = byId(process.env.AD_TEST_SID1);
  const name = `ad-claude-${s.id.slice(0, 8)}`;
  await vscode.commands.executeCommand('agentdeck.open', byId(s.id));
  await waitFor(() => tmux('list-clients', '-t', name).trim().length > 0, 're-attached');
  assert.strictEqual(tmux('ls').trim().split('\n').length, 1, 'exactly one tmux session expected');
  assert.ok(tmux('capture-pane', '-p', '-t', name).includes('hello-from-vscode'), 'earlier conversation state must still be there');
});

test('pin / meta round-trips through the server and shows up in the next refresh', async () => {
  const s = byId(process.env.AD_TEST_SID2);
  await vscode.commands.executeCommand('agentdeck.togglePin', s);
  await waitFor(() => byId(s.id).pinned === true, 'pinned');
  await ext.api.meta(s.provider, s.id, { status: 'wait', tags: ['支付'], pinned: true, alias: '' });
  await ext.refresh();
  assert.strictEqual(byId(s.id).status, 'wait');
  assert.deepStrictEqual(byId(s.id).tags, ['支付']);
});

test('lens switch: IDE lens focuses the last code file, Agent lens focuses the agent terminal', async () => {
  const file = vscode.Uri.file(path.join(REPO, 'a.txt'));
  await vscode.window.showTextDocument(file, { preview: false });
  const s = byId(process.env.AD_TEST_SID1);
  await vscode.commands.executeCommand('agentdeck.open', s);           // agent side
  await waitFor(() => vscode.window.activeTerminal, 'active terminal');
  await vscode.commands.executeCommand('agentdeck.lensIde');
  await waitFor(() => vscode.window.activeTextEditor && vscode.window.activeTextEditor.document.uri.fsPath === file.fsPath, 'code editor focused');
  await vscode.commands.executeCommand('agentdeck.lensAgent');
  await waitFor(() => vscode.window.activeTerminal && vscode.window.activeTerminal.name.includes('支付回调排查'), 'agent terminal focused');
});

test('sleep: process is gone, terminal closed, session returns to dormant (still resumable)', async () => {
  const s = byId(process.env.AD_TEST_SID1);
  const name = `ad-claude-${s.id.slice(0, 8)}`;
  await vscode.commands.executeCommand('agentdeck.sleep', byId(s.id));
  await waitFor(() => !tmux('ls').includes(name), 'tmux session killed');
  await waitFor(() => byId(s.id) && !byId(s.id).live, 'reported dormant');
  assert.ok(!vscode.window.terminals.some((t) => t.name.includes('支付回调排查')));
});

exports.run = async function run() {
  const failures = [];
  for (const t of tests) {
    try {
      await t.fn();
      console.log(`  ✔ ${t.name}`);
    } catch (e) {
      failures.push(t.name);
      console.log(`  ✖ ${t.name}\n      ${e && e.message}`);
    }
  }
  console.log(`\n${tests.length - failures.length}/${tests.length} integration checks passed`);
  if (failures.length) throw new Error(`${failures.length} failed: ${failures.join(' | ')}`);
};
