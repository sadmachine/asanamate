import assert from 'node:assert/strict';
import { mkdtemp, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Readable } from 'node:stream';
import test from 'node:test';
import { VERSION, main } from '../src/cli.js';
import { ID, ID2, failing, fakeIO, fakeProvider, lockEnv, session } from './fakes.js';

function sink() {
  const out = { text: '', write: (s) => { out.text += s; return true; } };
  return out;
}

async function run(argv, { providers, stdin = '', env = {}, io = fakeIO([], { interactive: false }) } = {}) {
  const stdout = sink();
  const stderr = sink();
  const code = await main(argv, { stdout, stderr, stdin: Readable.from([stdin]), env, providers, io });
  return { code, stdout: stdout.text, stderr: stderr.text };
}

const linked = () => ({
  claude: failing('claude', 'unavailable'),
  codex: fakeProvider('codex', [session({ name: 'Fix \u001b[2Jsorting [asana:42]', title: 'Fix \u001b[2Jsorting [asana:42]', cwd: tmpdir() })]),
});

test('help and version need no providers', async () => {
  assert.equal((await run(['help'])).code, 0);
  assert.match((await run([])).stdout, /asana-sessions list/);
  assert.equal((await run(['version'])).stdout, `${VERSION}\n`);
});

const agentRow = (provider, id, gid = '') => ['/repo', 'working', 'pane', 'title', 'main', '/repo', gid, provider, id].join('\t');
const agentCommand = (text) => ['asanamate', 'agents', '--', process.execPath, '-e', 'process.stdout.write(process.argv[1])', text];

test('agent decorator uses exact provider/session identity and preserves status and focus', async () => {
  const providers = {
    codex: fakeProvider('codex', [session({ name: 'Fix [asana:42]' })]),
    claude: fakeProvider('claude', [session({ name: 'Other [asana:99]' })]),
  };
  const rows = [agentRow('codex', ID), agentRow('claude', ID), agentRow('codex', ID2), '/legacy\tidle'].join('\n');
  const result = await run(agentCommand(rows), { providers });
  assert.equal(result.code, 0);
  assert.equal(result.stdout, [agentRow('codex', ID, '42'), agentRow('claude', ID, '99'), agentRow('codex', ID2), '/legacy\tidle', ''].join('\n'));
  assert.deepEqual(providers.codex.calls, [['list', undefined]]);
});

test('agent decorator preserves explicit source links and blocks conflicting metadata', async () => {
  const providers = { codex: fakeProvider('codex', [session({ name: 'Fix [asana:42]', tag: 'asana:99' })]) };
  const rows = [agentRow('codex', ID), agentRow('codex', ID, '77')].join('\n');
  const result = await run(agentCommand(rows), { providers });
  assert.equal(result.code, 0);
  assert.equal(result.stdout, `${agentRow('codex', ID, '-')}\n${agentRow('codex', ID, '77')}\n`);
  assert.match(result.stderr, /conflicting ticket links/);
});

test('agent decorator falls back for unavailable providers, unsupported providers, and missing identity', async () => {
  const providers = { codex: failing('codex', 'unavailable') };
  const rows = [agentRow('codex', ID), agentRow('other', ID), agentRow('codex', ''), '/legacy\tidle'].join('\n');
  const result = await run(agentCommand(rows), { providers });
  assert.equal(result.code, 0);
  assert.equal(result.stdout, `${rows}\n`);
  assert.match(result.stderr, /using repo\/branch fallback/);
});

test('agent decorator does not hide source failures or execute source arguments as shell text', async () => {
  const failure = await run(['asanamate', 'agents', '--', process.execPath, '-e', 'process.stdout.write("/repo\\tidle"); process.exit(3)']);
  assert.equal(failure.code, 1);
  assert.equal(failure.stdout, '');
  const legacy = '/repo\tidle\t$(exit 7)';
  const ok = await run(agentCommand(legacy));
  assert.equal(ok.code, 0);
  assert.equal(ok.stdout, `${legacy}\n`);
  const eof = await run(['asanamate', 'agents', '--', process.execPath, '-e', 'process.stdin.resume(); process.stdin.on("end", () => process.stdout.write("/repo\\tidle\\n"))']);
  assert.equal(eof.code, 0);
  assert.equal(eof.stdout, '/repo\tidle\n');
});

test('agent decorator queries no history without usable native identity', async () => {
  const providers = { codex: fakeProvider('codex') };
  const result = await run(agentCommand(agentRow('codex', '')), { providers });
  assert.equal(result.code, 0);
  assert.deepEqual(providers.codex.calls, []);
});

test('agent decorator keeps successful links when another provider times out', async () => {
  const providers = {
    codex: fakeProvider('codex', [session({ name: 'Fix [asana:42]' })]),
    claude: fakeProvider('claude', [], { list: () => new Promise(() => {}) }),
  };
  const rows = [agentRow('codex', ID), agentRow('claude', ID)].join('\n');
  const result = await run(agentCommand(rows), { providers });
  assert.equal(result.code, 0);
  assert.equal(result.stdout, `${agentRow('codex', ID, '42')}\n${agentRow('claude', ID)}\n`);
  assert.match(result.stderr, /timed out.*fallback/);
});

test('usage errors exit 2', async () => {
  for (const argv of [['bogus'], ['list'], ['list', 'not-a-task'], ['link', '--provider', 'codex'], ['list', '42', '--nope'], ['asanamate']]) {
    const r = await run(argv, { providers: linked() });
    assert.equal(r.code, 2, argv.join(' '));
    assert.match(r.stderr, /asana-sessions help/);
  }
});

test('list prints sanitized human rows and diagnostics', async () => {
  const r = await run(['list', 'https://app.asana.com/0/1/42'], { providers: linked() });
  assert.equal(r.code, 0);
  assert.match(r.stdout, new RegExp(`codex  ${ID}`));
  assert.match(r.stdout, /Fix sorting \[asana:42\]/);
  assert.doesNotMatch(r.stdout, /\u001b/);
  assert.match(r.stdout, /info: claude: unavailable/);
});

test('list --json returns the versioned record shape and partial exit status', async () => {
  const ok = await run(['list', '42', '--json'], { providers: linked() });
  assert.equal(ok.code, 0);
  const body = JSON.parse(ok.stdout);
  assert.equal(body.version, 1);
  assert.equal(body.conversations[0].session_id, ID);
  assert.equal(body.status, undefined);

  const partial = await run(['list', '42', '--json'], { providers: { claude: failing('claude', 'provider_error'), codex: linked().codex } });
  assert.equal(partial.code, 1);
  assert.equal(JSON.parse(partial.stdout).conversations.length, 1);

  const none = await run(['list', '42', '--json'], { providers: { claude: failing('claude', 'unavailable'), codex: failing('codex', 'unavailable') } });
  assert.equal(none.code, 1);
  assert.deepEqual(JSON.parse(none.stdout).conversations, []);
});

test('link and unlink print structured results', async () => {
  const providers = { codex: fakeProvider('codex', [session({ name: 'Fix' })]) };
  const { env } = await lockEnv();
  const linkedRun = await run(['link', '--provider', 'codex', '--session', ID, '--task', '42'], { providers, env });
  assert.equal(linkedRun.code, 0);
  assert.deepEqual(JSON.parse(linkedRun.stdout), { provider: 'codex', session_id: ID, task_gid: '42', title: 'Fix [asana:42]', changed: true });
  const conflict = await run(['link', '--provider', 'codex', '--session', ID, '--task', '7'], { providers, env });
  assert.equal(conflict.code, 1);
  assert.match(conflict.stderr, /--replace/);
  const unlinked = await run(['unlink', '--provider', 'codex', '--session', ID], { providers, env });
  assert.equal(JSON.parse(unlinked.stdout).title, 'Fix');
});

test('asanamate list adapts the protocol request', async () => {
  const request = JSON.stringify({ version: 1, operation: 'list', asana: { task_gid: '42' }, extra: true });
  const ok = await run(['asanamate', 'list'], { providers: linked(), stdin: request });
  assert.equal(ok.code, 0);
  assert.equal(JSON.parse(ok.stdout).conversations.length, 1);

  // A partial lookup is still a valid protocol response.
  const partial = await run(['asanamate', 'list'], { providers: { claude: failing('claude', 'permission_denied'), codex: linked().codex }, stdin: request });
  assert.equal(partial.code, 0);
  assert.equal(JSON.parse(partial.stdout).diagnostics[0].code, 'permission_denied');

  const none = await run(['asanamate', 'list'], { providers: { claude: failing('claude', 'unavailable'), codex: failing('codex', 'unavailable') }, stdin: request });
  assert.equal(none.code, 1);
  assert.equal(none.stdout, '');
  assert.match(none.stderr, /doctor/);

  for (const bad of ['nope', '[]', '{"version":2,"operation":"list","asana":{"task_gid":"42"}}', '{"version":1,"operation":"open","asana":{"task_gid":"42"}}', '{"version":1,"operation":"list","asana":{"task_gid":"4a"}}', 'x'.repeat(70000)]) {
    assert.equal((await run(['asanamate', 'list'], { providers: linked(), stdin: bad })).code, 2, bad.slice(0, 40));
  }
});

test('asanamate open reads the private snapshot and keeps stdin for the terminal', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'asana-sessions-open-'));
  const path = join(dir, 'conversation.json');
  const conversation = { provider: 'codex', session_id: ID, task_gid: '42', title: 'x', activity: 'unknown', archived: false };
  await writeFile(path, JSON.stringify({ version: 1, operation: 'open', asana: { task_gid: '42' }, conversation }));
  const env = { ASANAMATE_CONVERSATION_JSON: path, ASANAMATE_CONVERSATION_PROVIDER: 'codex', ASANAMATE_CONVERSATION_ID: ID };

  const io = fakeIO(['y']);
  const r = await run(['asanamate', 'open'], { providers: linked(), env, io });
  assert.equal(r.code, 3);
  assert.deepEqual(io.spawned.map((s) => s.args), [['resume', ID]]);

  const mismatch = await run(['asanamate', 'open'], { providers: linked(), env: { ...env, ASANAMATE_CONVERSATION_ID: 'other' }, io: fakeIO(['y']) });
  assert.equal(mismatch.code, 2);
  assert.equal((await run(['asanamate', 'open'], { providers: linked(), env: {} })).code, 2);

  await writeFile(path, JSON.stringify({ version: 1, operation: 'open', asana: { task_gid: '42' }, conversation: { ...conversation, task_gid: '7' } }));
  assert.equal((await run(['asanamate', 'open'], { providers: linked(), env })).code, 2);
});

test('--current uses only the documented provider identity and never guesses', async () => {
  const p = fakeProvider('codex', [session({ name: 'Exact session' })], { currentEnv: 'CODEX_THREAD_ID' });
  const { env } = await lockEnv();
  const argv = ['link', '--provider', 'codex', '--current', '--task', '42'];
  const missing = await run(argv, { providers: { codex: p }, env });
  assert.equal(missing.code, 2);
  assert.match(missing.stderr, /pass --session/);
  assert.equal(p.calls.length, 0);
  const invalid = await run(argv, { providers: { codex: p }, env: { ...env, CODEX_THREAD_ID: '../bad' } });
  assert.equal(invalid.code, 2);
  const linked = await run(argv, { providers: { codex: p }, env: { ...env, CODEX_THREAD_ID: ID } });
  assert.equal(linked.code, 0);
  assert.equal(JSON.parse(linked.stdout).session_id, ID);
  const both = await run([...argv, '--session', ID], { providers: { codex: p }, env });
  assert.equal(both.code, 2);
});
