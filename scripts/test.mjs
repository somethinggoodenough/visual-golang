import path from 'node:path';
import { goEnv, npm, root, run } from './shared.mjs';
run('go', ['test', './...'], path.join(root, 'backend'), goEnv);
run(npm, ['run', 'build'], path.join(root, 'frontend'));
