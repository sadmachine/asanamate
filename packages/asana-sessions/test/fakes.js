import { EventEmitter } from 'node:events';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ProviderError } from '../src/errors.js';

export const ID = '01a14609-3a63-7c02-bd2b-c313bedc3c37';
export const ID2 = '0b6c2c1e-5f39-4f0e-9a43-3f6f9c6c7e2a';

export async function lockEnv() {
  return { env: { XDG_STATE_HOME: await mkdtemp(join(tmpdir(), 'asana-sessions-test-')) }, timeoutMs: 200 };
}

export function session(fields) {
  return {
    provider: 'codex', sessionId: ID, name: null, summary: null, title: '', tag: null,
    cwd: undefined, branch: undefined, updatedAt: undefined, archived: false, activity: 'unknown',
    ...fields,
  };
}

// fakeProvider keeps sessions in memory and records every call.
export function fakeProvider(name, sessions = [], overrides = {}) {
  const calls = [];
  const store = new Map(sessions.map((s) => [s.sessionId, { ...s, provider: name }]));
  return {
    name,
    calls,
    store,
    validID: (id) => /^[0-9a-f-]{36}$/.test(id),
    async list(gid) {
      calls.push(['list', gid]);
      return [...store.values()];
    },
    async read(id) {
      calls.push(['read', id]);
      return store.has(id) ? { ...store.get(id) } : null;
    },
    async rename(id, title) {
      calls.push(['rename', id, title]);
      store.get(id).name = title;
      store.get(id).title = title;
    },
    async setTag(id, tag) {
      calls.push(['setTag', id, tag]);
      store.get(id).tag = tag;
    },
    async unarchive(id) {
      calls.push(['unarchive', id]);
      store.get(id).archived = false;
    },
    resume: (id) => ({ command: name, args: ['resume', id], clearEnv: ['PARENT_SESSION'] }),
    unknownActivity: name === 'claude' ? 'refuse' : 'confirm',
    // Mirror each real provider's link deferral.
    missingIsUnsaved: name === 'claude',
    deferLink: name === 'claude' ? (saved) => !saved : name === 'codex' ? (saved) => Boolean(saved && !saved.name) : undefined,
    ...overrides,
  };
}

export function failing(name, code) {
  return fakeProvider(name, [], {
    async list() {
      throw new ProviderError(code, `${name} ${code}`);
    },
  });
}

// fakeIO answers prompts in order and records what the user saw.
export function fakeIO(answers = [], { interactive = true } = {}) {
  const said = [];
  const spawned = [];
  return {
    interactive,
    said,
    spawned,
    say: (line) => said.push(line),
    ask: async (question) => {
      said.push(question);
      return answers.shift() ?? '';
    },
    env: { PARENT_SESSION: 'parent', KEEP: 'yes' },
    spawn(command, args, options) {
      spawned.push({ command, args, options });
      const child = new EventEmitter();
      child.kill = () => {};
      setImmediate(() => child.emit('exit', 3, null));
      return child;
    },
  };
}
