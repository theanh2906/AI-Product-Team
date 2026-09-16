import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const repositoryRoot = resolve(scriptDirectory, '../..');
const binariesDirectory = resolve(repositoryRoot, 'src-tauri/binaries');
const targetTriple = (process.env.CARGO_BUILD_TARGET || execFileSync('rustc', ['--print', 'host-tuple'], { encoding: 'utf8' })).trim();

if (!targetTriple.includes('windows')) {
  throw new Error(`Desktop embedded Go runtime currently supports Windows targets only; received ${targetTriple}.`);
}

const goArchitecture = targetTriple.startsWith('aarch64') ? 'arm64' : 'amd64';
const output = resolve(binariesDirectory, `productcrew-server-${targetTriple}.exe`);

mkdirSync(binariesDirectory, { recursive: true });
execFileSync(
  'go',
  ['build', '-trimpath', '-ldflags', '-s -w', '-o', output, './cmd/server'],
  {
    cwd: repositoryRoot,
    env: { ...process.env, GOOS: 'windows', GOARCH: goArchitecture },
    stdio: 'inherit',
  },
);

console.log(`Embedded Go runtime ready: ${output}`);
