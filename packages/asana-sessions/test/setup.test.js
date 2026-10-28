import assert from 'node:assert/strict';
import { mkdtemp, rm, symlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import test from 'node:test';
import { helperOnPath, pluginStatus, refresh, setup, remove, PLUGIN_ID, TESTED, AUTO_LINK_RULE } from '../src/setup.js';
import { fakeIO } from './fakes.js';

const pkg = resolve('.');
function context(name, { entry, hooks, version = TESTED[name], missing = false } = {}) {
  const calls = [];
  const c = { pkg, version: '0.1.0', env: { PATH: '' }, cwd: '/tmp', io: fakeIO([], { interactive: false }),
    output: '', stdout: { write: (s) => { c.output += s; } }, calls,
    providers: { codex: { hooks: async () => hooks ?? ['userPromptSubmit', 'stop'].map((eventName) => ({ pluginId: PLUGIN_ID, eventName, enabled: true, trustStatus: 'trusted' })) } },
    exec: async (command, args) => {
      calls.push([command, ...args]);
      if (command !== name) return { missing: true, ok: false, stdout: '', stderr: '' };
      const data = args[0] === '--version' ? version : JSON.stringify(name === 'claude' ? (entry ? [entry] : []) : { installed: entry ? [entry] : [] });
      return { ok: !missing, missing, stdout: data, stderr: '' };
    } };
  return c;
}
function entry(name, fields = {}) {
  const path = join(pkg, 'plugins', name, 'asana-sessions');
  return { id: PLUGIN_ID, pluginId: PLUGIN_ID, version: '0.1.0', enabled: true, readFromFolder: path, source: { path }, ...fields };
}

test('plugin status distinguishes missing, incompatible, registered, stale, disabled and trust', async () => {
  assert.equal((await pluginStatus('codex', context('codex', { missing: true }))).state, 'missing');
  assert.equal((await pluginStatus('codex', context('codex', { version: '0.1.0' }))).state, 'incompatible');
  assert.equal((await pluginStatus('codex', context('codex'))).state, 'not registered');
  for (const name of ['claude', 'codex']) {
    assert.equal((await pluginStatus(name, context(name, { entry: entry(name) }))).state, 'ready');
    assert.equal((await pluginStatus(name, context(name, { entry: entry(name, { version: '0.0.1' }) }))).state, 'stale');
    assert.equal((await pluginStatus(name, context(name, { entry: entry(name, { enabled: false }) }))).state, 'disabled');
  }
  const hooks = ['userPromptSubmit', 'stop'].map((eventName) => ({ pluginId: PLUGIN_ID, eventName, enabled: true, trustStatus: 'trusted' }));
  hooks[1].trustStatus = 'untrusted';
  const c = context('codex', { entry: entry('codex'), hooks });
  assert.equal((await pluginStatus('codex', c)).state, 'untrusted');
  hooks[1].trustStatus = 'trusted';
  hooks[1].enabled = false;
  assert.equal((await pluginStatus('codex', c)).state, 'disabled');
  hooks[1].enabled = true;
  for (const stale of [hooks.slice(0, 1), hooks.slice(1), [...hooks, hooks[1]]]) {
    c.providers.codex.hooks = async () => stale;
    assert.equal((await pluginStatus('codex', c)).state, 'stale');
  }
  c.providers.codex.hooks = async () => [];
  assert.equal((await pluginStatus('codex', c)).state, 'stale');
});

test('setup only calls owned native registration; never approves hook trust', async () => {
  const c = context('codex');
  assert.equal(await setup(c, { provider: 'codex' }), 1);
  assert.ok(c.output.includes(AUTO_LINK_RULE));
  assert.ok(c.calls.some((call) => call.join(' ') === `codex plugin add ${PLUGIN_ID}`));
  assert.ok(c.calls.some((call) => call[2] === 'marketplace' && call[3] === 'add'));
  assert.equal(c.calls.some((call) => call.some((arg) => /trust|bypass|approve/.test(arg))), false);
  assert.match(c.output, /not registered/); // Registration alone never claims ready.
  await assert.rejects(setup(c, {}), /pass --provider/);
});

test('remove uses only owned plugin commands and preserves native history', async (t) => {
  const state = await mkdtemp(join(tmpdir(), 'asana-sessions-remove-'));
  t.after(() => rm(state, { recursive: true, force: true }));
  const c = context('claude', { entry: entry('claude') }); c.env.XDG_STATE_HOME = state;
  assert.equal(await remove(c, { provider: 'claude' }), 0);
  assert.ok(c.calls.some((call) => call.join(' ') === `claude plugin uninstall ${PLUGIN_ID}`));
  assert.ok(c.calls.some((call) => call.join(' ') === 'claude plugin marketplace remove asana-sessions'));
  assert.match(c.output, /history are unchanged/);
});

test('helper PATH diagnostics distinguish this installation, missing and another executable', async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'asana-sessions-path-'));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const c = { pkg, env: { PATH: dir } };
  assert.equal((await helperOnPath(c)).ok, false);
  await symlink(join(pkg, 'bin/asana-sessions.js'), join(dir, 'asana-sessions'));
  assert.equal((await helperOnPath(c)).ok, true);
  await rm(join(dir, 'asana-sessions'));
  await symlink(process.execPath, join(dir, 'asana-sessions'));
  assert.match((await helperOnPath(c)).line, /another installation/);
});

test('setup repairs stale owned registrations before reinstalling', async () => {
  const c = context('claude', { entry: entry('claude', { version: '0.0.1' }) });
  await setup(c, { provider: 'claude' });
  const uninstall = c.calls.findIndex((call) => call.join(' ') === `claude plugin uninstall ${PLUGIN_ID}`);
  const add = c.calls.findIndex((call) => call[1] === 'plugin' && call[2] === 'marketplace' && call[3] === 'add');
  assert.ok(uninstall >= 0, 'stale owned plugin must be removed');
  assert.ok(add > uninstall, 'replacement source registered after removal');
});

test('refresh re-registers only stale registrations of the helper on PATH', async (t) => {
  const dir = await mkdtemp(join(tmpdir(), 'asana-sessions-refresh-'));
  t.after(() => rm(dir, { recursive: true, force: true }));
  await symlink(join(pkg, 'bin/asana-sessions.js'), join(dir, 'asana-sessions'));
  const registers = (c) => c.calls.filter((call) => call[1] === 'plugin' && ['add', 'install'].includes(call[2]));

  const stale = context('claude', { entry: entry('claude', { version: '0.0.1' }) });
  stale.env.PATH = dir;
  assert.equal(await refresh(stale), 0);
  assert.ok(stale.calls.some((call) => call.join(' ') === `claude plugin uninstall ${PLUGIN_ID}`));
  assert.equal(registers(stale).length, 1);

  for (const c of [context('claude'), context('claude', { entry: entry('claude') })]) {
    c.env.PATH = dir;
    assert.equal(await refresh(c), 0);
    assert.deepEqual(registers(c), [], 'unregistered and current plugins are left alone');
  }

  const elsewhere = context('claude', { entry: entry('claude', { version: '0.0.1' }) });
  assert.equal(await refresh(elsewhere), 0);
  assert.deepEqual(elsewhere.calls, [], 'an installation hooks do not run never changes plugins');
  assert.match(elsewhere.output, /not refreshed/);
});
