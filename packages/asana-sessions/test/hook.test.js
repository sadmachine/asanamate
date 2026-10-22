import assert from 'node:assert/strict';
import { readdir, readFile, rm, utimes } from 'node:fs/promises';
import { join } from 'node:path';
import { PassThrough, Readable } from 'node:stream';
import test from 'node:test';
import { hook, MAX_EVENT } from '../src/hook.js';
import { link, unlink } from '../src/engine.js';
import { taskGIDsInText } from '../src/marker.js';
import { savePending, takePending, sweepPending, RETENTION_MS } from '../src/pending.js';
import { ID, fakeProvider, lockEnv, session } from './fakes.js';

const event = (extra = {}) => ({ hook_event_name: 'UserPromptSubmit', session_id: ID, cwd: '/tmp', prompt: 'https://app.asana.com/0/1/42', ...extra });
async function run(p, input, env, options) {
  let output = '';
  const c = { providers: { [p.name]: p }, env, stdin: typeof input === 'string' ? Readable.from([input]) : input,
    stdout: { write: (s) => { output += s; } }, abort: new AbortController() };
  assert.equal(await hook(p.name, c, options), 0);
  return output ? JSON.parse(output).systemMessage : '';
}

test('URL extraction uses only supported task links and unique GIDs', () => {
  assert.deepEqual(taskGIDsInText('42 [task](https://app.asana.com/0/1/42). <https://app.asana.com/1/1/task/42> https://app.asana.com/0/1/43!'), ['42', '43']);
  assert.deepEqual(taskGIDsInText('https://evil.test/0/1/42 https://app.asana.com.evil/0/1/42 https://app.asana.com/0/1/list'), []);
});

test('prompt hook verifies exact session, preserves title and tag, never replaces', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const p = fakeProvider('codex', [session({ name: 'Work', tag: 'keep' })]);
  const send = (e) => run(p, JSON.stringify(event(e)), env);
  assert.match(await send(), /linked this conversation/);
  assert.equal(p.store.get(ID).name, 'Work [asana:42]');
  assert.equal(p.store.get(ID).tag, 'keep');
  assert.equal(await send(), '');
  assert.match(await send({ prompt: 'https://app.asana.com/0/1/43' }), /already links ticket 42/);
  assert.equal(p.store.get(ID).name, 'Work [asana:42]');
  assert.match(await send({ prompt: 'https://app.asana.com/0/1/42 https://app.asana.com/0/1/43' }), /2 Asana tasks/);
  assert.equal(await send({ prompt: 'no task', system: 'https://app.asana.com/0/1/43' }), '');
});

test('ASANA_SESSIONS_TASK links its task when the hook runs in that session', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const p = fakeProvider('codex', [session({ name: 'Work' })], { currentEnv: 'CODEX_THREAD_ID' });
  const send = (e, extra) => run(p, JSON.stringify(event(e)), { ...env, CODEX_THREAD_ID: ID, ...extra });
  assert.match(await send({ prompt: 'no task' }, { ASANA_SESSIONS_TASK: 'nope' }), /ASANA_SESSIONS_TASK is not an Asana task URL or GID/);
  assert.equal(p.store.get(ID).name, 'Work');
  const multi = 'https://app.asana.com/0/1/43 https://app.asana.com/0/1/44';
  assert.match(await send({ prompt: multi }, { ASANA_SESSIONS_TASK: 'https://app.asana.com/0/1/42' }), /linked this conversation/);
  assert.equal(p.store.get(ID).name, 'Work [asana:42]');
  assert.equal(await send({ prompt: 'no task' }, { ASANA_SESSIONS_TASK: '42' }), '');
  assert.match(await send({ prompt: 'no task' }, { ASANA_SESSIONS_TASK: '43' }), /already links ticket 42/);
});

test('ASANA_SESSIONS_TASK from another session falls back to the prompt', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const p = fakeProvider('codex', [session({ name: 'Work' })], { currentEnv: 'CODEX_THREAD_ID' });
  const send = (e, extra) => run(p, JSON.stringify(event(e)), { ...env, ASANA_SESSIONS_TASK: '43', ...extra });
  assert.equal(await send({ prompt: 'no task' }), '');
  assert.equal(await send({ prompt: 'no task' }, { CODEX_THREAD_ID: '00000000-0000-0000-0000-000000000000' }), '');
  assert.equal(p.store.get(ID).name, 'Work');
  assert.match(await send(), /linked this conversation/);
  assert.equal(p.store.get(ID).name, 'Work [asana:42]');
});

test('Claude defers missing first record; Stop consumes only pending GID', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const p = fakeProvider('claude');
  assert.equal(await run(p, JSON.stringify(event()), env), '');
  const files = await readdir(join(env.XDG_STATE_HOME, 'asana-sessions/pending'));
  const pending = JSON.parse(await readFile(join(env.XDG_STATE_HOME, 'asana-sessions/pending', files[0])));
  assert.deepEqual(Object.keys(pending).sort(), ['created_at', 'provider', 'session_id', 'task_gid']);
  p.store.set(ID, session({ provider: 'claude', name: 'Generated title' }));
  assert.match(await run(p, JSON.stringify(event({ hook_event_name: 'Stop', prompt: 'https://app.asana.com/0/1/43' })), env), /task 42/);
  assert.equal(await takePending(env, 'claude', ID), '');
  assert.equal(p.store.get(ID).name, 'Generated title [asana:42]');
});

for (const title of ['Generated title', null]) {
  test(`Codex defers an unnamed thread until Stop ${title ? 'preserves its generated title' : 'uses the fallback'}`, async (t) => {
    const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
    const p = fakeProvider('codex', [session({ title: 'private first prompt' })]);
    const send = (e) => run(p, JSON.stringify(event(e)), env);
    assert.equal(await send(), '');
    assert.equal(await send(), '');
    assert.match(await send({ prompt: 'https://app.asana.com/0/1/43' }), /task 42 is already waiting/);
    assert.equal(p.store.get(ID).name, null);
    assert.equal(p.calls.some(([method]) => method === 'rename'), false);
    p.store.get(ID).name = title;
    assert.match(await send({ hook_event_name: 'Stop', prompt: 'https://app.asana.com/0/1/43' }), /linked this conversation to Asana task 42/);
    assert.equal(p.store.get(ID).name, `${title ?? 'Conversation'} [asana:42]`);
    assert.equal(await takePending(env, 'codex', ID), '');
    p.calls.length = 0;
    assert.equal(await send({ hook_event_name: 'Stop' }), '');
    assert.deepEqual(p.calls, []);
  });
}

for (const mutation of ['unlink', 'replace', 'native rename']) {
  test(`Codex pending link respects ${mutation} before Stop`, async (t) => {
    const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
    const p = fakeProvider('codex', [session()]);
    const send = (e) => run(p, JSON.stringify(event(e)), env);
    assert.equal(await send(), '');
    p.store.get(ID).name = 'Generated title';
    if (mutation === 'unlink') {
      await unlink({ codex: p }, { provider: 'codex', sessionId: ID }, { env });
    } else if (mutation === 'replace') {
      await link({ codex: p }, { provider: 'codex', sessionId: ID, gid: '43', replace: true }, { env });
    } else {
      p.store.get(ID).name = 'Generated title [asana:43]';
    }
    const notice = await send({ hook_event_name: 'Stop' });
    assert.equal(p.store.get(ID).name, mutation === 'unlink' ? 'Generated title' : 'Generated title [asana:43]');
    if (mutation === 'native rename') {
      assert.match(notice, /already links ticket 43; automatic linking skipped/);
    } else {
      assert.equal(notice, '');
    }
    assert.equal(await takePending(env, 'codex', ID), '');
  });
}

test('Codex Stop without a pending link never reads or renames a thread', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const p = fakeProvider('codex', [session({ name: 'Conversation [asana:42]' })]);
  assert.equal(await run(p, JSON.stringify(event({ hook_event_name: 'Stop' })), env), '');
  assert.deepEqual(p.calls, []);
});

for (const persisted of [false, true]) {
  test(`explicit unlink cancels a deferred Claude link ${persisted ? 'after' : 'before'} persistence`, async (t) => {
    const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
    const p = fakeProvider('claude');
    const send = (e) => run(p, JSON.stringify(event(e)), env);
    await send();
    if (persisted) {
      p.store.set(ID, session({ provider: 'claude', name: 'Generated title', tag: 'keep' }));
    }
    const result = await unlink({ claude: p }, { provider: 'claude', sessionId: ID }, { env });
    assert.equal(result.changed, true);
    if (!persisted) {
      p.store.set(ID, session({ provider: 'claude', name: 'Generated title', tag: 'keep' }));
    }
    assert.equal(await send({ hook_event_name: 'Stop' }), '');
    assert.equal(p.store.get(ID).name, 'Generated title');
    assert.equal(p.store.get(ID).tag, 'keep');
    assert.equal(await takePending(env, 'claude', ID), '');
    // Cancelling this pending write does not disable future intentional links.
    assert.match(await send(), /linked this conversation/);
    assert.equal(p.store.get(ID).name, 'Generated title [asana:42]');
  });
}

test('unlink during deferred completion waits for the write and leaves the session unlinked', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const p = fakeProvider('claude', [session({ name: 'Work' })]);
  await savePending(env, 'claude', ID, '42');
  const entered = Promise.withResolvers();
  const release = Promise.withResolvers();
  const rename = p.rename;
  p.rename = async (...args) => {
    if (args[1].includes('[asana:')) {
      entered.resolve();
      await release.promise;
    }
    return rename(...args);
  };
  const stopped = run(p, JSON.stringify(event({ hook_event_name: 'Stop' })), env);
  await entered.promise;
  const unlinked = unlink({ claude: p }, { provider: 'claude', sessionId: ID }, { env });
  release.resolve();
  await stopped;
  await unlinked;
  assert.equal(p.store.get(ID).name, 'Work');
  assert.equal(await run(p, JSON.stringify(event({ hook_event_name: 'Stop' })), env), '');
});

test('unlink during first-prompt lookup cancels the pending link once it is queued', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const p = fakeProvider('claude');
  const entered = Promise.withResolvers();
  const release = Promise.withResolvers();
  const read = p.read;
  let first = true;
  p.read = async (...args) => {
    if (first) {
      first = false;
      entered.resolve();
      await release.promise;
      return null;
    }
    return read(...args);
  };
  const prompting = run(p, JSON.stringify(event()), env);
  await entered.promise;
  const unlinked = unlink({ claude: p }, { provider: 'claude', sessionId: ID }, { env });
  release.resolve();
  const [, result] = await Promise.all([prompting, unlinked]);
  assert.equal(result.changed, true);
  p.store.set(ID, session({ name: 'Work' }));
  assert.equal(await run(p, JSON.stringify(event({ hook_event_name: 'Stop' })), env), '');
  assert.equal(p.store.get(ID).name, 'Work');
});

test('deferred completion preserves a conflicting native link and reports the existing ticket', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const p = fakeProvider('claude');
  await run(p, JSON.stringify(event()), env);
  p.store.set(ID, session({ name: 'Work [asana:43]' }));
  const notice = await run(p, JSON.stringify(event({ hook_event_name: 'Stop' })), env);
  assert.match(notice, /already links ticket 43; automatic linking skipped/);
  assert.equal(p.store.get(ID).name, 'Work [asana:43]');
});

test('Stop replaces only a fallback link prefix once a generated title exists', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  let generated = null;
  const p = fakeProvider('claude', [session({ name: 'Conversation [asana:42]' })], { generatedTitle: async () => generated });
  const stop = () => run(p, JSON.stringify(event({ hook_event_name: 'Stop' })), env);
  assert.equal(await stop(), '');
  assert.equal(p.store.get(ID).name, 'Conversation [asana:42]');
  generated = 'Rescheduling UI';
  assert.equal(await stop(), '');
  assert.equal(p.store.get(ID).name, 'Rescheduling UI [asana:42]');
  generated = 'Something newer';
  await stop();
  assert.equal(p.store.get(ID).name, 'Rescheduling UI [asana:42]');
  p.store.set(ID, session({ name: 'Conversation [asana:42] [asana:43]' }));
  await stop();
  assert.equal(p.store.get(ID).name, 'Conversation [asana:42] [asana:43]');
});

test('malformed, oversize, unreadable and unknown events never block or rename', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const p = fakeProvider('codex');
  for (const text of ['no JSON', '[]', JSON.stringify(event({ session_id: '../bad' })), JSON.stringify(event({ cwd: '' })), JSON.stringify(event({ prompt: null })), 'x'.repeat(MAX_EVENT + 1)]) {
    assert.match(await run(p, text, env), /skipped/);
  }
  assert.match(await run(p, JSON.stringify(event()), env), /not found/);
  assert.equal(p.calls.some(([method]) => method === 'rename'), false);
  p.read = async () => { throw new Error('private data'); };
  assert.match(await run(p, JSON.stringify(event()), env), /unexpected error/);
});

test('deadline aborts provider operations and a stalled stdin', async () => {
  const p = fakeProvider('codex');
  const stream = new PassThrough();
  assert.match(await run(p, stream, {}, { deadlineMs: 10 }), /timed out/);
  assert.equal(stream.destroyed, true);
});

test('pending conflicts serialize; expired entries are discarded and swept', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const results = await Promise.all(['42', '43'].map((gid) => savePending(env, 'claude', ID, gid)));
  assert.equal(results.filter(Boolean).length, 1);
  assert.equal(await takePending(env, 'claude', ID), results.find(Boolean));
  await savePending(env, 'claude', ID, '42');
  const dir = join(env.XDG_STATE_HOME, 'asana-sessions/pending');
  for (const f of await readdir(dir)) await utimes(join(dir, f), 0, 0);
  await sweepPending(env, { now: Date.now() + RETENTION_MS });
  assert.deepEqual(await readdir(dir), []);
});

test('timeout during metadata read cannot cause a later rename', async (t) => {
  const { env } = await lockEnv(); t.after(() => rm(env.XDG_STATE_HOME, { recursive: true, force: true }));
  const p = fakeProvider('codex', [session({ name: 'Keep' })]);
  let finish;
  p.read = () => new Promise((resolve) => { finish = resolve; });
  assert.match(await run(p, JSON.stringify(event()), env, { deadlineMs: 10 }), /timed out/);
  finish(session({ name: 'Keep' }));
  await new Promise((resolve) => setImmediate(resolve));
  assert.equal(p.calls.some(([method]) => method === 'rename'), false);
});

test('event transcript must match the exact session before metadata is touched', async () => {
  const { createClaude } = await import('../src/claude.js');
  const { createCodex } = await import('../src/codex.js');
  for (const p of [createClaude(), createCodex()]) {
    p.read = () => { throw new Error('must not read'); };
    assert.match(await run(p, JSON.stringify(event({ transcript_path: `/wrong/${ID}.jsonl` })), {}), /does not identify/);
  }
});
