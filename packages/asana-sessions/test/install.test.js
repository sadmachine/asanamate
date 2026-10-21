import assert from 'node:assert/strict';
import { execFile, execFileSync, spawnSync } from 'node:child_process';
import { chmod, mkdir, mkdtemp, readFile, readdir, readlink, realpath, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { delimiter, dirname, join } from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';

const installer = fileURLToPath(new URL('../install.mjs', import.meta.url));
const execute = promisify(execFile);

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), 'asana-sessions-install-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const prefix = join(root, 'prefix with spaces');
  const command = join(prefix, 'bin', 'asana-sessions');
  const library = join(prefix, 'lib', 'asana-sessions');
  return { root, prefix, command, library };
}

async function archive(root, version, { lockVersion = version, link } = {}) {
  const source = join(root, version);
  const pkg = join(source, 'package');
  await mkdir(join(pkg, 'bin'), { recursive: true });
  const manifest = { name: 'asana-sessions', version, bin: { 'asana-sessions': 'bin/asana-sessions.js' } };
  await writeFile(join(pkg, 'package.json'), JSON.stringify(manifest));
  await writeFile(join(pkg, 'npm-shrinkwrap.json'), JSON.stringify({
    name: manifest.name, version: lockVersion, lockfileVersion: 3,
    packages: { '': { ...manifest, version: lockVersion } },
  }));
  await writeFile(join(pkg, 'bin', 'asana-sessions.js'), `#!/usr/bin/env node\nconsole.log(${JSON.stringify(version)});\n`);
  if (link) await symlink(link, join(pkg, 'escape'));
  const path = join(root, `${version}.tgz`);
  execFileSync('tar', ['-czf', path, '-C', source, 'package']);
  return path;
}

function install(path, prefix, env = process.env) {
  return spawnSync(process.execPath, [installer, path, '--prefix', prefix], { env, encoding: 'utf8' });
}

async function failingNpm(root) {
  const bin = join(root, 'fake-bin');
  await mkdir(bin);
  await writeFile(join(bin, 'npm'), '#!/bin/sh\nexit 23\n');
  await chmod(join(bin, 'npm'), 0o755);
  return { ...process.env, PATH: `${bin}${delimiter}${process.env.PATH}` };
}

test('installer handles spaces and upgrades without removing running installations', async (t) => {
  const { root, prefix, command } = await fixture(t);
  const first = install(await archive(root, '0.0.1'), prefix);
  assert.equal(first.status, 0, first.stderr);
  const old = await realpath(command);
  assert.equal(execFileSync(process.execPath, [command], { encoding: 'utf8' }), '0.0.1\n');
  const second = install(await archive(root, '0.0.2'), prefix);
  assert.equal(second.status, 0, second.stderr);
  assert.notEqual(await realpath(command), old);
  assert.equal(execFileSync(process.execPath, [command], { encoding: 'utf8' }), '0.0.2\n');
  assert.equal(execFileSync(process.execPath, [old], { encoding: 'utf8' }), '0.0.1\n');
});

test('failed dependency installation preserves command and cleans staging files', async (t) => {
  const { root, prefix, command, library } = await fixture(t);
  const path = await archive(root, '0.0.1');
  const first = install(path, prefix);
  assert.equal(first.status, 0, first.stderr);
  const old = await readlink(command);
  const directories = await readdir(library);
  const failed = install(path, prefix, await failingNpm(root));
  assert.equal(failed.status, 1);
  assert.match(failed.stderr, /npm failed \(exit 23\)/);
  assert.equal(await readlink(command), old);
  assert.deepEqual(await readdir(library), directories);
  assert.deepEqual(await readdir(dirname(command)), ['asana-sessions']);
});

test('installer refuses to replace ordinary files or directories', async (t) => {
  const { root, prefix, command } = await fixture(t);
  await mkdir(dirname(command), { recursive: true });
  await writeFile(command, 'keep me');
  const path = await archive(root, '0.0.1');
  for (const type of ['file', 'directory']) {
    const result = install(path, prefix);
    assert.equal(result.status, 1);
    assert.match(result.stderr, /Refusing to replace/);
    if (type === 'file') {
      assert.equal(await readFile(command, 'utf8'), 'keep me');
      await rm(command);
      await mkdir(command);
    } else assert.deepEqual(await readdir(command), []);
  }
});

test('installer rejects archive links and mismatched shrinkwrap before running npm', async (t) => {
  const { root, prefix, library } = await fixture(t);
  const outside = join(root, 'outside');
  await writeFile(outside, 'keep me');
  const env = await failingNpm(root);
  const linked = install(await archive(root, '0.0.1', { link: outside }), prefix, env);
  assert.equal(linked.status, 1);
  assert.match(linked.stderr, /links or special files/);
  assert.equal(await readFile(outside, 'utf8'), 'keep me');
  const mismatch = install(await archive(root, '0.0.2', { lockVersion: '0.0.1' }), prefix, env);
  assert.equal(mismatch.status, 1);
  assert.match(mismatch.stderr, /matching shrinkwrap/);
  assert.deepEqual(await readdir(library), []);
});

test('concurrent installers switch only between complete installations', async (t) => {
  const { root, prefix, command, library } = await fixture(t);
  const archives = await Promise.all(['0.0.1', '0.0.2'].map((version) => archive(root, version)));
  await Promise.all(archives.map((path) => execute(process.execPath, [installer, path, '--prefix', prefix])));
  assert.match(execFileSync(process.execPath, [command], { encoding: 'utf8' }), /^0\.0\.[12]\n$/);
  assert.equal((await readdir(library)).length, 2);
  assert.deepEqual(await readdir(dirname(command)), ['asana-sessions']);
});
