import { spawn } from 'node:child_process';
import assert from 'node:assert/strict';
import path from 'node:path';
import { goEnv, root, serverBinary } from './shared.mjs';

// Validate the same compiled server and static assets used by npm start.
const port = 18082;
const server = spawn(serverBinary, ['-addr', `127.0.0.1:${port}`, '-static', path.join(root, 'frontend/dist')], { env: goEnv, stdio: 'inherit' });
const exited = new Promise(resolve => server.once('exit', resolve));
try {
  let health;
  for (let attempt = 0; attempt < 60; attempt++) {
    try {
      const response = await fetch(`http://127.0.0.1:${port}/api/health`);
      if (response.ok) { health = await response.json(); break; }
    } catch {}
    if (server.exitCode !== null) throw new Error('Production server exited before becoming ready');
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  assert.equal(health?.status, 'ok');
  assert.equal(health.goAvailable, true);
  const response = await fetch(`http://127.0.0.1:${port}/`);
  assert.equal(response.status, 200);
  const html = await response.text();
  const asset = html.match(/src="(\/assets\/[^\"]+\.js)"/)?.[1];
  assert.ok(asset, 'The production HTML must include its built JavaScript entry');
  const js = await fetch(`http://127.0.0.1:${port}${asset}`);
  assert.equal(js.status, 200);
  assert.ok((await js.text()).length > 1000);
  console.log(`Production smoke test passed: frontend assets + ${health.goVersion}`);
} finally {
  server.kill('SIGTERM');
  await exited;
}
