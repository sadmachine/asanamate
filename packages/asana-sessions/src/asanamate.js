// The asanamate adapter: the only module that knows asanamate's conversation
// protocol (spec 16) and its ASANAMATE_* environment. It translates requests
// into plain engine calls.
import { createReadStream } from 'node:fs';
import { lookup, open } from './engine.js';
import { UsageError } from './errors.js';
import { readBounded } from './input.js';
import { validGID } from './marker.js';

const MAX_REQUEST = 64 * 1024;

function object(value, what) {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    throw new UsageError(`${what} must be a JSON object`);
  }
  return value;
}

function request(text, operation) {
  let req;
  try {
    req = JSON.parse(text);
  } catch {
    throw new UsageError('request is not valid JSON');
  }
  object(req, 'request');
  if (req.version !== 1) {
    throw new UsageError('unsupported protocol version; expected version 1');
  }
  if (req.operation !== operation) {
    throw new UsageError(`expected operation ${JSON.stringify(operation)}`);
  }
  if (!validGID(object(req.asana, 'asana').task_gid)) {
    throw new UsageError('asana.task_gid must be a task GID');
  }
  return req;
}

// asanamateList answers one discovery request from stdin. A partial lookup is
// still a valid response (exit 0); only no available provider exits nonzero.
export async function asanamateList(c) {
  const req = request(await readBounded(c.stdin, MAX_REQUEST, () => new UsageError('request is too large')), 'list');
  const { status, ...result } = await lookup(c.providers, req.asana.task_gid);
  if (status === 'unavailable') {
    c.stderr.write('asana-sessions: no Claude Code or Codex history is available; run `asana-sessions doctor`\n');
    return 1;
  }
  c.stdout.write(`${JSON.stringify(result)}\n`);
  return 0;
}

// asanamateOpen reads the private selection snapshot, never stdin, which stays
// the terminal for the resumed provider.
export async function asanamateOpen(c) {
  const path = c.env.ASANAMATE_CONVERSATION_JSON;
  if (!path) {
    throw new UsageError('ASANAMATE_CONVERSATION_JSON is not set');
  }
  const text = await readBounded(createReadStream(path), MAX_REQUEST, () => new UsageError('conversation snapshot is too large'));
  const req = request(text, 'open');
  const conv = object(req.conversation, 'conversation');
  if (typeof conv.provider !== 'string' || typeof conv.session_id !== 'string' || conv.task_gid !== req.asana.task_gid) {
    throw new UsageError('conversation must name a provider, session_id, and the requested task_gid');
  }
  for (const [name, value] of [['PROVIDER', conv.provider], ['ID', conv.session_id]]) {
    const exported = c.env[`ASANAMATE_CONVERSATION_${name}`];
    if (exported !== undefined && exported !== value) {
      throw new UsageError(`ASANAMATE_CONVERSATION_${name} does not match the snapshot`);
    }
  }
  return open(c.providers, { provider: conv.provider, sessionId: conv.session_id, gid: req.asana.task_gid }, { ...c.io, env: c.env });
}
