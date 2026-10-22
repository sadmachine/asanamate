import assert from 'node:assert/strict';
import { tmpdir } from 'node:os';
import test from 'node:test';
import { Cancelled, link, lookup, open, unlink } from '../src/engine.js';
import { ProviderError, UsageError } from '../src/errors.js';
import { ID, ID2, failing, fakeIO, fakeProvider, lockEnv, session } from './fakes.js';

test('lookup returns exact links from both providers, newest first', async () => {
  const result = await lookup({
    claude: fakeProvider('claude', [session({ name: 'Old [asana:42]', title: 'Old [asana:42]', updatedAt: '2026-01-01T00:00:00.000Z' })]),
    codex: fakeProvider('codex', [
      session({ sessionId: ID2, name: 'New [asana:42]', title: 'New [asana:42]', cwd: '/w', branch: 'b', updatedAt: '2026-02-01T00:00:00.000Z', archived: true }),
      session({ sessionId: ID, name: 'Other [asana:421]' }),
      session({ sessionId: '11111111-1111-1111-1111-111111111111', name: 'mentions 42' }),
    ]),
  }, '42');
  assert.equal(result.status, 'complete');
  assert.deepEqual(result.diagnostics, []);
  assert.deepEqual(result.conversations, [
    { provider: 'codex', session_id: ID2, task_gid: '42', title: 'New [asana:42]', cwd: '/w', branch: 'b', updated_at: '2026-02-01T00:00:00.000Z', archived: true, activity: 'unknown' },
    { provider: 'claude', session_id: ID, task_gid: '42', title: 'Old [asana:42]', updated_at: '2026-01-01T00:00:00.000Z', archived: false, activity: 'unknown' },
  ]);
});

test('lookup lists a fallback-titled link by its generated title without renaming', async () => {
  const titles = { [ID]: 'Rescheduling UI', [ID2]: null };
  const p = fakeProvider('claude', [
    session({ name: 'Conversation [asana:42]', title: 'Conversation [asana:42]' }),
    session({ sessionId: ID2, name: 'Conversation [asana:42]', title: 'Conversation [asana:42]' }),
  ], { generatedTitle: async (id) => titles[id] });
  const result = await lookup({ claude: p }, '42');
  assert.deepEqual(result.conversations.map((c) => c.title).sort(), ['Conversation [asana:42]', 'Rescheduling UI [asana:42]']);
  assert.equal(p.store.get(ID).name, 'Conversation [asana:42]');
  assert.equal(p.calls.some(([method]) => method === 'rename'), false);
});

test('lookup reports conflicts and excludes the record', async () => {
  const result = await lookup({ claude: fakeProvider('claude', [session({ name: 'X [asana:42]', tag: 'asana:7' })]) }, '42');
  assert.equal(result.status, 'partial');
  assert.deepEqual(result.conversations, []);
  assert.equal(result.diagnostics[0].code, 'metadata_conflict');
});

test('lookup distinguishes absent, failed, and unavailable providers', async () => {
  const one = await lookup({ claude: failing('claude', 'unavailable'), codex: fakeProvider('codex') }, '42');
  assert.equal(one.status, 'complete');
  assert.deepEqual(one.diagnostics.map((d) => [d.provider, d.code, d.severity]), [['claude', 'unavailable', 'info']]);

  const partial = await lookup({
    claude: failing('claude', 'permission_denied'),
    codex: fakeProvider('codex', [session({ name: 'A [asana:42]' })]),
  }, '42');
  assert.equal(partial.status, 'partial');
  assert.equal(partial.conversations.length, 1);
  assert.equal(partial.diagnostics[0].severity, 'error');

  const none = await lookup({ claude: failing('claude', 'unavailable'), codex: failing('codex', 'unavailable') }, '42');
  assert.equal(none.status, 'unavailable');

  await assert.rejects(lookup({}, 'abc'), UsageError);
});

test('link appends one marker, verifies it, and is idempotent', async () => {
  const p = fakeProvider('codex', [session({ name: 'Fix sorting' })]);
  const lock = await lockEnv();
  const first = await link({ codex: p }, { provider: 'codex', sessionId: ID, gid: '42' }, lock);
  assert.deepEqual(first, { provider: 'codex', session_id: ID, task_gid: '42', title: 'Fix sorting [asana:42]', changed: true });
  const again = await link({ codex: p }, { provider: 'codex', sessionId: ID, gid: '42' }, lock);
  assert.equal(again.changed, false);
  assert.equal(p.calls.filter((c) => c[0] === 'rename').length, 1);
});

test('link names an untitled session without copying its first prompt', async () => {
  const p = fakeProvider('claude', [session({ name: null, summary: null, title: 'secret prompt text' })]);
  const r = await link({ claude: p }, { provider: 'claude', sessionId: ID, gid: '42' }, await lockEnv());
  assert.equal(r.title, 'Conversation [asana:42]');
  const s = fakeProvider('claude', [session({ name: null, summary: 'Generated summary' })]);
  assert.equal((await link({ claude: s }, { provider: 'claude', sessionId: ID, gid: '42' }, await lockEnv())).title, 'Generated summary [asana:42]');
});

test('link normalizes duplicate identical markers', async () => {
  const p = fakeProvider('codex', [session({ name: 'A [asana:42] [asana:42]' })]);
  const r = await link({ codex: p }, { provider: 'codex', sessionId: ID, gid: '42' }, await lockEnv());
  assert.equal(r.title, 'A [asana:42]');
  assert.equal(r.changed, true);
});

test('link refuses another ticket without --replace, and replace reconciles the tag', async () => {
  const p = fakeProvider('claude', [session({ name: 'A [asana:7]', tag: 'asana:7' })]);
  const lock = await lockEnv();
  await assert.rejects(link({ claude: p }, { provider: 'claude', sessionId: ID, gid: '42' }, lock), (err) => err.code === 'metadata_conflict');
  assert.equal(p.calls.some((c) => c[0] === 'rename'), false);
  const r = await link({ claude: p }, { provider: 'claude', sessionId: ID, gid: '42', replace: true }, lock);
  assert.equal(r.title, 'A [asana:42]');
  assert.equal(p.store.get(ID).tag, null);

  const unrelated = fakeProvider('claude', [session({ name: 'B', tag: 'favorite' })]);
  await link({ claude: unrelated }, { provider: 'claude', sessionId: ID, gid: '42' }, lock);
  assert.equal(unrelated.store.get(ID).tag, 'favorite');
});

test('link fails when readback differs (concurrent rename)', async () => {
  const p = fakeProvider('codex', [session({ name: 'A' })], {
    async rename(id) {
      this.store.get(id).name = 'Renamed by the user';
    },
  });
  await assert.rejects(link({ codex: p }, { provider: 'codex', sessionId: ID, gid: '42' }, await lockEnv()), /did not verify/);
});

test('link validates provider, session, and GID before touching history', async () => {
  const p = fakeProvider('codex', [session({})]);
  const lock = await lockEnv();
  await assert.rejects(link({ codex: p }, { provider: 'gemini', sessionId: ID, gid: '42' }, lock), UsageError);
  await assert.rejects(link({ codex: p }, { provider: '__proto__', sessionId: ID, gid: '42' }, lock), UsageError);
  await assert.rejects(link({ codex: p }, { provider: 'codex', sessionId: '../x', gid: '42' }, lock), UsageError);
  await assert.rejects(link({ codex: p }, { provider: 'codex', sessionId: ID, gid: '4a' }, lock), UsageError);
  await assert.rejects(link({ codex: p }, { provider: 'codex', sessionId: ID2, gid: '42' }, lock), /not found/);
  assert.deepEqual(p.calls, [['read', ID2]]);
});

test('unlink removes only Asana metadata', async () => {
  const p = fakeProvider('claude', [session({ name: 'Fix sorting [asana:42]', tag: 'asana:42' })]);
  const lock = await lockEnv();
  assert.deepEqual(await unlink({ claude: p }, { provider: 'claude', sessionId: ID }, lock), { provider: 'claude', session_id: ID, title: 'Fix sorting', changed: true });
  assert.equal(p.store.get(ID).tag, null);
  assert.equal((await unlink({ claude: p }, { provider: 'claude', sessionId: ID }, lock)).changed, false);

  const keep = fakeProvider('claude', [session({ name: 'Plain [asana:42]', tag: 'favorite' })]);
  await unlink({ claude: keep }, { provider: 'claude', sessionId: ID }, lock);
  assert.equal(keep.store.get(ID).tag, 'favorite');
});

const linked = (fields) => session({ name: 'A [asana:42]', title: 'A [asana:42]', cwd: tmpdir(), ...fields });

test('open refuses Claude with unknown activity and gives a recovery command', async () => {
  const io = fakeIO(['y']);
  await assert.rejects(open({ claude: fakeProvider('claude', [linked()]) }, { provider: 'claude', sessionId: ID, gid: '42' }, io), /Close any other session.*claude resume/);
  assert.deepEqual(io.spawned, []);
});

test('open refuses active conversations', async () => {
  const io = fakeIO(['y']);
  await assert.rejects(open({ codex: fakeProvider('codex', [linked({ activity: 'active' })]) }, { provider: 'codex', sessionId: ID, gid: '42' }, io), /active elsewhere/);
  assert.deepEqual(io.spawned, []);
});

test('open requires a terminal confirmation for unknown activity', async () => {
  const providers = { codex: fakeProvider('codex', [linked()]) };
  const sel = { provider: 'codex', sessionId: ID, gid: '42' };
  await assert.rejects(open(providers, sel, fakeIO([], { interactive: false })), /no terminal/);
  await assert.rejects(open(providers, sel, fakeIO(['n'])), Cancelled);
  await assert.rejects(open(providers, sel, fakeIO([''])), Cancelled);

  const io = fakeIO(['y']);
  assert.equal(await open(providers, sel, io), 3);
  assert.deepEqual(io.spawned.map((s) => [s.command, s.args, s.options.cwd, s.options.stdio]), [['codex', ['resume', ID], tmpdir(), 'inherit']]);
  assert.deepEqual(io.spawned[0].options.env, { KEEP: 'yes' });
});

test('open proceeds without a prompt for inactive conversations', async () => {
  const io = fakeIO([]);
  assert.equal(await open({ codex: fakeProvider('codex', [linked({ activity: 'inactive' })]) }, { provider: 'codex', sessionId: ID, gid: '42' }, io), 3);
  assert.equal(io.said.length, 0);
});

test('open rejects a missing or relinked session', async () => {
  const io = fakeIO(['y']);
  await assert.rejects(open({ codex: fakeProvider('codex', [linked({ name: 'A [asana:7]' })]) }, { provider: 'codex', sessionId: ID, gid: '42' }, io), /no longer/);
  await assert.rejects(open({ codex: fakeProvider('codex', []) }, { provider: 'codex', sessionId: ID, gid: '42' }, io), /no longer/);
  await assert.rejects(open({ codex: failing('codex', 'provider_error') }, { provider: 'codex', sessionId: ID, gid: '42' }, io), ProviderError);
  assert.deepEqual(io.spawned, []);
});

test('open asks for a replacement directory and never rewrites metadata', async () => {
  const p = fakeProvider('codex', [linked({ cwd: '/nonexistent/asana-sessions', branch: 'feature\u001b[31m' })]);
  const io = fakeIO(['y', '/nonexistent/other', tmpdir()]);
  assert.equal(await open({ codex: p }, { provider: 'codex', sessionId: ID, gid: '42' }, io), 3);
  assert.equal(io.spawned[0].options.cwd, tmpdir());
  assert.ok(io.said.some((l) => l.includes('no longer exists')));
  assert.ok(io.said.some((l) => l.startsWith('Not a directory')));
  assert.ok(io.said.some((l) => l.startsWith('Recorded branch: feature (')));
  assert.equal(p.calls.some((c) => c[0] === 'rename'), false);

  await assert.rejects(open({ codex: fakeProvider('codex', [linked({ cwd: undefined })]) }, { provider: 'codex', sessionId: ID, gid: '42' }, fakeIO(['y', ''])), Cancelled);
  await assert.rejects(open({ codex: fakeProvider('codex', [linked({ activity: 'inactive', cwd: undefined })]) }, { provider: 'codex', sessionId: ID, gid: '42' }, fakeIO([], { interactive: false })), /unknown/);
});

test('open restores an archived session only after confirmation and discloses it', async () => {
  const p = fakeProvider('codex', [linked({ archived: true })]);
  const declined = fakeIO(['n']);
  await assert.rejects(open({ codex: p }, { provider: 'codex', sessionId: ID, gid: '42' }, declined), Cancelled);
  assert.equal(p.calls.some((c) => c[0] === 'unarchive'), false);
  // The disclosure precedes the question: a full-screen provider hides later output.
  const disclosed = declined.said.findIndex((l) => l.includes('archive'));
  assert.ok(disclosed >= 0 && disclosed < declined.said.findIndex((l) => l.includes('Resume anyway')));
  const io = fakeIO(['y']);
  await open({ codex: p }, { provider: 'codex', sessionId: ID, gid: '42' }, io);
  assert.deepEqual(p.calls.filter((c) => c[0] === 'unarchive'), [['unarchive', ID]]);
  assert.ok(io.said.some((l) => l.includes('archive')));
});
