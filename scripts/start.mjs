import path from 'node:path';
import { goEnv, root, run, serverBinary } from './shared.mjs';
run(serverBinary, ['-static', path.join(root, 'frontend', 'dist')], path.join(root, 'backend'), goEnv);
