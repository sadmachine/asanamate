import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdir, readdir, rm, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { sessionKey, stateDir, withLock } from '../src/lock.js';
import { ID, lockEnv } from './fakes.js';

async function setup(t) {
  const options = await lockEnv();
  t.after(() => rm(options.env.XDG_STATE_HOME, { recursive: true, force: true }));
  const path = join(stateDir(options.env), 'locks', `${sessionKey('codex', ID)}.lock`);
  return { options, path };
}

test('a lock whose owner process exited is free at once', async (t) => {
  const { options, path } = await setup(t);
  const { pid } = spawnSync(process.execPath, ['-e', '']);
  await mkdir(path, { recursive: true });
  await writeFile(join(path, 'owner'), `${pid} abandoned`);
  assert.equal(await withLock('codex', ID, async () => 'ran', options), 'ran');
  await assert.rejects(readdir(path), { code: 'ENOENT' });
});

test('a live owner keeps its lock until the timeout', async (t) => {
  const { options, path } = await setup(t);
  await mkdir(path, { recursive: true });
  await writeFile(join(path, 'owner'), `${process.pid} other`);
  await assert.rejects(withLock('codex', ID, async () => 'ran', options), /in progress/);
});

test('a holder that outlived its lock does not release its successor', async (t) => {
  const { options, path } = await setup(t);
  await withLock('codex', ID, async () => {
    await rm(path, { recursive: true });
    await mkdir(path);
    await writeFile(join(path, 'owner'), `${process.pid} successor`);
  }, options);
  assert.deepEqual(await readdir(path), ['owner']);
});

test('a relative XDG_STATE_HOME is ignored', () => {
  assert.equal(stateDir({ XDG_STATE_HOME: 'state' }), join(homedir(), '.local', 'state', 'asana-sessions'));
  assert.equal(stateDir({ XDG_STATE_HOME: '/x' }), '/x/asana-sessions');
});
