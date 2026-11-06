import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { tmpdir } from 'node:os';
import { PassThrough, Writable } from 'node:stream';
import test from 'node:test';
import { createCodex } from '../src/codex.js';
import { ID, ID2 } from './fakes.js';

// fakeServer answers each JSON-RPC line with handler(method, params): a result,
// a raw string line, or undefined to stay silent.
function fakeServer(handler) {
  const seen = [];
  const children = [];
  const spawn = (command, args) => {
    const child = new EventEmitter();
    child.command = [command, ...args];
    child.exitCode = null;
    child.signalCode = null;
    child.stdout = new PassThrough();
    child.stderr = new PassThrough();
    child.kill = (signal) => {
      child.killed = signal;
      child.signalCode = signal;
      setImmediate(() => child.emit('exit', null, signal));
    };
    child.stdin = new Writable({
      write(chunk, _enc, done) {
        for (const line of chunk.toString().split('\n').filter(Boolean)) {
          const msg = JSON.parse(line);
          seen.push(msg);
          if (msg.id === undefined || msg.method === undefined) {
            continue;
          }
          const reply = handler(msg.method, msg.params);
          if (typeof reply === 'string') {
            child.stdout.write(`${reply}\n`);
          } else if (reply?.error) {
            child.stdout.write(`${JSON.stringify({ id: msg.id, error: reply.error })}\n`);
          } else if (reply !== undefined) {
            child.stdout.write(`${JSON.stringify({ id: msg.id, result: reply })}\n`);
          }
        }
        done();
      },
    });
    children.push(child);
    return child;
  };
  return { spawn, seen, children };
}

const thread = (id, name, extra = {}) => ({ id, name, preview: 'first prompt', cwd: '/w', updatedAt: 1760000000, gitInfo: { branch: 'b' }, ...extra });
const env = { CODEX_HOME: tmpdir() };

test('list exhausts both archive states and deduplicates without starting turns', async () => {
  const server = fakeServer((method, params) => {
    if (method === 'initialize') {
      return {};
    }
    if (method === 'thread/list' && !params.archived) {
      return params.cursor ? { data: [thread(ID2, 'B [asana:42]')], nextCursor: null } : { data: [thread(ID, 'A [asana:42]')], nextCursor: 'c1' };
    }
    if (method === 'thread/list') {
      return { data: [thread(ID2, 'B [asana:42]')], nextCursor: null };
    }
    return { error: { code: -1, message: 'unexpected' } };
  });
  const sessions = await createCodex({ spawn: server.spawn, env }).list('42');
  assert.deepEqual(sessions.map((s) => [s.sessionId, s.archived, s.branch, s.activity]), [[ID, false, 'b', 'unknown'], [ID2, true, 'b', 'unknown']]);
  assert.equal(sessions[0].updatedAt, '2025-10-09T08:53:20.000Z');
  const lists = server.seen.filter((m) => m.method === 'thread/list');
  assert.deepEqual(lists.map((m) => [m.params.archived, m.params.cursor, m.params.searchTerm]), [[false, null, '[asana:42]'], [false, 'c1', '[asana:42]'], [true, null, '[asana:42]']]);
  assert.deepEqual(lists[0].params.sourceKinds, ['cli', 'vscode', 'exec', 'appServer']);
  assert.deepEqual(server.seen.map((m) => m.method).filter((m) => /resume|turn/.test(m)), []);
  assert.deepEqual(server.children[0].command, ['codex', 'app-server']);
  assert.equal(server.children[0].killed, 'SIGTERM');
});

test('read, rename, and unarchive use the metadata methods only', async () => {
  const server = fakeServer((method) => (method === 'thread/read' ? { thread: thread(ID, null) } : {}));
  const codex = createCodex({ spawn: server.spawn, env });
  const s = await codex.read(ID);
  assert.equal(s.name, null);
  assert.equal(s.title, 'first prompt');
  await codex.rename(ID, 'A [asana:42]');
  await codex.unarchive(ID);
  assert.deepEqual(server.seen.filter((m) => m.id !== undefined && m.method !== 'initialize').map((m) => [m.method, m.params]), [
    ['thread/read', { threadId: ID, includeTurns: false }],
    ['thread/name/set', { threadId: ID, name: 'A [asana:42]' }],
    ['thread/unarchive', { threadId: ID }],
  ]);
  assert.ok(server.children.every((c) => c.killed));
});

test('a silent server times out and is stopped', async () => {
  const server = fakeServer(() => undefined);
  await assert.rejects(createCodex({ spawn: server.spawn, env, timeoutMs: 50 }).list('42'), (err) => err.code === 'incomplete');
  assert.equal(server.children[0].killed, 'SIGTERM');
});

test('an oversize reply line fails before the line ends', async () => {
  // The first request answers with an unterminated line past the limit.
  const server = fakeServer(() => {
    server.children[0].stdout.write('x'.repeat(5 * 1024 * 1024));
    return undefined;
  });
  await assert.rejects(createCodex({ spawn: server.spawn, env }).list('42'), /malformed/);
});

test('malformed replies, unknown methods, and bad threads fail clearly', async () => {
  const malformed = fakeServer(() => 'not json');
  await assert.rejects(createCodex({ spawn: malformed.spawn, env }).list('42'), (err) => err.code === 'provider_error' && !err.message.includes('not json'));
  assert.ok(malformed.children[0].killed);

  const old = fakeServer((method) => (method === 'initialize' ? {} : { error: { code: -32601, message: 'method not found' } }));
  await assert.rejects(createCodex({ spawn: old.spawn, env }).list('42'), (err) => err.code === 'unsupported_version');

  const shape = fakeServer((method) => (method === 'initialize' ? {} : { data: 'nope' }));
  await assert.rejects(createCodex({ spawn: shape.spawn, env }).list('42'), /malformed/);

  const badID = fakeServer((method) => (method === 'initialize' ? {} : { data: [thread('../etc', 'A [asana:42]')] }));
  await assert.rejects(createCodex({ spawn: badID.spawn, env }).list('42'), /valid ID/);
});

test('a missing executable or history home is unavailable', async () => {
  const spawn = () => {
    const child = fakeServer(() => undefined).spawn('codex', []);
    setImmediate(() => child.emit('error', Object.assign(new Error('spawn codex ENOENT'), { code: 'ENOENT' })));
    return child;
  };
  await assert.rejects(createCodex({ spawn, env }).list('42'), (err) => err.code === 'unavailable');
  await assert.rejects(createCodex({ spawn, env: { CODEX_HOME: '/nonexistent/codex-home' } }).list('42'), (err) => err.code === 'unavailable');
});
