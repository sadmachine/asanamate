// Pending links: a task GID found in a prompt before Claude saved the
// conversation or Codex generated its title. The Stop hook completes them.
// Entries hold only provider, session ID, task GID, and creation time, never
// the prompt.
import { randomUUID } from 'node:crypto';
import { mkdir, readFile, readdir, rename, rm, stat, writeFile } from 'node:fs/promises';
import { join } from 'node:path';
import { validGID } from './marker.js';
import { sessionKey, stateDir, withLock } from './lock.js';

// RETENTION_MS bounds an entry whose Stop hook never ran, such as after a
// crash or a disabled hook.
export const RETENTION_MS = 24 * 60 * 60 * 1000;

const dir = (env) => join(stateDir(env), 'pending');
const file = (env, provider, sessionId) => join(dir(env), `${sessionKey(provider, sessionId)}.json`);

// savePending records gid for the session. It returns the GID already pending
// for that session when it differs, and then writes nothing.
export async function savePending(env, provider, sessionId, gid) {
  return withLock(`pending-${provider}`, sessionId, () => save(env, provider, sessionId, gid), { env });
}

async function save(env, provider, sessionId, gid) {
  const existing = await peek(env, provider, sessionId);
  if (existing && existing !== gid) {
    return existing;
  }
  await mkdir(dir(env), { recursive: true, mode: 0o700 });
  const path = file(env, provider, sessionId);
  const tmp = `${path}.${randomUUID()}.tmp`;
  try {
    await writeFile(tmp, JSON.stringify({ provider, session_id: sessionId, task_gid: gid, created_at: new Date().toISOString() }), { mode: 0o600 });
    await rename(tmp, path);
  } finally {
    await rm(tmp, { force: true });
  }
  return '';
}

async function peek(env, provider, sessionId) {
  try {
    const entry = JSON.parse(await readFile(file(env, provider, sessionId), 'utf8'));
    const age = Date.now() - Date.parse(entry?.created_at);
    return entry?.provider === provider && entry.session_id === sessionId
      && validGID(entry.task_gid) && age >= 0 && age < RETENTION_MS
      ? entry.task_gid : '';
  } catch (err) {
    if (err.code === 'ENOENT' || err instanceof SyntaxError) {
      return '';
    }
    throw err;
  }
}

// takePending removes and returns the session's pending GID, or ''.
export async function takePending(env, provider, sessionId) {
  return withLock(`pending-${provider}`, sessionId, async () => {
    const gid = await peek(env, provider, sessionId);
    await rm(file(env, provider, sessionId), { force: true });
    return gid;
  }, { env });
}

// sweepPending removes entries older than RETENTION_MS, and with a provider,
// every entry for it.
export async function sweepPending(env, { provider, now = Date.now() } = {}) {
  let names;
  try {
    names = (await readdir(dir(env), { withFileTypes: true }))
      .filter((e) => e.isFile() && /^(claude|codex)-[a-f0-9]{32}\.json(?:\..*\.tmp)?$/.test(e.name))
      .map((e) => e.name);
  } catch {
    return;
  }
  for (const name of names) {
    const path = join(dir(env), name);
    const old = await stat(path).then((s) => now - s.mtimeMs > RETENTION_MS, () => false);
    if (old || (provider && name.startsWith(`${provider}-`))) {
      await rm(path, { force: true });
    }
  }
}
