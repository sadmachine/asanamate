// Serializes this package's own metadata writes per provider/session. It does
// not protect against a provider's concurrent title writer.
import { createHash, randomUUID } from 'node:crypto';
import { mkdir, readFile, rm, stat, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { isAbsolute, join } from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import { ProviderError } from './errors.js';

// stateDir ignores a relative XDG_STATE_HOME, as the XDG spec requires; hooks
// run in the project directory and would otherwise write into it.
export function stateDir(env = process.env) {
  const base = env.XDG_STATE_HOME && isAbsolute(env.XDG_STATE_HOME) ? env.XDG_STATE_HOME : join(homedir(), '.local', 'state');
  return join(base, 'asana-sessions');
}

// sessionKey names one provider/session in file names without exposing the ID.
export function sessionKey(provider, sessionId) {
  return `${provider}-${createHash('sha256').update(sessionId).digest('hex').slice(0, 32)}`;
}

// A lock is a directory holding an owner file: "<pid> <token>". A lock whose
// owner process is gone, such as a hook that exited at its deadline, is free
// at once; one with no readable owner is free once older than staleMs.
export async function withLock(provider, sessionId, fn, { env = process.env, timeoutMs = 3000, staleMs = 30000, signal } = {}) {
  const dir = join(stateDir(env), 'locks');
  await mkdir(dir, { recursive: true, mode: 0o700 });
  const path = join(dir, `${sessionKey(provider, sessionId)}.lock`);
  const ownerPath = join(path, 'owner');
  const owner = `${process.pid} ${randomUUID()}`;
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    signal?.throwIfAborted();
    try {
      await mkdir(path, { mode: 0o700 });
    } catch (err) {
      if (err.code !== 'EEXIST') {
        throw err;
      }
      const held = await readFile(ownerPath, 'utf8').catch(() => '');
      if (await abandoned(path, held, staleMs)) {
        // Break only the lock judged abandoned; another waiter may already
        // hold a new one at this path.
        if (await readFile(ownerPath, 'utf8').catch(() => '') === held) {
          await rm(path, { recursive: true, force: true });
        }
      } else if (Date.now() > deadline) {
        throw new ProviderError('provider_error', 'another asana-sessions write to this conversation is in progress');
      } else {
        await sleep(50, undefined, { signal });
      }
      continue;
    }
    try {
      await writeFile(ownerPath, owner, { mode: 0o600 });
    } catch (err) {
      await rm(path, { recursive: true, force: true });
      throw err;
    }
    break;
  }
  try {
    signal?.throwIfAborted();
    return await fn();
  } finally {
    // A holder that outlived staleMs must not release its successor's lock.
    if (await readFile(ownerPath, 'utf8').catch(() => '') === owner) {
      await rm(path, { recursive: true, force: true });
    }
  }
}

async function abandoned(path, owner, staleMs) {
  const pid = Number(owner.split(' ')[0]);
  if (owner && Number.isInteger(pid) && pid > 0 && !alive(pid)) {
    return true;
  }
  const info = await stat(path).catch(() => null);
  return Boolean(info) && Date.now() - info.mtimeMs > staleMs;
}

function alive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err) {
    return err.code === 'EPERM';
  }
}
