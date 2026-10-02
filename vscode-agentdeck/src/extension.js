'use strict';
const vscode = require('vscode');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { Api } = require('./api');
const gitx = require('./git');
const M = require('./model');

const STATUS_COLOR = { doing: 'charts.blue', wait: 'charts.yellow', block: 'charts.red', done: 'disabledForeground' };

function activate(context) {
  const getCfg = () => {
    const c = vscode.workspace.getConfiguration('agentdeck');
    return {
      port: c.get('port', 47017),
      binaryPath: c.get('binaryPath', ''),
      autoStart: c.get('autoStart', true),
      tmuxSocket: c.get('tmuxSocket', 'agentdeck'),
      refreshSeconds: c.get('refreshSeconds', 5),
      extensionPath: context.extensionPath,
    };
  };
  const api = new Api(getCfg);

  const state = {
    sessions: [],
    offline: false,
    currentKey: null,
    terminals: new Map(), // tmux session name -> vscode.Terminal
    lastAgentTerminal: null,
    lastCodeUri: null,
    changes: { root: null, items: [], note: '' },
  };
  const byKey = () => new Map(state.sessions.map((s) => [M.sessionKey(s), s]));
  const current = () => byKey().get(state.currentKey) || null;
  const fail = (e) => vscode.window.showErrorMessage(`AgentDeck: ${e && e.message ? e.message : e}`);
  const guard = (fn) => async (...a) => { try { return await fn(...a); } catch (e) { fail(e); } };
  const resolve = (arg) => (arg && arg.type === 'session' ? arg.s : arg && arg.provider ? arg : current());

  // ---------- sessions tree ----------
  const sessionsEmitter = new vscode.EventEmitter();
  const sessionsProvider = {
    onDidChangeTreeData: sessionsEmitter.event,
    getTreeItem(el) {
      if (el.type === 'group') {
        const it = new vscode.TreeItem(`${el.g.label} (${el.g.items.length})`,
          el.g.collapsed ? vscode.TreeItemCollapsibleState.Collapsed : vscode.TreeItemCollapsibleState.Expanded);
        it.id = `group:${el.g.id}`;
        return it;
      }
      const s = el.s;
      const it = new vscode.TreeItem(`${s.pinned ? '★ ' : ''}${M.titleOf(s)}`, vscode.TreeItemCollapsibleState.None);
      it.id = M.sessionKey(s);
      it.description = M.describeSession(s);
      it.tooltip = `${M.titleOf(s)}\n${s.cwd}\n${s.live ? '运行中' : '已休眠（未占用内存）'}`;
      const color = s.status ? new vscode.ThemeColor(STATUS_COLOR[s.status]) : undefined;
      it.iconPath = s.live
        ? new vscode.ThemeIcon('circle-filled', new vscode.ThemeColor('charts.green'))
        : new vscode.ThemeIcon(s.status ? 'record' : 'circle-outline', color);
      it.contextValue = s.live ? 'session.live' : 'session.dormant';
      it.command = { command: 'agentdeck.open', title: '打开', arguments: [s] };
      return it;
    },
    getChildren(el) {
      if (!el) return M.groupSessions(state.sessions).map((g) => ({ type: 'group', g }));
      if (el.type === 'group') return el.g.items.map((s) => ({ type: 'session', s }));
      return [];
    },
  };
  const sessionsView = vscode.window.createTreeView('agentdeck.sessions', { treeDataProvider: sessionsProvider });

  // ---------- changes tree ----------
  const changesEmitter = new vscode.EventEmitter();
  const changesProvider = {
    onDidChangeTreeData: changesEmitter.event,
    getTreeItem(el) {
      if (el.note) {
        const it = new vscode.TreeItem(el.note, vscode.TreeItemCollapsibleState.None);
        it.iconPath = new vscode.ThemeIcon('info');
        return it;
      }
      const c = el.c;
      const it = new vscode.TreeItem(path.basename(c.path), vscode.TreeItemCollapsibleState.None);
      it.id = `change:${c.path}`;
      it.description = `${M.KIND_BADGE[c.kind]}  ${path.dirname(c.path) === '.' ? '' : path.dirname(c.path)}`;
      it.tooltip = `${c.path} (${c.kind})`;
      it.resourceUri = vscode.Uri.file(path.join(state.changes.root, c.path));
      it.command = { command: 'agentdeck.openChange', title: '查看改动', arguments: [c] };
      return it;
    },
    getChildren(el) {
      if (el) return [];
      if (state.changes.note) return [{ note: state.changes.note }];
      return state.changes.items.map((c) => ({ c }));
    },
  };
  const changesView = vscode.window.createTreeView('agentdeck.changes', { treeDataProvider: changesProvider });

  async function refreshChanges() {
    const s = current();
    if (!s) {
      state.changes = { root: null, items: [], note: '在「Agent 会话」里选择一个会话' };
    } else {
      const r = await gitx.changes(s.cwd).catch(() => null);
      state.changes = r
        ? { root: r.root, items: r.changes, note: r.changes.length ? '' : '没有未提交的改动' }
        : { root: null, items: [], note: '该会话目录不是 git 仓库' };
      changesView.description = `${M.titleOf(s)}`;
    }
    changesEmitter.fire();
  }

  // ---------- content providers (read-only virtual docs) ----------
  context.subscriptions.push(
    vscode.workspace.registerTextDocumentContentProvider('agentdeck-git', {
      async provideTextDocumentContent(uri) {
        const q = JSON.parse(uri.query || '{}');
        return gitx.show(q.root, q.ref || 'HEAD', q.rel);
      },
    }),
    vscode.workspace.registerTextDocumentContentProvider('agentdeck-preview', {
      async provideTextDocumentContent(uri) {
        const [, provider, file] = uri.path.split('/');
        const id = file.replace(/\.md$/, '');
        const s = state.sessions.find((x) => x.provider === provider && x.id === id);
        const msgs = await api.preview(provider, id);
        const head = s ? `# ${M.titleOf(s)}\n\n\`${s.cwd}\` · ${provider} · 已休眠（未占用内存）\n\n> 在左侧会话树点 ▶ 恢复并接入。以下是最近的对话：\n\n` : '';
        return head + (msgs.length
          ? msgs.map((m) => `**${m.role === 'user' ? '你' : 'Agent'}**\n\n${m.text}\n`).join('\n---\n\n')
          : '（没有可预览的文本消息）');
      },
    }),
  );

  // ---------- terminals: tmux attach in editor tabs ----------
  const resolveTmux = () => ['/opt/homebrew/bin/tmux', '/usr/local/bin/tmux', '/usr/bin/tmux'].find((p) => fs.existsSync(p)) || 'tmux';

  function attach(s, tmuxName) {
    const existing = state.terminals.get(tmuxName);
    if (existing && existing.exitStatus === undefined) {
      existing.show();
      return existing;
    }
    const t = vscode.window.createTerminal({
      name: `${s.provider === 'claude' ? '◆' : '◇'} ${M.titleOf(s)}`,
      shellPath: resolveTmux(),
      shellArgs: ['-L', getCfg().tmuxSocket, 'attach', '-t', `=${tmuxName}`],
      location: { viewColumn: vscode.ViewColumn.Active },
      env: { TERM: 'xterm-256color' },
    });
    state.terminals.set(tmuxName, t);
    state.lastAgentTerminal = t;
    t.show();
    return t;
  }
  context.subscriptions.push(
    vscode.window.onDidCloseTerminal((t) => {
      for (const [k, v] of state.terminals) if (v === t) state.terminals.delete(k); // session itself keeps running in tmux
      if (state.lastAgentTerminal === t) state.lastAgentTerminal = null;
    }),
    vscode.window.onDidChangeActiveTerminal((t) => {
      if (t && [...state.terminals.values()].includes(t)) state.lastAgentTerminal = t;
    }),
    vscode.window.onDidChangeActiveTextEditor((e) => {
      if (e && e.document.uri.scheme === 'file') state.lastCodeUri = e.document.uri;
    }),
  );

  // ---------- refresh loop + status bar ----------
  const status = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  status.show();
  context.subscriptions.push(status);

  async function refresh() {
    try {
      state.sessions = await api.sessions();
      state.offline = false;
    } catch (_) {
      state.offline = true;
    }
    vscode.commands.executeCommand('setContext', 'agentdeck.offline', state.offline);
    const live = state.sessions.filter((s) => s.live);
    if (state.offline) {
      status.text = '$(debug-disconnect) AgentDeck';
      status.tooltip = '未连接到 agentdeck 服务，点击启动';
      status.command = 'agentdeck.startServer';
    } else {
      status.text = `$(terminal) ${live.length} 运行 · ${live.reduce((a, s) => a + (s.rss_mb || 0), 0)}MB`;
      status.tooltip = '点击快速切换会话（⌘⌥K）';
      status.command = 'agentdeck.quickSwitch';
    }
    sessionsEmitter.fire();
    if (changesView.visible || state.currentKey) await refreshChanges();
  }

  let timer;
  const startTimer = () => {
    clearInterval(timer);
    timer = setInterval(() => refresh().catch(() => {}), Math.max(2, getCfg().refreshSeconds) * 1000);
  };
  context.subscriptions.push({ dispose: () => clearInterval(timer) });

  // ---------- commands ----------
  async function setCurrent(s) {
    state.currentKey = M.sessionKey(s);
    await refreshChanges();
  }

  async function showPreview(s) {
    const uri = vscode.Uri.from({ scheme: 'agentdeck-preview', path: `/${s.provider}/${s.id}.md` });
    const doc = await vscode.workspace.openTextDocument(uri);
    await vscode.window.showTextDocument(doc, { preview: true, preserveFocus: true });
  }

  async function resumeAndAttach(s) {
    const r = s.live ? { tmux: s.tmux } : await api.resume(s.provider, s.id);
    await refresh();
    attach(s, r.tmux);
  }

  const cmd = (id, fn) => context.subscriptions.push(vscode.commands.registerCommand(id, guard(fn)));

  cmd('agentdeck.refresh', refresh);
  cmd('agentdeck.startServer', async () => {
    if (!(await api.ensureServer())) throw new Error('服务没有启动成功');
    await refresh();
  });
  cmd('agentdeck.open', async (arg) => {
    const s = resolve(arg);
    if (!s) return;
    await setCurrent(s);
    if (s.live) attach(s, s.tmux); else await showPreview(s);
  });
  cmd('agentdeck.resume', async (arg) => { const s = resolve(arg); if (s) { await setCurrent(s); await resumeAndAttach(s); } });
  cmd('agentdeck.sleep', async (arg) => {
    const s = resolve(arg);
    if (!s || !s.live) return;
    await api.sleep(s.tmux);
    const t = state.terminals.get(s.tmux);
    if (t) t.dispose();
    await refresh();
  });

  const metaOf = (s) => ({ status: s.status || '', tags: s.tags || [], pinned: !!s.pinned, alias: s.alias || '' });
  const patchMeta = async (s, patch) => { await api.meta(s.provider, s.id, { ...metaOf(s), ...patch }); await refresh(); };

  cmd('agentdeck.setStatus', async (arg) => {
    const s = resolve(arg);
    if (!s) return;
    const pick = await vscode.window.showQuickPick(
      [{ label: '无', v: '' }, ...Object.entries(M.STATUS_LABEL).map(([v, label]) => ({ label, v }))],
      { placeHolder: `标记「${M.titleOf(s)}」的状态` });
    if (pick) await patchMeta(s, { status: pick.v });
  });
  cmd('agentdeck.editTags', async (arg) => {
    const s = resolve(arg);
    if (!s) return;
    const v = await vscode.window.showInputBox({ prompt: '标签（逗号分隔）', value: (s.tags || []).join(', ') });
    if (v !== undefined) await patchMeta(s, { tags: v.split(/[,，]/).map((x) => x.trim()).filter(Boolean) });
  });
  cmd('agentdeck.togglePin', async (arg) => { const s = resolve(arg); if (s) await patchMeta(s, { pinned: !s.pinned }); });
  cmd('agentdeck.rename', async (arg) => {
    const s = resolve(arg);
    if (!s) return;
    const v = await vscode.window.showInputBox({ prompt: '显示名称（留空恢复原标题）', value: s.alias || '' });
    if (v !== undefined) await patchMeta(s, { alias: v });
  });
  cmd('agentdeck.addFolder', async (arg) => {
    const s = resolve(arg);
    if (!s) return;
    const folders = vscode.workspace.workspaceFolders || [];
    if (folders.some((f) => f.uri.fsPath === s.cwd)) return vscode.window.showInformationMessage('该目录已在当前工作区');
    vscode.workspace.updateWorkspaceFolders(folders.length, 0, { uri: vscode.Uri.file(s.cwd), name: path.basename(s.cwd) });
  });

  async function newSession(provider, arg) {
    const s = arg && (arg.type === 'session' || arg.provider) ? resolve(arg) : null;
    let cwd = s ? s.cwd : (vscode.workspace.workspaceFolders || [])[0]?.uri.fsPath;
    if (!cwd) cwd = await vscode.window.showInputBox({ prompt: '在哪个目录新建会话？', value: os.homedir() });
    if (!cwd) return;
    const r = await api.create(provider, cwd);
    await refresh();
    const created = state.sessions.find((x) => x.tmux === r.tmux) || { provider, id: r.tmux, title: '新会话', cwd, live: true, tmux: r.tmux };
    attach(created, r.tmux);
  }
  cmd('agentdeck.newClaude', (arg) => newSession('claude', arg));
  cmd('agentdeck.newCodex', (arg) => newSession('codex', arg));

  cmd('agentdeck.quickSwitch', async () => {
    const list = [...state.sessions].sort((a, b) => (b.live - a.live) || (new Date(b.updated) - new Date(a.updated)));
    const qp = vscode.window.createQuickPick();
    qp.placeholder = '搜索会话（标题 / 目录 / 标签），回车打开；休眠的会自动恢复';
    qp.matchOnDescription = true;
    qp.matchOnDetail = true;
    qp.items = list.map((s) => ({
      label: `${s.live ? '$(circle-filled)' : '$(circle-outline)'} ${M.titleOf(s)}`,
      description: M.describeSession(s),
      detail: s.cwd,
      s,
    }));
    qp.onDidAccept(() => {
      const it = qp.selectedItems[0];
      qp.hide();
      if (it) setCurrent(it.s).then(() => resumeAndAttach(it.s)).catch(fail);
    });
    qp.onDidHide(() => qp.dispose());
    qp.show();
  });

  // Two "lenses" over the same window: switching never closes anything, each side keeps its own focus target.
  cmd('agentdeck.lensAgent', async () => {
    await vscode.commands.executeCommand('workbench.view.extension.agentdeck');
    const t = state.lastAgentTerminal;
    if (t && t.exitStatus === undefined) t.show(); else await vscode.commands.executeCommand('agentdeck.quickSwitch');
  });
  cmd('agentdeck.lensIde', async () => {
    await vscode.commands.executeCommand('workbench.view.explorer');
    if (state.lastCodeUri) {
      await vscode.window.showTextDocument(state.lastCodeUri, { preserveFocus: false, preview: false });
    } else {
      await vscode.commands.executeCommand('workbench.action.focusFirstEditorGroup');
    }
  });

  cmd('agentdeck.openChange', async (c) => {
    const root = state.changes.root;
    if (!root || !c) return;
    const right = vscode.Uri.file(path.join(root, c.path));
    const rel = c.orig || c.path;
    const left = vscode.Uri.from({ scheme: 'agentdeck-git', path: `/${rel}`, query: JSON.stringify({ root, ref: 'HEAD', rel }) });
    if (c.kind === 'untracked' || c.kind === 'added') return vscode.commands.executeCommand('vscode.open', right);
    if (c.kind === 'deleted') return vscode.commands.executeCommand('vscode.open', left);
    return vscode.commands.executeCommand('vscode.diff', left, right, `${path.basename(c.path)}（HEAD ↔ 工作区）`);
  });

  context.subscriptions.push(
    sessionsView, changesView,
    vscode.workspace.onDidChangeConfiguration((e) => { if (e.affectsConfiguration('agentdeck')) startTimer(); }),
  );

  // ---------- boot ----------
  (async () => {
    try {
      await api.ensureServer();
    } catch (e) {
      vscode.window.showWarningMessage(`AgentDeck: ${e.message}`);
    }
    await refresh();
    startTimer();
  })();

  // test hook
  return { api, state, refresh, attach, getSessions: () => state.sessions };
}

function deactivate() {}

module.exports = { activate, deactivate };
