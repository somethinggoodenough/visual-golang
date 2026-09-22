import { spawn } from 'node:child_process';
import path from 'node:path';
import { buildServer, goEnv, npm, root, serverBinary } from './shared.mjs';

buildServer();
const children = [];
let stopping = false;
function stop(code = 0) {
  if (stopping) return;
  stopping = true;
  for (const child of children) {
    if (!child.pid) continue;
    try { process.platform === 'win32' ? child.kill('SIGTERM') : process.kill(-child.pid, 'SIGTERM'); } catch {}
  }
  setTimeout(() => process.exit(code), 350);
}
function start(command, args, cwd, env = process.env) {
  const child = spawn(command, args, { cwd, env, stdio: 'inherit', detached: process.platform !== 'win32', shell: process.platform === 'win32' && command.endsWith('.cmd') });
  children.push(child);
  child.on('error', error => { console.error(error.message); stop(1); });
  child.on('exit', code => { if (!stopping) stop(code || 0); });
}
process.on('SIGINT', () => stop());
process.on('SIGTERM', () => stop());
start(serverBinary, ['-static', ''], path.join(root, 'backend'), goEnv);
start(npm, ['run', 'dev', '--', '--port', '5173', '--strictPort'], path.join(root, 'frontend'));
