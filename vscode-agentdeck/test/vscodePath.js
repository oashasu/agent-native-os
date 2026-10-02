'use strict';
const fs = require('fs');
const path = require('path');
const { downloadAndUnzipVSCode } = require('@vscode/test-electron');

/** Download (once) a portable VS Code and return its real executable. Newer builds renamed MacOS/Electron -> MacOS/Code. */
async function vscodeExecutable() {
  const exe = await downloadAndUnzipVSCode('stable');
  if (fs.existsSync(exe)) return exe;
  const alt = path.join(path.dirname(exe), 'Code');
  if (fs.existsSync(alt)) return alt;
  throw new Error(`VS Code executable not found next to ${exe}`);
}
module.exports = { vscodeExecutable };
