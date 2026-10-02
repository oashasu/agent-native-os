'use strict';
const test = require('node:test');
const assert = require('node:assert');
const M = require('../src/model');

const now = Date.parse('2026-10-02T12:00:00Z');
const mk = (o) => ({ provider: 'claude', id: 'x', title: 't', cwd: '/work/a', updated: '2026-10-02T11:00:00Z', ...o });

test('groupSessions: exclusive groups with priority live > pinned > marked > recent', () => {
  const s = [
    mk({ id: '1', live: true, pinned: true, status: 'wait' }), // live wins over pinned/marked
    mk({ id: '2', pinned: true, status: 'block' }),            // pinned wins over marked
    mk({ id: '3', status: 'doing' }),
    mk({ id: '4', status: 'done' }),                           // "done" is not "marked": falls into recent
    mk({ id: '5' }),
  ];
  const g = Object.fromEntries(M.groupSessions(s).map((x) => [x.id, x.items.map((i) => i.id)]));
  assert.deepStrictEqual(g.live, ['1']);
  assert.deepStrictEqual(g.pinned, ['2']);
  assert.deepStrictEqual(g.marked, ['3']);
  assert.deepStrictEqual([...g.recent].sort(), ['4', '5']);
  // every session appears exactly once
  const all = M.groupSessions(s).flatMap((x) => x.items.map((i) => i.id));
  assert.strictEqual(all.length, new Set(all).size);
  assert.strictEqual(all.length, s.length);
});

test('groupSessions: temp-dir sessions hidden unless live; empty groups dropped; overflow goes to collapsed "older"', () => {
  const s = [
    mk({ id: 'tmp', cwd: '/private/tmp/x' }),
    mk({ id: 'tmplive', cwd: '/var/folders/ab/c', live: true }),
    ...Array.from({ length: M.RECENT_LIMIT + 3 }, (_, i) => mk({ id: `r${i}`, updated: new Date(now - i * 1000).toISOString() })),
  ];
  const groups = M.groupSessions(s);
  const ids = groups.flatMap((g) => g.items.map((i) => i.id));
  assert.ok(!ids.includes('tmp'));
  assert.ok(ids.includes('tmplive'));
  assert.deepStrictEqual(groups.map((g) => g.id), ['live', 'recent', 'older']);
  assert.strictEqual(groups.find((g) => g.id === 'recent').items.length, M.RECENT_LIMIT);
  assert.strictEqual(groups.find((g) => g.id === 'older').collapsed, true);
  assert.strictEqual(M.groupSessions(s, { showTmp: true }).flatMap((g) => g.items).length, s.length);
});

test('groupSessions: recent is newest-first', () => {
  const g = M.groupSessions([
    mk({ id: 'old', updated: '2026-09-01T00:00:00Z' }),
    mk({ id: 'new', updated: '2026-10-01T00:00:00Z' }),
  ]);
  assert.deepStrictEqual(g[0].items.map((i) => i.id), ['new', 'old']);
});

test('describeSession: status, tags, age and memory only when live', () => {
  const d = M.describeSession(mk({ status: 'wait', tags: ['支付'], live: true, rss_mb: 120 }), now);
  assert.strictEqual(d, '[等我] · claude · a · #支付 · 1 小时前 · 120MB');
  assert.ok(!M.describeSession(mk({ rss_mb: 120 }), now).includes('MB'));
});

test('ago buckets', () => {
  assert.strictEqual(M.ago('2026-10-02T11:59:50Z', now), '刚刚');
  assert.strictEqual(M.ago('2026-10-02T11:30:00Z', now), '30 分钟前');
  assert.strictEqual(M.ago('2026-10-01T12:00:00Z', now), '1 天前');
  assert.strictEqual(M.ago('2026-10-02T13:00:00Z', now), '刚刚'); // clock skew never yields negatives
});

test('parseStatus: modified / untracked / deleted / added / rename (-z, old path follows)', () => {
  const buf = [' M src/a.js', '?? new file.txt', ' D gone.js', 'A  added.js', 'R  renamed.js\0old name.js', 'MM both.js', ''].join('\0')
    .replace('renamed.js\0\0old name.js', 'renamed.js\0old name.js');
  const out = M.parseStatus(buf);
  const by = Object.fromEntries(out.map((c) => [c.path, c]));
  assert.strictEqual(by['src/a.js'].kind, 'modified');
  assert.strictEqual(by['new file.txt'].kind, 'untracked');
  assert.strictEqual(by['gone.js'].kind, 'deleted');
  assert.strictEqual(by['added.js'].kind, 'added');
  assert.strictEqual(by['renamed.js'].kind, 'renamed');
  assert.strictEqual(by['renamed.js'].orig, 'old name.js');
  assert.strictEqual(by['both.js'].kind, 'modified');
  assert.strictEqual(out.length, 6, 'the rename old-path field must not become its own entry');
});

test('parseStatus: empty output means clean tree', () => {
  assert.deepStrictEqual(M.parseStatus(''), []);
});
