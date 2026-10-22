// Claude Code history through the official Agent SDK's local session functions.
// No function here starts an agent or calls the model API.
import { execFile } from 'node:child_process';
import { readFile, readdir, stat } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { ProviderError, providerError } from './errors.js';

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function createClaude({ sdk, env = process.env, starts = processStarts } = {}) {
  const load = async () => {
    try {
      return sdk ?? (await import('@anthropic-ai/claude-agent-sdk'));
    } catch {
      throw new ProviderError('unavailable', 'the Claude Agent SDK is not installed; reinstall asana-sessions');
    }
  };
  const home = () => env.CLAUDE_CONFIG_DIR || join(homedir(), '.claude');
  const history = () => stat(join(home(), 'projects')).catch((err) => {
    throw providerError(err, 'Claude Code');
  });
  const call = async (fn) => {
    const previous = process.env.CLAUDE_CONFIG_DIR;
    try {
      // The SDK reads its config directory from the process environment only.
      if (process.env !== env) {
        if (env.CLAUDE_CONFIG_DIR) {
          process.env.CLAUDE_CONFIG_DIR = env.CLAUDE_CONFIG_DIR;
        } else {
          delete process.env.CLAUDE_CONFIG_DIR;
        }
      }
      return await fn(await load());
    } catch (err) {
      throw providerError(err, 'Claude Code');
    } finally {
      if (previous === undefined) {
        delete process.env.CLAUDE_CONFIG_DIR;
      } else {
        process.env.CLAUDE_CONFIG_DIR = previous;
      }
    }
  };

  return {
    name: 'claude',
    validID: (id) => UUID.test(id),
    // The CLI exports the current conversation's ID to its tool commands.
    currentEnv: 'CLAUDE_CODE_SESSION_ID',
    // homeEnv names the config directory holding a hook event's transcript,
    // <home>/projects/<project>/<id>.jsonl, or returns null.
    homeEnv: (transcript, id) => {
      const m = typeof transcript === 'string' && transcript.match(/^(.+)\/projects\/[^/]+\/[^/]+$/);
      return m && transcript.endsWith(`/${id}.jsonl`) ? { CLAUDE_CONFIG_DIR: m[1] } : null;
    },

    async probe() {
      await history();
      await call((s) => s.listSessions({ limit: 1 }));
    },

    // list returns every session across projects and worktrees; the SDK returns
    // all of them when no limit is given, so there is no page to miss.
    async list() {
      await history();
      const [infos, open] = await Promise.all([
        call((s) => s.listSessions({ includeProgrammatic: true })),
        running(home(), starts),
      ]);
      return infos.map((info) => normalize(info, !open ? 'unknown' : open.has(info.sessionId) ? 'active' : 'inactive'));
    },

    async read(id) {
      await history();
      return call(async (s) => {
        const info = await s.getSessionInfo(id);
        return info ? normalize(info, 'unknown') : null;
      });
    },

    // generatedTitle returns Claude Code's latest generated title from the
    // session transcript, or null. The SDK reports it only until a custom
    // title is set. The transcript format is not a documented API, so anything
    // unexpected means null.
    async generatedTitle(id) {
      if (!UUID.test(id)) {
        return null;
      }
      const root = join(home(), 'projects');
      for (const project of await readdir(root).catch(() => [])) {
        const text = await readFile(join(root, project, `${id}.jsonl`), 'utf8').catch(() => null);
        if (text !== null) {
          return lastAITitle(text);
        }
      }
      return null;
    },

    rename: (id, title) => call((s) => s.renameSession(id, title)),
    setTag: (id, tag) => call((s) => s.tagSession(id, tag)),

    // The CLI has no cross-process writer exclusion, so the opener refuses
    // unless the session registry proves the conversation inactive. An
    // inherited CLAUDE_CODE_CHILD_SESSION turns transcript saving off.
    resume: (id) => ({ command: 'claude', args: ['--resume', id], clearEnv: ['CLAUDE_CODE_CHILD_SESSION'] }),
    unknownActivity: 'refuse',
  };
}

// running reads the CLI's live-session registry, <home>/sessions/<pid>.json,
// which every interactive and print-mode process writes while it runs, updates
// when it switches conversation, and removes on exit. It returns the session
// IDs in use, or null when activity cannot be established. Entries left by a
// killed process are ignored when the PID is gone or its start time differs.
// The format is not a documented API: anything unexpected means unknown.
async function running(home, starts) {
  const dir = join(home, 'sessions');
  let names;
  try {
    names = (await readdir(dir)).filter((n) => n.endsWith('.json'));
  } catch {
    return null;
  }
  const entries = [];
  for (const name of names) {
    let d;
    try {
      d = JSON.parse(await readFile(join(dir, name), 'utf8'));
    } catch (err) {
      if (err.code === 'ENOENT') {
        continue; // exited while listing
      }
      return null;
    }
    if (!Number.isInteger(d?.pid) || d.pid <= 0 || typeof d.sessionId !== 'string' || typeof d.procStart !== 'string') {
      return null;
    }
    entries.push(d);
  }
  const live = entries.length ? await starts(entries.map((d) => d.pid)) : new Map();
  if (!live) {
    return null;
  }
  const open = new Set();
  for (const d of entries) {
    // A PID from another process namespace cannot be checked here.
    if (d.pidDomain !== process.platform || norm(live.get(d.pid) ?? '') === norm(d.procStart)) {
      open.add(d.sessionId);
    }
  }
  return open;
}

const norm = (s) => s.trim().replace(/\s+/g, ' ');

// processStarts maps each running PID to its start time, in the format the
// registry's procStart uses. A missing PID has exited. Returns null if ps fails.
function processStarts(pids) {
  return new Promise((resolve) => {
    execFile('ps', ['-o', 'pid=,lstart=', '-p', pids.join(',')], { env: { ...process.env, TZ: 'UTC', LC_ALL: 'C' }, timeout: 3000 }, (err, stdout) => {
      // ps exits 1 when none of the PIDs exist.
      if (err && !(err.code === 1 && stdout === '')) {
        return resolve(null);
      }
      const live = new Map();
      for (const line of stdout.split('\n')) {
        const m = line.trim().match(/^(\d+)\s+(.+)$/);
        if (m) {
          live.set(Number(m[1]), m[2]);
        }
      }
      return resolve(live);
    });
  });
}

function lastAITitle(text) {
  for (const line of text.split('\n').reverse()) {
    if (!line.includes('"ai-title"')) {
      continue;
    }
    try {
      const entry = JSON.parse(line);
      if (entry?.type === 'ai-title' && typeof entry.aiTitle === 'string' && entry.aiTitle.trim()) {
        return entry.aiTitle.trim();
      }
    } catch {
      // A partial or foreign line is not a title.
    }
  }
  return null;
}

// normalize keeps the user-set title as name: only it can carry a ticket marker.
// summary is a generated title, never the raw first prompt.
function normalize(info, activity) {
  if (typeof info?.sessionId !== 'string' || !UUID.test(info.sessionId)) {
    throw new ProviderError('provider_error', 'Claude Code returned a session without a valid ID');
  }
  const summary = info.summary && info.summary !== info.firstPrompt ? info.summary : null;
  return {
    provider: 'claude',
    sessionId: info.sessionId,
    name: info.customTitle || null,
    summary,
    title: info.customTitle || info.summary || '',
    tag: info.tag || null,
    cwd: info.cwd || undefined,
    branch: info.gitBranch || undefined,
    updatedAt: Number.isFinite(info.lastModified) ? new Date(info.lastModified).toISOString() : undefined,
    archived: false,
    activity,
  };
}
