// Codex history through a short-lived `codex app-server` stdio connection.
// Only thread/list, thread/read, thread/name/set, thread/unarchive, and
// hooks/list are used; never thread/resume or turn/start.
import { spawn as nodeSpawn } from 'node:child_process';
import { stat } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { ProviderError, providerError } from './errors.js';
import { UUID } from './ids.js';
// Interactive, noninteractive, and app-server sources; defaults omit some.
const SOURCE_KINDS = ['cli', 'vscode', 'exec', 'appServer'];
const PAGE_SIZE = 100;
const MAX_PAGES = 1000;
const MAX_LINE = 4 * 1024 * 1024;

// signal, when aborted, stops every running app-server.
export function createCodex({ spawn = nodeSpawn, env = process.env, timeoutMs = 8000, version = '0', signal } = {}) {
  const home = () => env.CODEX_HOME || join(homedir(), '.codex');

  // connect runs fn against one initialized app-server and always stops it.
  async function connect(fn) {
    try {
      await stat(home());
    } catch (err) {
      throw providerError(err, 'Codex');
    }
    const rpc = new Rpc(spawn('codex', ['app-server'], { stdio: ['pipe', 'pipe', 'pipe'], env, signal, killSignal: 'SIGKILL' }), timeoutMs);
    try {
      await rpc.request('initialize', { clientInfo: { name: 'asana-sessions', version } });
      rpc.notify('initialized');
      return await fn(rpc);
    } finally {
      rpc.close();
    }
  }

  return {
    name: 'codex',
    validID: (id) => UUID.test(id),
    // The CLI exports the current thread's ID to its tool commands.
    currentEnv: 'CODEX_THREAD_ID',
    // A thread must stay unnamed until its first turn can generate a title;
    // setting a name prevents generation.
    deferLink: (saved) => Boolean(saved && !saved.name),
    // homeEnv names the Codex home holding a hook event's transcript,
    // <home>/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl, or returns null.
    homeEnv: (transcript, id) => {
      const m = typeof transcript === 'string' && transcript.match(/^(.+)\/sessions\/\d{4}\/\d{2}\/\d{2}\/rollout-[^/]+$/);
      return m && transcript.endsWith(`-${id}.jsonl`) ? { CODEX_HOME: m[1] } : null;
    },

    probe: () => connect((rpc) => rpc.request('thread/list', { limit: 1, sourceKinds: SOURCE_KINDS })),

    // list queries non-archived and archived threads separately, exhausts both
    // cursors, and deduplicates by ID. gid narrows the server-side title search;
    // callers still match the exact suffix.
    list(gid) {
      return connect(async (rpc) => {
        const threads = new Map();
        for (const archived of [false, true]) {
          let cursor = null;
          for (let page = 0; ; page++) {
            if (page === MAX_PAGES) {
              throw new ProviderError('incomplete', 'Codex returned too many history pages');
            }
            const res = await rpc.request('thread/list', {
              archived, cursor, limit: PAGE_SIZE, sourceKinds: SOURCE_KINDS,
              ...(gid ? { searchTerm: `[asana:${gid}]` } : {}),
            });
            if (!Array.isArray(res?.data)) {
              throw new ProviderError('provider_error', 'Codex returned a malformed thread list');
            }
            for (const thread of res.data) {
              threads.set(thread?.id, normalize(thread, archived));
            }
            if (!res.nextCursor || res.nextCursor === cursor) {
              break;
            }
            cursor = res.nextCursor;
          }
        }
        return [...threads.values()];
      });
    },

    read(id) {
      return connect(async (rpc) => normalize((await rpc.request('thread/read', { threadId: id, includeTurns: false }))?.thread, false));
    },

    // hooks lists the hooks Codex would run in cwd, with their source plugin
    // and trust status.
    hooks: (cwd) => connect(async (rpc) => {
      const res = await rpc.request('hooks/list', { cwds: [cwd] });
      if (!Array.isArray(res?.data)) {
        throw new ProviderError('provider_error', 'Codex returned a malformed hook list');
      }
      return res.data.flatMap((entry) => (Array.isArray(entry?.hooks) ? entry.hooks : []));
    }),

    rename: (id, title) => connect((rpc) => rpc.request('thread/name/set', { threadId: id, name: title })),
    unarchive: (id) => connect((rpc) => rpc.request('thread/unarchive', { threadId: id })),

    // The shared daemon refuses a second writer for one thread; --no-daemon
    // would bypass that native safeguard.
    resume: (id) => ({ command: 'codex', args: ['resume', id], clearEnv: ['CODEX_THREAD_ID', 'CODEX_SESSION_ID'] }),
    unknownActivity: 'confirm',
  };
}

// A helper connection's notLoaded status says nothing about other servers, so
// activity is always unknown.
function normalize(thread, archived) {
  if (typeof thread?.id !== 'string' || !UUID.test(thread.id)) {
    throw new ProviderError('provider_error', 'Codex returned a thread without a valid ID');
  }
  const name = typeof thread.name === 'string' && thread.name ? thread.name : null;
  return {
    provider: 'codex',
    sessionId: thread.id,
    name,
    summary: null,
    title: name ?? (typeof thread.preview === 'string' ? thread.preview : ''),
    tag: null,
    cwd: typeof thread.cwd === 'string' && thread.cwd ? thread.cwd : undefined,
    branch: typeof thread.gitInfo?.branch === 'string' && thread.gitInfo.branch ? thread.gitInfo.branch : undefined,
    updatedAt: Number.isFinite(thread.updatedAt) ? new Date(thread.updatedAt * 1000).toISOString() : undefined,
    archived,
    activity: 'unknown',
  };
}

// Rpc is newline-delimited JSON-RPC over the child's stdio. Protocol stdout is
// never echoed; stderr is drained and discarded.
export class Rpc {
  constructor(child, timeoutMs) {
    this.child = child;
    this.next = 1;
    this.pending = new Map();
    this.failure = null;
    child.on('error', (err) => this.fail(providerError(err, 'Codex')));
    child.on('exit', () => this.fail(new ProviderError('provider_error', 'Codex app-server exited before replying')));
    child.stdin.on('error', () => {});
    child.stderr.resume();
    // Split lines here rather than with readline, which buffers a whole line
    // before any length check.
    let partial = '';
    child.stdout.setEncoding('utf8');
    child.stdout.on('data', (chunk) => {
      partial += chunk;
      for (let end = partial.indexOf('\n'); end >= 0; end = partial.indexOf('\n')) {
        const line = partial.slice(0, end).replace(/\r$/, '');
        partial = partial.slice(end + 1);
        this.receive(line);
      }
      if (partial.length > MAX_LINE) {
        partial = '';
        this.fail(new ProviderError('provider_error', 'Codex app-server sent a malformed reply'));
      }
    });
    child.stdout.on('end', () => partial && this.receive(partial));
    this.timer = setTimeout(() => this.fail(new ProviderError('incomplete', 'Codex app-server timed out')), timeoutMs);
  }

  request(method, params) {
    if (this.failure) {
      return Promise.reject(this.failure);
    }
    const id = this.next++;
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject, method });
      this.child.stdin.write(`${JSON.stringify({ id, method, params })}\n`);
    });
  }

  notify(method) {
    this.child.stdin.write(`${JSON.stringify({ method })}\n`);
  }

  receive(line) {
    let msg;
    try {
      if (line.length > MAX_LINE) {
        throw new Error('oversize');
      }
      msg = JSON.parse(line);
    } catch {
      return this.fail(new ProviderError('provider_error', 'Codex app-server sent a malformed reply'));
    }
    if (msg?.method !== undefined) {
      if (msg.id !== undefined) {
        // Server requests (approvals) never apply to metadata operations.
        this.child.stdin.write(`${JSON.stringify({ id: msg.id, error: { code: -32601, message: 'unsupported' } })}\n`);
      }
      return undefined;
    }
    const waiter = this.pending.get(msg?.id);
    if (!waiter) {
      return undefined;
    }
    this.pending.delete(msg.id);
    if (msg.error) {
      const code = msg.error.code === -32601 ? 'unsupported_version' : 'provider_error';
      const text = String(msg.error.message ?? 'error').split('\n')[0].slice(0, 300);
      return waiter.reject(new ProviderError(code, `Codex ${waiter.method}: ${text}`));
    }
    return waiter.resolve(msg.result);
  }

  fail(err) {
    this.failure ??= err;
    for (const { reject } of this.pending.values()) {
      reject(this.failure);
    }
    this.pending.clear();
    this.close();
  }

  close() {
    clearTimeout(this.timer);
    if (this.child.exitCode === null && this.child.signalCode === null) {
      this.child.kill('SIGTERM');
      setTimeout(() => this.child.exitCode === null && this.child.kill('SIGKILL'), 1000).unref();
    }
  }
}
