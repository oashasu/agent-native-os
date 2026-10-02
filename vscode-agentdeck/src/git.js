'use strict';
const { execFile } = require('child_process');
const { parseStatus } = require('./model');

function git(cwd, args, opts = {}) {
  return new Promise((resolve, reject) => {
    execFile('git', ['-C', cwd, ...args], { maxBuffer: 64 * 1024 * 1024, encoding: 'buffer', ...opts }, (err, stdout, stderr) => {
      if (err) {
        const e = new Error(String(stderr || err.message).trim());
        e.code = err.code;
        return reject(e);
      }
      resolve(stdout);
    });
  });
}

/** Changed files of the repo containing cwd. Returns {root, changes[]} or null when cwd is not in a repo. */
async function changes(cwd) {
  let root;
  try {
    root = String(await git(cwd, ['rev-parse', '--show-toplevel'])).trim();
  } catch (_) {
    return null;
  }
  const out = await git(root, ['status', '--porcelain=v1', '-z']);
  return { root, changes: parseStatus(out) };
}

/** File content at a ref (empty string when the path does not exist there). */
async function show(root, ref, rel) {
  try {
    return String(await git(root, ['show', `${ref}:${rel}`]));
  } catch (_) {
    return '';
  }
}

module.exports = { changes, show };
