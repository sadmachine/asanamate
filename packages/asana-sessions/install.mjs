#!/usr/bin/env node
// Install a release with its shrinkwrap as the npm project root.
import { spawnSync } from 'node:child_process';
import { chmod, lstat, mkdir, mkdtemp, readFile, rename, rm, symlink } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join, resolve } from 'node:path';

function run(command, args, options = {}) {
  const result = spawnSync(command, args, { stdio: 'inherit', ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} failed${result.signal ? ` (${result.signal})` : ` (exit ${result.status})`}`);
  return result.stdout;
}

async function checkCommand(path) {
  const entry = await lstat(path).catch((error) => {
    if (error.code !== 'ENOENT') throw error;
  });
  if (entry && !entry.isSymbolicLink()) throw new Error(`Refusing to replace existing file or directory: ${path}`);
}

async function install(archive, prefix) {
  if (Number(process.versions.node.split('.')[0]) < 22) throw new Error('Node.js 22 or later is required');
  archive = resolve(archive);
  prefix = resolve(prefix);
  const bin = join(prefix, 'bin');
  const command = join(bin, 'asana-sessions');
  const library = join(prefix, 'lib', 'asana-sessions');
  await mkdir(bin, { recursive: true });
  await checkCommand(command);

  // npm archives contain only regular files and directories under package/.
  // Reject traversal and links before extracting into the installation tree.
  const listing = { encoding: 'utf8', stdio: ['ignore', 'pipe', 'inherit'], maxBuffer: 16 * 1024 * 1024 };
  const paths = run('tar', ['-tzf', archive], listing).trimEnd().split('\n');
  if (paths.some((path) => !path.startsWith('package/') || path.split('/').includes('..'))) {
    throw new Error('Archive contains a path outside package/');
  }
  const entries = run('tar', ['-tvzf', archive], listing).trimEnd().split('\n');
  if (entries.some((entry) => !['-', 'd'].includes(entry[0]))) throw new Error('Archive contains links or special files');

  await mkdir(library, { recursive: true });
  const directory = await mkdtemp(join(library, 'install-'));
  let linkDirectory;
  let installed = false;
  try {
    run('tar', ['-xzf', archive, '-C', directory, '--strip-components=1', '--no-same-owner', '--no-same-permissions']);
    const pkg = JSON.parse(await readFile(join(directory, 'package.json'), 'utf8'));
    const lock = JSON.parse(await readFile(join(directory, 'npm-shrinkwrap.json'), 'utf8'));
    if (pkg.name !== 'asana-sessions' || !pkg.version || lock.name !== pkg.name ||
        lock.version !== pkg.version || lock.packages?.['']?.version !== pkg.version ||
        pkg.bin?.['asana-sessions'] !== 'bin/asana-sessions.js') {
      throw new Error('Archive is not an asana-sessions package with a matching shrinkwrap');
    }
    run('npm', ['ci', '--omit=dev', '--no-audit', '--no-fund'], { cwd: directory });
    const executable = join(directory, 'bin', 'asana-sessions.js');
    await chmod(executable, 0o755);
    run(process.execPath, [executable, 'version']);

    // Each installation gets its own directory. Concurrent installs can only
    // switch between complete installations; running processes keep their files.
    linkDirectory = await mkdtemp(join(bin, '.asana-sessions-'));
    const link = join(linkDirectory, 'command');
    await symlink(executable, link);
    await checkCommand(command);
    await rename(link, command);
    installed = true;
    console.log(`Installed asana-sessions ${pkg.version}: ${command}`);
  } finally {
    if (linkDirectory) await rm(linkDirectory, { recursive: true, force: true });
    if (!installed) await rm(directory, { recursive: true, force: true });
  }
}

const args = process.argv.slice(2);
if (args.length === 1 && ['--help', '-h'].includes(args[0])) {
  console.log('Usage: node install.mjs <archive.tgz> [--prefix <directory>]\nDefault prefix: ~/.local');
} else if (args.length !== 1 && (args.length !== 3 || args[1] !== '--prefix')) {
  console.error('Usage: node install.mjs <archive.tgz> [--prefix <directory>]');
  process.exitCode = 2;
} else {
  try {
    await install(args[0], args[2] ?? join(homedir(), '.local'));
  } catch (error) {
    console.error(`Installation failed: ${error.message}`);
    process.exitCode = 1;
  }
}
