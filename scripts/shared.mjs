import { spawnSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

export const root = fileURLToPath(new URL('../', import.meta.url));
mkdirSync(path.join(root, '.cache'), { recursive: true });
export const goEnv = { ...process.env, GOTOOLCHAIN: 'local', GOWORK: 'off', GOPROXY: 'off', GOSUMDB: 'off', GOCACHE: process.env.GOCACHE || path.join(root, '.cache', 'go-build') };
export const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm';
export const serverBinary = path.join(root, '.cache', process.platform === 'win32' ? 'goviz-server.exe' : 'goviz-server');
export function run(command, args, cwd = root, env = process.env) {
  const result = spawnSync(command, args, { cwd, env, stdio: 'inherit', shell: process.platform === 'win32' && command.endsWith('.cmd') });
  if (result.error) console.error(result.error.message);
  if (result.status !== 0) process.exit(result.status || 1);
}
export function buildServer() { run('go', ['build', '-o', serverBinary, './cmd/server'], path.join(root, 'backend'), goEnv); }
