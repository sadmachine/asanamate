import assert from 'node:assert/strict';
import { mkdir, mkdtemp, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { createClaude } from '../src/claude.js';
import { ID } from './fakes.js';

async function home() {
  const dir = await mkdtemp(join(tmpdir(), 'asana-sessions-claude-'));
  await mkdir(join(dir, 'projects'));
  return { CLAUDE_CONFIG_DIR: dir };
}

// The fake exposes only local session functions; any agent call would throw.
function fakeSDK(infos) {
  const calls = [];
  return {
    calls,
    listSessions: async (options) => {
      calls.push(['listSessions', options]);
      return infos;
    },
    getSessionInfo: async (id) => infos.find((i) => i.sessionId === id),
    renameSession: async (...args) => calls.push(['renameSession', ...args]),
    tagSession: async (...args) => calls.push(['tagSession', ...args]),
  };
}

test('list reads every session across projects as normalized metadata', async () => {
  const sdk = fakeSDK([
    { sessionId: ID, summary: 'Fix [asana:42]', customTitle: 'Fix [asana:42]', firstPrompt: 'hi', cwd: '/w', gitBranch: 'b', tag: 'asana:42', lastModified: 0 },
    { sessionId: '11111111-1111-1111-1111-111111111111', summary: 'raw prompt [asana:42]', firstPrompt: 'raw prompt [asana:42]', lastModified: 0 },
  ]);
  const [linked, prompt] = await createClaude({ sdk, env: await home() }).list();
  assert.deepEqual(sdk.calls, [['listSessions', { includeProgrammatic: true }]]);
  assert.deepEqual(linked, {
    provider: 'claude', sessionId: ID, name: 'Fix [asana:42]', summary: 'Fix [asana:42]', title: 'Fix [asana:42]', tag: 'asana:42',
    cwd: '/w', branch: 'b', updatedAt: '1970-01-01T00:00:00.000Z', archived: false, activity: 'unknown',
  });
  // A first prompt is never treated as a title that can carry a marker.
  assert.equal(prompt.name, null);
  assert.equal(prompt.summary, null);
});

test('generatedTitle reads the latest generated title from the transcript', async () => {
  const env = await home();
  const claude = createClaude({ sdk: fakeSDK([]), env });
  assert.equal(await claude.generatedTitle(ID), null);
  await mkdir(join(env.CLAUDE_CONFIG_DIR, 'projects', '-w'));
  const lines = [
    { type: 'ai-title', aiTitle: 'First', sessionId: ID },
    { type: 'user', message: 'mentions "ai-title" in text' },
    { type: 'ai-title', aiTitle: '  Latest  ', sessionId: ID },
    { type: 'ai-title', aiTitle: '', sessionId: ID },
  ];
  await writeFile(join(env.CLAUDE_CONFIG_DIR, 'projects', '-w', `${ID}.jsonl`), `${lines.map((l) => JSON.stringify(l)).join('\n')}\n{"type":"ai-ti`);
  assert.equal(await claude.generatedTitle(ID), 'Latest');
  assert.equal(await claude.generatedTitle('../../etc/passwd'), null);
});

test('missing history is unavailable; resumption is refused by default', async () => {
  const claude = createClaude({ sdk: fakeSDK([]), env: { CLAUDE_CONFIG_DIR: '/nonexistent/claude-home' } });
  await assert.rejects(claude.list(), (err) => err.code === 'unavailable');
  assert.equal(claude.unknownActivity, 'refuse');
  assert.deepEqual(claude.resume(ID), { command: 'claude', args: ['--resume', ID], clearEnv: ['CLAUDE_CODE_CHILD_SESSION'] });
  assert.equal(claude.validID('../../etc/passwd'), false);
});

test('read returns null for an unknown session; mutations call the SDK', async () => {
  const sdk = fakeSDK([]);
  const claude = createClaude({ sdk, env: await home() });
  assert.equal(await claude.read(ID), null);
  await claude.rename(ID, 'A');
  await claude.setTag(ID, null);
  assert.deepEqual(sdk.calls, [['renameSession', ID, 'A'], ['tagSession', ID, null]]);
});

test('the live-session registry decides activity', async () => {
  const env = await home();
  const dir = join(env.CLAUDE_CONFIG_DIR, 'sessions');
  await mkdir(dir);
  const other = '22222222-2222-2222-2222-222222222222';
  const stale = '33333333-3333-3333-3333-333333333333';
  const entry = (pid, sessionId, procStart, pidDomain = process.platform) => writeFile(join(dir, `${pid}.json`), JSON.stringify({ pid, sessionId, procStart, pidDomain }));
  await entry(10, ID, 'Fri Oct 16 05:02:58 2026');
  await entry(11, stale, 'Fri Oct 16 05:02:58 2026'); // killed: PID gone
  await entry(12, stale, 'Thu Oct 15 01:00:00 2026'); // PID reused by another process
  await writeFile(join(dir, '10.abc.key'), 'ignored');
  const infos = [ID, other, stale].map((sessionId) => ({ sessionId, summary: 's', lastModified: 0 }));
  const starts = async () => new Map([[10, 'Fri Oct 16  05:02:58 2026'], [12, 'Fri Oct 16 06:00:00 2026']]);
  const activity = async (opts) => Object.fromEntries((await createClaude({ sdk: fakeSDK(infos), env, ...opts }).list()).map((s) => [s.sessionId, s.activity]));

  assert.deepEqual(await activity({ starts }), { [ID]: 'active', [other]: 'inactive', [stale]: 'inactive' });
  assert.deepEqual(Object.values(await activity({ starts: async () => null })), ['unknown', 'unknown', 'unknown']);

  // A PID from another namespace cannot be checked, so it counts as running.
  await entry(13, other, 'x', 'linux-container');
  assert.equal((await activity({ starts }))[other], 'active');

  await writeFile(join(dir, '14.json'), '{"pid":"14"}');
  assert.deepEqual(Object.values(await activity({ starts })), ['unknown', 'unknown', 'unknown']);
});

test('no registry directory means unknown activity', async () => {
  const [s] = await createClaude({ sdk: fakeSDK([{ sessionId: ID, summary: 's', lastModified: 0 }]), env: await home(), starts: async () => new Map() }).list();
  assert.equal(s.activity, 'unknown');
});
