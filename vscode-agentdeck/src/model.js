'use strict';
// Pure logic, no vscode import: unit-testable with `node --test`.

const STATUS_LABEL = { doing: '进行中', wait: '等我', block: '阻塞', done: '完成' };
const RECENT_LIMIT = 40;
const TMP_RE = /^\/(private\/)?(tmp|var\/folders)(\/|$)|^\/private\/var\//;

const sessionKey = (s) => `${s.provider}:${s.id}`;
const titleOf = (s) => s.alias || s.title;
const baseName = (p) => (p || '').split('/').filter(Boolean).pop() || p || '';

function ago(iso, now = Date.now()) {
  const s = (now - new Date(iso).getTime()) / 1000;
  if (!(s >= 0)) return '刚刚';
  if (s < 60) return '刚刚';
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`;
  if (s < 86400 * 30) return `${Math.floor(s / 86400)} 天前`;
  return new Date(iso).toLocaleDateString('zh-CN');
}

/**
 * Exclusive groups (a session shows up once): live > pinned > marked(status) > recent > older.
 * Temp-dir sessions that are not live are hidden unless showTmp.
 */
function groupSessions(sessions, { showTmp = false } = {}) {
  const visible = sessions.filter((s) => s.live || showTmp || !TMP_RE.test(s.cwd || ''));
  const byUpdated = (a, b) => new Date(b.updated) - new Date(a.updated);
  const used = new Set();
  const take = (pred) => {
    const out = visible.filter((s) => !used.has(sessionKey(s)) && pred(s)).sort(byUpdated);
    out.forEach((s) => used.add(sessionKey(s)));
    return out;
  };
  const live = take((s) => s.live);
  const pinned = take((s) => s.pinned);
  const marked = take((s) => s.status && s.status !== 'done');
  const rest = take(() => true);
  const groups = [
    { id: 'live', label: '运行中', items: live, collapsed: false },
    { id: 'pinned', label: '置顶', items: pinned, collapsed: false },
    { id: 'marked', label: '已标记', items: marked, collapsed: false },
    { id: 'recent', label: '最近', items: rest.slice(0, RECENT_LIMIT), collapsed: false },
    { id: 'older', label: '更早', items: rest.slice(RECENT_LIMIT), collapsed: true },
  ];
  return groups.filter((g) => g.items.length > 0);
}

function describeSession(s, now = Date.now()) {
  const parts = [s.provider, baseName(s.cwd)];
  if (s.status) parts.unshift(`[${STATUS_LABEL[s.status] || s.status}]`);
  for (const t of s.tags || []) parts.push(`#${t}`);
  parts.push(ago(s.updated, now));
  if (s.live && s.rss_mb) parts.push(`${s.rss_mb}MB`);
  return parts.join(' · ');
}

/** Parse `git status --porcelain=v1 -z`. Rename/copy entries carry the old path as the next NUL field. */
function parseStatus(buf) {
  const fields = String(buf).split('\0');
  const out = [];
  for (let i = 0; i < fields.length; i++) {
    const f = fields[i];
    if (f.length < 4) continue;
    const x = f[0];
    const y = f[1];
    const path = f.slice(3);
    let orig;
    if (x === 'R' || x === 'C' || y === 'R' || y === 'C') orig = fields[++i];
    out.push({ x, y, path, orig, kind: kindOf(x, y) });
  }
  return out;
}

function kindOf(x, y) {
  if (x === '?' && y === '?') return 'untracked';
  if (x === 'D' || y === 'D') return 'deleted';
  if (x === 'A') return 'added';
  if (x === 'R' || y === 'R') return 'renamed';
  return 'modified';
}

const KIND_BADGE = { untracked: 'U', deleted: 'D', added: 'A', renamed: 'R', modified: 'M' };

module.exports = { STATUS_LABEL, sessionKey, titleOf, baseName, ago, groupSessions, describeSession, parseStatus, kindOf, KIND_BADGE, RECENT_LIMIT };
