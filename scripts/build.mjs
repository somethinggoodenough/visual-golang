import path from 'node:path';
import { buildServer, npm, root, run } from './shared.mjs';
run(npm, ['run', 'build'], path.join(root, 'frontend'));
buildServer();
